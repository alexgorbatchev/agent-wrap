use crate::{Error, Result};
use agent_watcher::{
    Event, Harness,
    scanner::{ProcessInfo, ProcessSnapshot, SnapshotProvider},
};
use path_clean::PathClean;
use std::{
    ffi::OsStr,
    io,
    path::{Path, PathBuf},
};
use tokio_util::sync::CancellationToken;
pub fn harness(executable: &OsStr, explicit: &str) -> Result<Harness> {
    let inferred = Path::new(executable)
        .file_name()
        .unwrap_or(executable)
        .to_string_lossy();
    match if explicit.is_empty() {
        inferred.as_ref()
    } else {
        explicit
    } {
        "claude" | "claude-code" => Ok(Harness::ClaudeCode),
        "pi" => Ok(Harness::Pi),
        "codex" => Ok(Harness::Codex),
        "opencode" => Ok(Harness::Opencode),
        "agy" | "antigravity" => Ok(Harness::Agy),
        other => Err(Error::Invalid(format!(
            "unknown agent {other:?}; select --harness claude-code, pi, codex, opencode, or agy"
        ))),
    }
}
pub fn is_tool(event: &Event) -> bool {
    matches!(
        event.parsed.event_type.as_str(),
        "tool_call" | "file_modification"
    )
}
fn existing(path: &str, base: &Path, file: bool) -> Option<PathBuf> {
    if path.is_empty() || path.contains(['\0', '\r', '\n']) || path.contains("://") {
        return None;
    }
    let path = base.join(path).clean();
    if path.is_dir() {
        return Some(path);
    }
    if file {
        path.parent().filter(|p| p.is_dir()).map(Path::to_path_buf)
    } else {
        None
    }
}
pub fn directory(event: &Event, previous: &Path) -> PathBuf {
    if !is_tool(event) {
        return previous.into();
    }
    let base = if event.parsed.cwd.is_empty() {
        previous
    } else {
        Path::new(&event.parsed.cwd)
    };
    if let Some(input) = &event.parsed.data.tool_input {
        for (keys, file) in [
            (
                &[
                    "workdir",
                    "cwd",
                    "Cwd",
                    "WorkingDirectory",
                    "working_dir",
                    "workingDir",
                ][..],
                false,
            ),
            (
                &[
                    "file_path",
                    "filePath",
                    "notebook_path",
                    "path",
                    "Path",
                    "TargetFile",
                    "target_file",
                    "AbsolutePath",
                    "absolute_path",
                ][..],
                true,
            ),
        ] {
            for key in keys {
                if let Some(path) = input
                    .get(*key)
                    .and_then(|v| v.as_str())
                    .and_then(|p| existing(p, base, file))
                {
                    return path;
                }
            }
        }
    }
    event
        .parsed
        .data
        .file_modification
        .as_ref()
        .and_then(|f| existing(&f.file_path, base, true))
        .unwrap_or_else(|| base.into())
}
pub fn owned_pid(pid: i32, leader: i32) -> bool {
    pid > 0 && leader > 0 && unsafe { libc::getsid(pid) } == leader
}
pub struct ScopedProvider {
    pub leader: i32,
    pub harness: Harness,
}
impl SnapshotProvider for ScopedProvider {
    fn snapshot(&self, cancel: &CancellationToken) -> io::Result<ProcessSnapshot> {
        let mut result =
            ProcessSnapshot::capture_filtered(cancel, |pid| owned_pid(pid, self.leader))?;
        if self.harness != Harness::ClaudeCode
            && let Some(process) = result.process(self.leader).filter(|p| p.alive)
        {
            let native = tui_frame::process::read(self.leader);
            if native.session_id.value == Some(self.leader)
                && let Some(cwd) = native.directory.value
            {
                result.declare_candidate(
                    self.harness,
                    ProcessInfo {
                        pid: self.leader,
                        create_time: process.create_time,
                        cwd,
                    },
                )?;
            }
        }
        if cancel.is_cancelled() {
            return Err(io::Error::new(
                io::ErrorKind::Interrupted,
                "session scan cancelled",
            ));
        }
        Ok(result)
    }
}
