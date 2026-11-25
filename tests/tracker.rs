use agent_watcher::{Event, Harness};
use agent_wrap::{header::State, project, tracker::Tracker};
use tokio_util::sync::CancellationToken;
fn event(harness: Harness, cwd: &std::path::Path, time: i64, kind: &str) -> Event {
    Event {
        harness,
        session_id: "one".into(),
        parsed: agent_parser::ParsedEvent {
            cwd: cwd.to_string_lossy().into(),
            timestamp: time,
            event_type: kind.into(),
            ..Default::default()
        },
        pid: 0,
        harness_version: String::new(),
        subagent_id: None,
    }
}
#[test]
fn reported_bases_are_session_scoped_and_failed_resolution_is_transactional() {
    let tmp = tempfile::tempdir_in(".tmp").unwrap();
    let base = tmp.path().canonicalize().unwrap();
    for name in ["src", "docs"] {
        std::fs::create_dir(base.join(name)).unwrap();
    }
    let cancel = CancellationToken::new();
    let initial = project::resolve(&cancel, &base, "").unwrap();
    let mut tracker = Tracker::new(State::new(initial), "".into(), 1);
    let mut e = event(Harness::Codex, &base, 2, "tool_call");
    e.parsed.data.tool_input =
        Some(serde_json::from_value(serde_json::json!({"path":"src/new.rs"})).unwrap());
    assert_eq!(
        tracker.apply(&cancel, e.clone()).unwrap().0.current.dir,
        base.join("src")
    );
    e.parsed.event_type = "thinking".into();
    e.parsed.timestamp = 3;
    assert!(!tracker.apply(&cancel, e.clone()).unwrap().1);
    e.parsed.event_type = "tool_call".into();
    e.parsed.data.tool_input =
        Some(serde_json::from_value(serde_json::json!({"path":"docs/new.rs"})).unwrap());
    assert_eq!(
        tracker.apply(&cancel, e.clone()).unwrap().0.current.dir,
        base.join("docs")
    );
    e.parsed.timestamp = 0;
    assert!(!tracker.apply(&cancel, e).unwrap().1);
    let bad = event(Harness::Pi, &base.join("missing"), 4, "model_change");
    assert!(tracker.apply(&cancel, bad).is_err());
    // A failed task directory must not poison the next event's relative base.
    let mut next = event(Harness::Pi, std::path::Path::new(""), 5, "tool_call");
    next.parsed.data.tool_input =
        Some(serde_json::from_value(serde_json::json!({"path":"src/new.rs"})).unwrap());
    assert_eq!(
        tracker.apply(&cancel, next).unwrap().0.current.dir,
        base.join("src")
    );
    let mut moved = event(Harness::Codex, &base.join("docs"), 6, "model_change");
    assert!(tracker.apply(&cancel, moved.clone()).unwrap().1);
    moved.parsed.event_type = "tool_call".into();
    moved.parsed.cwd = base.to_string_lossy().into();
    assert_eq!(
        tracker.apply(&cancel, moved.clone()).unwrap().0.current.dir,
        base.join("docs")
    );
    moved.subagent_id = Some("child".into());
    assert_eq!(
        tracker.apply(&cancel, moved.clone()).unwrap().0.current.dir,
        base
    );
    moved.parsed.event_type = "run_exited".into();
    moved.parsed.cwd = "/missing".into();
    assert!(!tracker.apply(&cancel, moved).unwrap().1);
}
