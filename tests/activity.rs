use agent_parser::{EventData, ParsedEvent};
use agent_watcher::{Event, Harness, scanner::SnapshotProvider};
use agent_wrap::activity;
use std::{
    fs,
    os::unix::process::CommandExt,
    process::{Child, Command},
};
use tokio_util::sync::CancellationToken;
fn event(harness: Harness, kind: &str, cwd: &std::path::Path, input: serde_json::Value) -> Event {
    Event {
        harness,
        parsed: ParsedEvent {
            event_type: kind.into(),
            cwd: cwd.to_string_lossy().into(),
            data: EventData {
                tool_input: input.as_object().cloned(),
                ..Default::default()
            },
            ..Default::default()
        },
        pid: 0,
        session_id: String::new(),
        subagent_id: None,
        harness_version: String::new(),
    }
}
#[test]
fn directory_activity_uses_explicit_targets_without_executing_shell_commands() {
    fs::create_dir_all(".tmp").unwrap();
    let temp = tempfile::tempdir_in(".tmp").unwrap();
    let base = fs::canonicalize(temp.path()).unwrap();
    fs::create_dir(base.join("child")).unwrap();
    let e = event(
        Harness::Pi,
        "tool_call",
        &base,
        serde_json::json!({"workdir":"child","file_path":"new"}),
    );
    assert_eq!(activity::directory(&e, &base), base.join("child"));
    for key in ["file_path", "filePath", "notebook_path", "path"] {
        let e = event(
            Harness::Codex,
            "tool_call",
            &base,
            serde_json::json!({key:"child/new.txt"}),
        );
        assert_eq!(activity::directory(&e, &base), base.join("child"));
    }
    for path in ["https://example.invalid/child", "bad\nname", "\u{0}", ""] {
        let e = event(
            Harness::Pi,
            "tool_call",
            &base,
            serde_json::json!({"path":path}),
        );
        assert_eq!(activity::directory(&e, &base), base);
    }
    let e = event(
        Harness::Opencode,
        "tool_use",
        &base,
        serde_json::json!({"cwd":"child"}),
    );
    assert_eq!(activity::directory(&e, &base), base.join("child"));
    let e = event(
        Harness::Pi,
        "assistant_message",
        &base,
        serde_json::json!({"path":"child"}),
    );
    assert_eq!(
        activity::directory(&e, &base.join("previous")),
        base.join("previous")
    );
    let e = event(
        Harness::Pi,
        "tool_call",
        &base,
        serde_json::json!({"cmd":"cd child"}),
    );
    assert_eq!(activity::directory(&e, &base), base);
    assert_eq!(
        activity::harness(std::ffi::OsStr::new("/opt/claude"), "").unwrap(),
        Harness::ClaudeCode
    );
    assert_eq!(
        activity::harness(std::ffi::OsStr::new("alias"), "codex").unwrap(),
        Harness::Codex
    );
    assert!(activity::harness(std::ffi::OsStr::new("unknown"), "").is_err());
}
struct OwnedChild(Child);
impl Drop for OwnedChild {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}
#[test]
fn real_process_scope_excludes_outsiders_and_declares_alias_without_forging_metadata() {
    let mut command = Command::new("sleep");
    command.arg("30");
    unsafe {
        command.pre_exec(|| {
            if libc::setsid() < 0 {
                Err(std::io::Error::last_os_error())
            } else {
                Ok(())
            }
        });
    }
    let child = OwnedChild(command.spawn().unwrap());
    let pid = i32::try_from(child.0.id()).unwrap();
    assert!(activity::owned_pid(pid, pid));
    assert!(!activity::owned_pid(0, pid));
    assert!(!activity::owned_pid(std::process::id() as i32, pid));
    for harness in [
        Harness::Pi,
        Harness::Codex,
        Harness::Opencode,
        Harness::ClaudeCode,
    ] {
        let provider = activity::ScopedProvider {
            leader: pid,
            harness,
        };
        let snapshot = provider.snapshot(&CancellationToken::new()).unwrap();
        assert!(
            snapshot
                .processes()
                .all(|p| activity::owned_pid(p.pid, pid))
        );
        assert_eq!(snapshot.process(pid).unwrap().name, "sleep");
        if harness == Harness::Pi {
            assert_eq!(snapshot.pi_processes()[0].pid, pid);
        }
        if harness == Harness::Codex {
            assert_eq!(snapshot.codex_processes()[0].pid, pid);
        }
        if harness == Harness::Opencode {
            assert_eq!(snapshot.opencode_processes()[0].pid, pid);
        }
    }
    let cancel = CancellationToken::new();
    cancel.cancel();
    assert!(
        activity::ScopedProvider {
            leader: pid,
            harness: Harness::Pi
        }
        .snapshot(&cancel)
        .is_err()
    );
}
