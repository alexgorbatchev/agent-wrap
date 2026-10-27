use agent_common::logging::{FileConfig, Logging, Mode as LogMode, Options};
use cli_command_kit::Mode;
use log4rs::{
    append::{
        Append,
        rolling_file::{
            RollingFileAppender,
            policy::compound::{
                CompoundPolicy, roll::fixed_window::FixedWindowRoller, trigger::size::SizeTrigger,
            },
        },
    },
    encode::pattern::PatternEncoder,
};
use regex::Regex;
use std::{
    collections::HashSet,
    fs::{self, File, OpenOptions},
    io::{self, Write},
    os::{
        fd::AsRawFd,
        unix::fs::{DirBuilderExt, OpenOptionsExt},
    },
    path::{Path, PathBuf},
    sync::LazyLock,
    time::{Duration, SystemTime},
};

pub const RETENTION: Duration = Duration::from_secs(14 * 86400);
static SESSION_LOG: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^(session-[A-Za-z0-9]+)(?:\.[0-4])?\.log(?:\.gz)?$")
        .expect("session log expression")
});
#[derive(Clone, Debug)]
pub struct Locations {
    pub cache: PathBuf,
    pub state: PathBuf,
}
impl Locations {
    pub fn from_env() -> io::Result<Self> {
        let home = || {
            dirs::home_dir().ok_or_else(|| {
                io::Error::new(io::ErrorKind::NotFound, "home directory is unavailable")
            })
        };
        let cache = match std::env::var_os("XDG_CACHE_HOME").filter(|p| !p.is_empty()) {
            Some(p) => PathBuf::from(p),
            None => dirs::cache_dir().ok_or_else(|| {
                io::Error::new(io::ErrorKind::NotFound, "OS cache directory is unavailable")
            })?,
        };
        let state = match std::env::var_os("XDG_STATE_HOME").filter(|p| !p.is_empty()) {
            Some(p) => PathBuf::from(p),
            None => home()?.join(".local/state"),
        };
        Ok(Self {
            cache: cache.join("agent-wrap"),
            state: state.join("agent-wrap"),
        })
    }
}
/// Advisory ownership uses no-follow opens; temporary guards release on drop.
pub fn lock_file(path: &Path, create: bool, nonblocking: bool) -> io::Result<File> {
    let file = OpenOptions::new()
        .read(true)
        .write(true)
        .create(create)
        .mode(0o600)
        .custom_flags(libc::O_CLOEXEC | libc::O_NOFOLLOW)
        .open(path)?;
    loop {
        if unsafe {
            libc::flock(
                file.as_raw_fd(),
                libc::LOCK_EX | if nonblocking { libc::LOCK_NB } else { 0 },
            )
        } == 0
        {
            return Ok(file);
        }
        let error = io::Error::last_os_error();
        if error.kind() != io::ErrorKind::Interrupted {
            return Err(error);
        }
    }
}
fn old(path: &Path, now: SystemTime) -> io::Result<bool> {
    Ok(now
        .duration_since(fs::symlink_metadata(path)?.modified()?)
        .unwrap_or_default()
        > RETENTION)
}
fn maybe_lock(path: &Path) -> io::Result<Option<File>> {
    match lock_file(path, false, true) {
        Ok(file) => Ok(Some(file)),
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(error),
    }
}
fn remove_if_present(path: &Path) -> io::Result<()> {
    match fs::remove_file(path) {
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(()),
        result => result,
    }
}
pub fn prune(cache: &Path, logs: &Path, default_log: bool, now: SystemTime) -> io::Result<()> {
    let mut live = HashSet::new();
    for entry in fs::read_dir(cache)? {
        let entry = entry?;
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if !entry.file_type()?.is_dir() || !name.starts_with("session-") {
            continue;
        }
        let owner = match maybe_lock(&entry.path().join("owner.lock")) {
            Err(error) if error.kind() == io::ErrorKind::WouldBlock => {
                live.insert(name.into_owned());
                continue;
            }
            other => other?,
        };
        if old(&entry.path(), now)? {
            fs::remove_dir_all(entry.path())?;
        }
        drop(owner);
    }
    if !default_log {
        return Ok(());
    }
    for entry in fs::read_dir(logs)? {
        let entry = entry?;
        if !entry.file_type()?.is_file() {
            continue;
        }
        let name = entry.file_name();
        let name = name.to_string_lossy();
        let is_lock = name.ends_with(".log.lock");
        let stem = if is_lock {
            name.trim_end_matches(".lock")
        } else {
            &name
        };
        let Some(matched) = SESSION_LOG.captures(stem) else {
            continue;
        };
        if live.contains(&matched[1]) || !old(&entry.path(), now)? {
            continue;
        }
        let lock_path = logs.join(format!("{}.log.lock", &matched[1]));
        let owner = match maybe_lock(&lock_path) {
            Err(error) if error.kind() == io::ErrorKind::WouldBlock => continue,
            other => other?,
        };
        remove_if_present(&entry.path())?;
        drop(owner);
    }
    Ok(())
}
struct LogOwner {
    path: PathBuf,
    _file: File,
}
impl Drop for LogOwner {
    fn drop(&mut self) {
        let _ = remove_if_present(&self.path);
    }
}
pub struct RuntimeFiles {
    pub logging: Logging,
    cache_dir: tempfile::TempDir,
    _owner: File,
    _log_owner: Option<LogOwner>,
    pub cache: PathBuf,
    pub log_path: PathBuf,
}
impl RuntimeFiles {
    pub fn open(locations: &Locations, explicit: Option<&Path>, mode: Mode) -> io::Result<Self> {
        fs::DirBuilder::new()
            .recursive(true)
            .mode(0o700)
            .create(&locations.cache)?;
        let mut guard_path = locations.cache.as_os_str().to_owned();
        guard_path.push(".lock");
        let _guard = lock_file(Path::new(&guard_path), true, false)?;
        let cache_dir = tempfile::Builder::new()
            .prefix("session-")
            .tempdir_in(&locations.cache)?;
        let cache = cache_dir.path().to_path_buf();
        let owner = lock_file(&cache.join("owner.lock"), true, false)?;
        let log_path = explicit.map(Path::to_path_buf).unwrap_or_else(|| {
            locations
                .state
                .join(cache.file_name().expect("temporary directory name"))
                .with_extension("log")
        });
        let logs = log_path
            .parent()
            .filter(|p| !p.as_os_str().is_empty())
            .unwrap_or(Path::new("."));
        fs::create_dir_all(logs)?;
        let mut cleanup_guard = None;
        let log_owner = if let Some(matched) = log_path
            .file_name()
            .and_then(|p| p.to_str())
            .and_then(|p| SESSION_LOG.captures(p))
        {
            cleanup_guard = Some(lock_file(&logs.join(".cleanup.lock"), true, false)?);
            let path = logs.join(format!("{}.log.lock", &matched[1]));
            let file = lock_file(&path, true, true).map_err(|e| {
                io::Error::new(
                    e.kind(),
                    format!(
                        "log destination {} is already in use or unavailable: {e}",
                        cli_command_kit::display_path(&log_path)
                    ),
                )
            })?;
            Some(LogOwner { path, _file: file })
        } else {
            None
        };
        prune(
            &locations.cache,
            logs,
            explicit.is_none(),
            SystemTime::now(),
        )?;
        let writer = Rotating::open(&log_path, 10 * 1024 * 1024)?;
        let logging = Logging::setup(
            writer,
            Options {
                service: "agent-wrap".into(),
                level: "info".into(),
                mode: if mode == Mode::Agent {
                    LogMode::Agent
                } else {
                    LogMode::Human
                },
            },
            FileConfig {
                disabled: true,
                ..Default::default()
            },
        )?;
        drop(cleanup_guard);
        Ok(Self {
            logging,
            cache_dir,
            _owner: owner,
            _log_owner: log_owner,
            cache,
            log_path,
        })
    }
    pub fn config(&self) -> agent_watcher::Config {
        agent_watcher::Config {
            cursor_path: self.cache.join("cursors.json"),
            session_state_path: self.cache.join("sessions.json"),
            opencode_checkpoint_path: self.cache.join("opencode.json"),
            ..Default::default()
        }
    }
    pub fn close(self) -> io::Result<()> {
        let Self {
            logging,
            cache_dir,
            _owner: owner,
            _log_owner: log_owner,
            ..
        } = self;
        let log_error = logging.finish().err();
        drop(logging);
        let cache_error = cache_dir.close().err();
        drop(owner);
        let lock_error = log_owner
            .as_ref()
            .and_then(|owner| remove_if_present(&owner.path).err());
        drop(log_owner);
        let errors = log_error
            .into_iter()
            .chain(cache_error)
            .chain(lock_error)
            .collect::<Vec<_>>();
        if errors.is_empty() {
            Ok(())
        } else {
            Err(io::Error::other(
                errors
                    .iter()
                    .map(ToString::to_string)
                    .collect::<Vec<_>>()
                    .join("; "),
            ))
        }
    }
}
/// Native log4rs rolls the exact selected path, compresses and retains five
/// archives. The I/O adapter lets the shared tracing formatter write each record.
pub struct Rotating {
    appender: RollingFileAppender,
    archives: Vec<PathBuf>,
}
impl Rotating {
    pub fn open(path: &Path, size: u64) -> io::Result<Self> {
        let parent = path
            .parent()
            .filter(|p| !p.as_os_str().is_empty())
            .unwrap_or(Path::new("."));
        fs::create_dir_all(parent)?;
        let stem = path.file_stem().ok_or_else(|| {
            io::Error::new(io::ErrorKind::InvalidInput, "log file name is required")
        })?;
        let mut prefix = parent.join(stem).into_os_string();
        prefix.push(".");
        let roller = FixedWindowRoller::builder()
            .build_literal(Path::new(&prefix), std::ffi::OsStr::new(".log.gz"), 5)
            .map_err(io::Error::other)?;
        let policy = CompoundPolicy::new(Box::new(SizeTrigger::new(size)), Box::new(roller));
        let appender = RollingFileAppender::builder()
            .encoder(Box::new(PatternEncoder::new("{m}")))
            .build_literal(path, Box::new(policy))?;
        let result = Self {
            appender,
            archives: (0..5)
                .map(|i| {
                    let mut archive = prefix.clone();
                    archive.push(format!("{i}.log.gz"));
                    PathBuf::from(archive)
                })
                .collect(),
        };
        result.expire()?;
        Ok(result)
    }
    fn expire(&self) -> io::Result<()> {
        for archive in &self.archives {
            match old(archive, SystemTime::now()) {
                Ok(true) => remove_if_present(archive)?,
                Ok(false) => {}
                Err(e) if e.kind() == io::ErrorKind::NotFound => {}
                Err(e) => return Err(e),
            }
        }
        Ok(())
    }
}
impl Write for Rotating {
    fn write(&mut self, bytes: &[u8]) -> io::Result<usize> {
        self.expire()?;
        let message = std::str::from_utf8(bytes)
            .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
        self.appender
            .append(
                &log::Record::builder()
                    .level(log::Level::Info)
                    .args(format_args!("{message}"))
                    .build(),
            )
            .map_err(io::Error::other)?;
        Ok(bytes.len())
    }
    fn flush(&mut self) -> io::Result<()> {
        self.appender.flush();
        Ok(())
    }
}
