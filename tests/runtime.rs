use agent_wrap::{
    files::Locations,
    runtime::{self, Options},
};
use std::{
    os::fd::{AsFd, AsRawFd, FromRawFd, OwnedFd},
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    time::Duration,
};
use tui_frame::{
    Cancellation,
    terminal::{Effect, Options as TerminalOptions, Terminal},
};
fn pty() -> (OwnedFd, OwnedFd) {
    let (mut m, mut s) = (-1, -1);
    let size = libc::winsize {
        ws_col: 80,
        ws_row: 12,
        ws_xpixel: 800,
        ws_ypixel: 240,
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
    unsafe { (OwnedFd::from_raw_fd(m), OwnedFd::from_raw_fd(s)) }
}
#[test]
fn real_children_preserve_arguments_status_terminal_and_owned_cleanup() {
    let dir = tempfile::tempdir_in(".tmp").unwrap();
    let root = dir.path().canonicalize().unwrap();
    let locations = Locations {
        cache: root.join("cache"),
        state: root.join("state"),
    };
    let (master, slave) = pty();
    let done = Arc::new(AtomicBool::new(false));
    let stop = done.clone();
    let outer = std::thread::spawn(move || {
        let mut term = Terminal::new(TerminalOptions::new(80, 12)).unwrap();
        let mut bytes = [0; 16384];
        while !stop.load(Ordering::Acquire) {
            let mut fd = libc::pollfd {
                fd: master.as_raw_fd(),
                events: libc::POLLIN,
                revents: 0,
            };
            unsafe { libc::poll(&mut fd, 1, 10) };
            if fd.revents & libc::POLLIN != 0 {
                let n = unsafe {
                    libc::read(master.as_raw_fd(), bytes.as_mut_ptr().cast(), bytes.len())
                };
                if n > 0 {
                    term.write(&bytes[..n as usize]).unwrap();
                    for effect in term.take_effects() {
                        if let Effect::Reply(reply) = effect {
                            unsafe {
                                libc::write(master.as_raw_fd(), reply.as_ptr().cast(), reply.len())
                            };
                        }
                    }
                }
            }
        }
        master
    });
    for (script, code) in [
        (
            "test \"$1\" = '--flag with spaces' || exit 99; printf CHILD; exit 37",
            37,
        ),
        ("kill -TERM $$", 143),
        ("exit 0", 0),
    ] {
        let opts = Options {
            harness: "pi".into(),
            directory: root.clone(),
            scan_interval: Duration::from_millis(10),
            locations: Some(locations.clone()),
            ..Default::default()
        };
        let cancel = Cancellation::new();
        let timeout = cancel.clone();
        let watchdog = std::thread::spawn(move || {
            for _ in 0..400 {
                if timeout.is_cancelled() {
                    return;
                }
                std::thread::sleep(Duration::from_millis(10));
            }
            timeout.cancel();
        });
        let result = runtime::run(
            ["/bin/sh", "-c", script, "sh", "--flag with spaces"]
                .map(Into::into)
                .to_vec(),
            opts,
            slave.as_fd(),
            slave.as_fd(),
            &cancel,
        );
        cancel.cancel();
        watchdog.join().unwrap();
        assert_eq!(result.unwrap(), code);
        assert_eq!(std::fs::read_dir(&locations.cache).unwrap().count(), 0);
    }
    done.store(true, Ordering::Release);
    outer.join().unwrap();
    let null = std::fs::File::open("/dev/null").unwrap();
    assert!(
        runtime::run(
            vec!["pi".into()],
            Options::default(),
            null.as_fd(),
            null.as_fd(),
            &Cancellation::new()
        )
        .is_err()
    );
    let canceled = Cancellation::new();
    canceled.cancel();
    assert!(
        runtime::run(
            Vec::new(),
            Options::default(),
            null.as_fd(),
            null.as_fd(),
            &canceled
        )
        .is_err()
    );
}
