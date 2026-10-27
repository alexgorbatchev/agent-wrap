use serde_json::{Value, json};
use std::{
    fs,
    io::{BufRead, Write},
    os::{
        fd::{AsRawFd, FromRawFd, OwnedFd},
        unix::fs::symlink,
    },
    path::{Path, PathBuf},
    process::{Command, Stdio},
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, Ordering},
    },
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};
use tui_frame::terminal::{Effect, Options, Terminal};
fn millis() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_millis() as i64
}
fn append(path: &Path, value: Value) {
    let mut file = fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .unwrap();
    writeln!(file, "{value}").unwrap();
}
const ID: &str = "11111111-1111-4111-8111-111111111111";
const SCHEMA: &str = "CREATE TABLE session(id TEXT,project_id TEXT,parent_id TEXT,slug TEXT,directory TEXT,title TEXT,version TEXT,agent TEXT,model TEXT,cost REAL,tokens_input INTEGER DEFAULT 0,tokens_output INTEGER DEFAULT 0,tokens_reasoning INTEGER DEFAULT 0,tokens_cache_read INTEGER DEFAULT 0,tokens_cache_write INTEGER DEFAULT 0,time_created INTEGER,time_updated INTEGER,time_archived INTEGER,metadata TEXT);CREATE TABLE message(id TEXT,session_id TEXT,time_created INTEGER,data TEXT);CREATE TABLE part(id TEXT,message_id TEXT,session_id TEXT,time_created INTEGER,data TEXT);";
#[test]
fn fixture_child() {
    let Ok(harness) = std::env::var("WRAP_FIXTURE") else {
        return;
    };
    let base = std::env::current_dir().unwrap();
    let path = PathBuf::from(std::env::var_os("WRAP_TRANSCRIPT").unwrap());
    fs::create_dir_all(path.parent().unwrap()).unwrap();
    let now = millis();
    if let Some(path) = std::env::var_os("WRAP_PID") {
        fs::write(path, std::process::id().to_string()).unwrap();
    }
    let mut db = None;
    match harness.as_str() {
        "claude-code" => {
            append(
                &PathBuf::from(std::env::var_os("CLAUDE_SESSIONS_DIR").unwrap())
                    .join(format!("{}.json", std::process::id())),
                json!({"pid":std::process::id(),"sessionId":ID,"cwd":base,"startedAt":now,"version":"fixture","status":"busy","transcriptPath":path}),
            );
            fs::write(&path, "").unwrap();
        }
        "pi" => append(
            &path,
            json!({"type":"session","version":3,"id":ID,"cwd":base,"timestamp":now}),
        ),
        "codex" => append(
            &path,
            json!({"type":"session_meta","timestamp":now,"payload":{"id":ID,"cwd":base,"source":"cli","cli_version":"fixture"}}),
        ),
        "opencode" => {
            let conn =
                rusqlite::Connection::open(std::env::var_os("OPENCODE_DB_PATH").unwrap()).unwrap();
            conn.execute_batch(SCHEMA).unwrap();
            conn.execute("INSERT INTO session(id,directory,title,version,time_created,time_updated) VALUES (?1,?2,'Task','fixture',?3,?3)",rusqlite::params![ID,base.to_str().unwrap(),now]).unwrap();
            db = Some(conn);
        }
        _ => panic!("unsupported fixture"),
    }
    print!("\x1b[2J\x1b[HFRAME CHILD ONLINE");
    std::io::stdout().flush().unwrap();
    for (index, line) in std::io::stdin().lock().lines().enumerate() {
        let dir = line.unwrap();
        if dir == "quit" {
            std::process::exit(37);
        }
        let now = millis();
        let call = format!("call{index}");
        let input = json!({"file_path":format!("{dir}/target.rs"),"path":format!("{dir}/target.rs"),"filePath":format!("{dir}/target.rs")});
        match harness.as_str() {
            "claude-code" => append(
                &path,
                json!({"type":"assistant","sessionId":ID,"uuid":call,"cwd":base,"timestamp":now,"message":{"role":"assistant","id":call,"model":"test","content":[{"type":"tool_use","id":call,"name":"Read","input":input}]}}),
            ),
            "pi" => append(
                &path,
                json!({"type":"message","id":call,"timestamp":now,"message":{"role":"assistant","model":"test","timestamp":now,"content":[{"type":"toolCall","id":call,"name":"read","arguments":input}]}}),
            ),
            "codex" => append(
                &path,
                json!({"type":"response_item","timestamp":now,"payload":{"type":"function_call","name":"exec_command","call_id":call,"arguments":json!({"cmd":"pwd","workdir":dir}).to_string()}}),
            ),
            "opencode" => {
                let db = db.as_ref().unwrap();
                db.execute(
                    "INSERT INTO message VALUES (?1,?2,?3,?4)",
                    rusqlite::params![call, ID, now, r#"{"role":"assistant"}"#],
                )
                .unwrap();
                db.execute("INSERT INTO part VALUES (?1,?1,?2,?3,?4)",rusqlite::params![call,ID,now,json!({"type":"tool","tool":"read","callID":call,"state":{"status":"running","input":input,"time":{"start":now}}}).to_string()]).unwrap();
            }
            _ => unreachable!(),
        };
        print!("\x1b[2J\x1b[HCHILD UPDATED");
        std::io::stdout().flush().unwrap();
    }
}
struct Screen {
    master: Arc<OwnedFd>,
    text: Arc<Mutex<String>>,
    done: Arc<AtomicBool>,
    worker: Option<std::thread::JoinHandle<()>>,
}
impl Screen {
    fn new() -> (Self, OwnedFd) {
        let (mut m, mut s) = (-1, -1);
        let size = libc::winsize {
            ws_col: 120,
            ws_row: 16,
            ws_xpixel: 1200,
            ws_ypixel: 320,
        };
        assert_eq!(
            unsafe {
                libc::openpty(
                    &mut m,
                    &mut s,
                    std::ptr::null_mut(),
                    std::ptr::null(),
                    &size,
                )
            },
            0
        );
        let master = Arc::new(unsafe { OwnedFd::from_raw_fd(m) });
        let slave = unsafe { OwnedFd::from_raw_fd(s) };
        for fd in [m, s] {
            assert_eq!(
                unsafe { libc::fcntl(fd, libc::F_SETFD, libc::FD_CLOEXEC) },
                0
            );
        }
        let text = Arc::new(Mutex::new(String::new()));
        let done = Arc::new(AtomicBool::new(false));
        let (fd, result, stop) = (master.clone(), text.clone(), done.clone());
        let worker = std::thread::spawn(move || {
            let mut term = Terminal::new(Options::new(120, 16)).unwrap();
            let mut bytes = [0; 32768];
            while !stop.load(Ordering::Acquire) {
                let mut p = libc::pollfd {
                    fd: fd.as_raw_fd(),
                    events: libc::POLLIN,
                    revents: 0,
                };
                unsafe { libc::poll(&mut p, 1, 10) };
                if p.revents & libc::POLLIN != 0 {
                    let n = unsafe {
                        libc::read(fd.as_raw_fd(), bytes.as_mut_ptr().cast(), bytes.len())
                    };
                    if n > 0 {
                        term.write(&bytes[..n as usize]).unwrap();
                        for effect in term.take_effects() {
                            if let Effect::Reply(bytes) = effect {
                                unsafe {
                                    libc::write(fd.as_raw_fd(), bytes.as_ptr().cast(), bytes.len())
                                };
                            }
                        }
                        let state = term.snapshot().unwrap();
                        *result.lock().unwrap() = state
                            .cells
                            .iter()
                            .map(|c| c.text.as_str())
                            .collect::<String>();
                    }
                }
            }
        });
        (
            Self {
                master,
                text,
                done,
                worker: Some(worker),
            },
            slave,
        )
    }
    fn send(&self, text: &str) {
        assert_eq!(
            unsafe { libc::write(self.master.as_raw_fd(), text.as_ptr().cast(), text.len()) },
            text.len() as isize
        );
    }
    fn wait(&self, text: &str) {
        let end = Instant::now() + Duration::from_secs(8);
        loop {
            let screen = self.text.lock().unwrap().clone();
            if screen.contains(text) {
                return;
            }
            assert!(Instant::now() < end, "missing {text:?}; screen={screen}");
            std::thread::sleep(Duration::from_millis(10));
        }
    }
}
impl Drop for Screen {
    fn drop(&mut self) {
        self.done.store(true, Ordering::Release);
        if let Some(worker) = self.worker.take() {
            worker.join().unwrap();
        }
    }
}
struct Child(std::process::Child);
impl Child {
    fn wait(&mut self) -> std::process::ExitStatus {
        let end = Instant::now() + Duration::from_secs(8);
        loop {
            if let Some(status) = self.0.try_wait().unwrap() {
                return status;
            }
            assert!(Instant::now() < end, "wrapper did not shut down");
            std::thread::sleep(Duration::from_millis(10));
        }
    }
}
impl Drop for Child {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}
fn repo(parent: &Path, name: &str) -> PathBuf {
    let dir = parent.join(name);
    fs::create_dir(&dir).unwrap();
    for args in [
        vec!["init", "-b", "trunk"],
        vec![
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.com",
            "commit",
            "--allow-empty",
            "-m",
            "fixture",
        ],
        vec![
            "remote",
            "add",
            "origin",
            &format!("https://example.com/test/{name}.git"),
        ],
        vec!["update-ref", "refs/remotes/origin/trunk", "HEAD"],
        vec![
            "symbolic-ref",
            "refs/remotes/origin/HEAD",
            "refs/remotes/origin/trunk",
        ],
    ] {
        let output = Command::new("git")
            .args(args)
            .current_dir(&dir)
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    }
    dir
}
#[test]
fn all_harnesses_and_aliases_update_live_header_then_join_and_cleanup() {
    for harness in ["claude-code", "pi", "codex", "opencode"] {
        for alias in [false, true] {
            let tmp = tempfile::tempdir_in(".tmp").unwrap();
            let root = tmp.path().canonicalize().unwrap();
            let first = repo(&root, "first");
            let second = repo(&root, "second");
            let third = repo(&root, "third");
            let executable = root.join(if alias {
                "custom-agent"
            } else if harness == "claude-code" {
                "claude"
            } else {
                harness
            });
            symlink(std::env::current_exe().unwrap(), &executable).unwrap();
            let (screen, slave) = Screen::new();
            let mut command = Command::new(env!("CARGO_BIN_EXE_agent-wrap"));
            command
                .args([
                    "--harness",
                    harness,
                    "--dir",
                    first.to_str().unwrap(),
                    "--scan-interval",
                    "20ms",
                    "--",
                ])
                .arg(executable)
                .args(["--exact", "fixture_child", "--nocapture"]);
            for key in [
                "CLAUDE_SESSIONS_DIR",
                "CLAUDE_PROJECTS_DIR",
                "PI_SESSIONS_DIR",
                "CODEX_SESSIONS_DIR",
                "XDG_CACHE_HOME",
                "XDG_STATE_HOME",
                "XDG_DATA_HOME",
            ] {
                let path = root.join(key);
                fs::create_dir(&path).unwrap();
                command.env(key, path);
            }
            let transcript = match harness {
                "pi" => root.join("PI_SESSIONS_DIR").join(agent_watcher::scanner::encode_pi_cwd(&first)).join("session.jsonl"),
                "codex" => root.join("CODEX_SESSIONS_DIR/rollout-2026-10-14T00-00-00-11111111-1111-4111-8111-111111111111.jsonl"),
                _ => root.join("transcript.jsonl"),
            };
            command
                .env("WRAP_FIXTURE", harness)
                .env("WRAP_TRANSCRIPT", transcript)
                .env("OPENCODE_DB_PATH", root.join("opencode.db"))
                .env_remove("AGENT_STATUS_TEST_MOCK_PROC")
                .stdin(Stdio::from(slave.try_clone().unwrap()))
                .stdout(Stdio::from(slave.try_clone().unwrap()))
                .stderr(Stdio::from(slave));
            let mut child = Child(command.spawn().unwrap());
            screen.wait("FRAME CHILD ONLINE");
            for dir in [&second, &third, &first] {
                screen.send(&format!("{}\n", dir.display()));
                screen.wait(&format!(
                    "{} · ",
                    dir.file_name().unwrap().to_str().unwrap()
                ));
                assert!(
                    !screen
                        .text
                        .lock()
                        .unwrap()
                        .contains("waiting for agent activity")
                );
            }
            screen.send("quit\n");
            let end = Instant::now() + Duration::from_secs(8);
            let status = loop {
                if let Some(status) = child.0.try_wait().unwrap() {
                    break status;
                }
                assert!(
                    Instant::now() < end,
                    "wrapper did not join: {}",
                    screen.text.lock().unwrap()
                );
                std::thread::sleep(Duration::from_millis(10));
            };
            assert_eq!(
                status.code(),
                Some(37),
                "{harness} alias={alias}: {}",
                screen.text.lock().unwrap()
            );
            assert_eq!(
                fs::read_dir(root.join("XDG_CACHE_HOME/agent-wrap"))
                    .unwrap()
                    .count(),
                0
            );
        }
    }
}

#[test]
fn signals_and_terminal_disconnect_join_owned_session_restore_modes_and_clean_state() {
    for signal in [
        Some(libc::SIGINT),
        Some(libc::SIGTERM),
        Some(libc::SIGHUP),
        None,
    ] {
        let tmp = tempfile::tempdir_in(".tmp").unwrap();
        let root = tmp.path().canonicalize().unwrap();
        let first = repo(&root, "first");
        let executable = root.join("pi");
        symlink(std::env::current_exe().unwrap(), &executable).unwrap();
        let (screen, slave) = Screen::new();
        let mut before = unsafe { std::mem::zeroed::<libc::termios>() };
        assert_eq!(
            unsafe { libc::tcgetattr(slave.as_raw_fd(), &mut before) },
            0
        );
        let mut command = Command::new(env!("CARGO_BIN_EXE_agent-wrap"));
        command
            .args([
                "--dir",
                first.to_str().unwrap(),
                "--scan-interval",
                "20ms",
                "--",
            ])
            .arg(executable)
            .args(["--exact", "fixture_child", "--nocapture"]);
        for key in ["PI_SESSIONS_DIR", "XDG_CACHE_HOME", "XDG_STATE_HOME"] {
            let path = root.join(key);
            fs::create_dir(&path).unwrap();
            command.env(key, path);
        }
        command
            .env("WRAP_FIXTURE", "pi")
            .env(
                "WRAP_TRANSCRIPT",
                root.join("PI_SESSIONS_DIR")
                    .join(agent_watcher::scanner::encode_pi_cwd(&first))
                    .join("session.jsonl"),
            )
            .env("WRAP_PID", root.join("child.pid"))
            .stdin(Stdio::from(slave.try_clone().unwrap()))
            .stdout(Stdio::from(slave.try_clone().unwrap()))
            .stderr(Stdio::from(slave.try_clone().unwrap()));
        let mut child = Child(command.spawn().unwrap());
        screen.wait("FRAME CHILD ONLINE");
        let leader: i32 = fs::read_to_string(root.join("child.pid"))
            .unwrap()
            .parse()
            .unwrap();
        if let Some(signal) = signal {
            assert_eq!(unsafe { libc::kill(child.0.id() as i32, signal) }, 0);
            child.wait();
            let mut after = unsafe { std::mem::zeroed::<libc::termios>() };
            assert_eq!(unsafe { libc::tcgetattr(slave.as_raw_fd(), &mut after) }, 0);
            assert_eq!(
                (
                    after.c_iflag,
                    after.c_oflag,
                    after.c_cflag,
                    after.c_lflag,
                    after.c_cc
                ),
                (
                    before.c_iflag,
                    before.c_oflag,
                    before.c_cflag,
                    before.c_lflag,
                    before.c_cc
                )
            );
            drop(screen);
        } else {
            drop(screen);
            child.wait();
        }
        let members = tui_frame::process::list(leader);
        assert!(members.error.is_none(), "{:?}", members.error);
        assert!(
            members.items.is_empty(),
            "session members remain: {:?}",
            members.items
        );
        assert_eq!(
            fs::read_dir(root.join("XDG_CACHE_HOME/agent-wrap"))
                .unwrap()
                .count(),
            0
        );
    }
}
