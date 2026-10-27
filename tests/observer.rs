use agent_watcher::{Acknowledgement, Event, Harness};
use agent_wrap::{
    header, project,
    tracker::{Observer, Payload, Tracker},
};
use std::{
    io,
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
};
use tokio_util::sync::CancellationToken;
use tui_frame::frame::Frame;
#[test]
fn delivery_acknowledges_only_accepted_or_deliberately_skipped_events() {
    let tmp = tempfile::tempdir_in(".tmp").unwrap();
    let cancel = CancellationToken::new();
    let context = project::resolve(&cancel, tmp.path(), "").unwrap();
    let state = header::State::new(context.clone());
    let frame = Frame::new(
        portable_pty::CommandBuilder::new("true"),
        Payload {
            state: state.clone(),
            name: "Pi".into(),
        },
    );
    let mut observer = Observer {
        leader: unsafe { libc::getsid(0) },
        since: 1,
        tracker: Tracker::new(state, "".into(), 1),
        controller: frame.controller(),
        name: "Pi".into(),
    };
    let acked = Arc::new(AtomicUsize::new(0));
    let counter = acked.clone();
    let ack = Acknowledgement::new(move || {
        counter.fetch_add(1, Ordering::Relaxed);
        Ok(())
    });
    let mut event = Event {
        harness: Harness::Pi,
        session_id: "one".into(),
        harness_version: String::new(),
        subagent_id: None,
        pid: std::process::id() as i32,
        parsed: agent_parser::ParsedEvent {
            event_type: "tool_call".into(),
            timestamp: 2,
            cwd: context.dir.to_string_lossy().into(),
            ..Default::default()
        },
    };
    assert!(
        observer
            .handle(&cancel, event.clone(), Some(ack.clone()))
            .is_err()
    );
    assert_eq!(acked.load(Ordering::Relaxed), 0);
    frame.header(header::ROWS, |_| {}).unwrap();
    observer
        .handle(&cancel, event.clone(), Some(ack.clone()))
        .unwrap();
    assert_eq!(acked.load(Ordering::Relaxed), 1);
    event.pid = 0;
    observer
        .handle(&cancel, event.clone(), Some(ack.clone()))
        .unwrap();
    event.pid = std::process::id() as i32;
    event.parsed.timestamp = 0;
    observer
        .handle(&cancel, event.clone(), Some(ack.clone()))
        .unwrap();
    assert_eq!(acked.load(Ordering::Relaxed), 3);
    event.parsed.timestamp = 3;
    event.parsed.cwd = context.dir.join("missing").to_string_lossy().into();
    observer
        .handle(&cancel, event.clone(), Some(ack.clone()))
        .unwrap();
    assert_eq!(acked.load(Ordering::Relaxed), 4);
    event.parsed.cwd = context.dir.to_string_lossy().into();
    drop(frame);
    observer.handle(&cancel, event.clone(), Some(ack)).unwrap();
    assert_eq!(acked.load(Ordering::Relaxed), 5);
    assert!(
        observer
            .handle(
                &cancel,
                event,
                Some(Acknowledgement::new(|| Err(
                    io::ErrorKind::PermissionDenied.into()
                )))
            )
            .is_err()
    );
}
