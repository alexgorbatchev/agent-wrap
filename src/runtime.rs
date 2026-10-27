use crate::{
    Error, Result, activity,
    files::{Locations, RuntimeFiles},
    header, project,
    tracker::{Observer, Payload, Tracker},
};
use agent_watcher::{Handler, Harness};
use cli_command_kit::Mode;
use portable_pty::CommandBuilder;
use std::{
    cell::RefCell,
    ffi::OsString,
    io,
    os::fd::{AsRawFd, BorrowedFd},
    path::PathBuf,
    sync::{Arc, Mutex},
    thread,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio_util::sync::CancellationToken;
use tui_frame::{
    Cancellation,
    frame::{EventKind, Frame},
};
thread_local! {static DISPATCH:RefCell<Option<tracing::dispatcher::DefaultGuard>>=const{RefCell::new(None)};}
#[derive(Clone, Debug)]
pub struct Options {
    pub harness: String,
    pub directory: PathBuf,
    pub default_branch: String,
    pub name: String,
    pub log_file: Option<PathBuf>,
    pub scan_interval: Duration,
    pub locations: Option<Locations>,
    pub mode: Mode,
}
impl Default for Options {
    fn default() -> Self {
        Self {
            harness: String::new(),
            directory: PathBuf::new(),
            default_branch: String::new(),
            name: String::new(),
            log_file: None,
            scan_interval: Duration::from_secs(2),
            locations: None,
            mode: Mode::from_env(),
        }
    }
}
struct Signals {
    handle: signal_hook::iterator::Handle,
    worker: Option<thread::JoinHandle<()>>,
}
impl Signals {
    fn new(cancel: Cancellation, watch: CancellationToken) -> io::Result<Self> {
        let mut signals =
            signal_hook::iterator::Signals::new([libc::SIGINT, libc::SIGTERM, libc::SIGHUP])?;
        let handle = signals.handle();
        let worker = thread::spawn(move || {
            if signals.forever().next().is_some() {
                cancel.cancel();
                watch.cancel();
            }
        });
        Ok(Self {
            handle,
            worker: Some(worker),
        })
    }
}
impl Drop for Signals {
    fn drop(&mut self) {
        self.handle.close();
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}
pub fn display_name(harness: Harness) -> &'static str {
    match harness {
        Harness::ClaudeCode => "Claude Code",
        Harness::Pi => "Pi",
        Harness::Codex => "Codex",
        Harness::Opencode => "OpenCode",
    }
}
pub fn run(
    args: Vec<OsString>,
    mut options: Options,
    input: BorrowedFd<'_>,
    output: BorrowedFd<'_>,
    cancel: &Cancellation,
) -> Result<u32> {
    if cancel.is_cancelled() {
        return Err(io::Error::new(io::ErrorKind::Interrupted, "operation cancelled").into());
    }
    let Some(executable) = args.first() else {
        return Err(Error::Invalid("an agent command is required".into()));
    };
    activity::harness(executable, &options.harness)?;
    if options.directory.as_os_str().is_empty() {
        options.directory = std::env::current_dir()?;
    }
    let watch_cancel = CancellationToken::new();
    let _signals = Signals::new(cancel.clone(), watch_cancel.clone())?;
    let initial = project::resolve(&watch_cancel, &options.directory, &options.default_branch)?;
    if unsafe { libc::isatty(input.as_raw_fd()) } != 1
        || unsafe { libc::isatty(output.as_raw_fd()) } != 1
    {
        return Err(Error::Invalid(
            "agent-wrap requires an interactive terminal".into(),
        ));
    }
    let locations = options
        .locations
        .take()
        .map(Ok)
        .unwrap_or_else(Locations::from_env)?;
    let files = RuntimeFiles::open(&locations, options.log_file.as_deref(), options.mode)?;
    let result = run_owned(
        args,
        options,
        (input, output),
        cancel,
        &watch_cancel,
        initial,
        &files,
    );
    let close = files.close();
    match (result, close) {
        (Ok(code), Ok(())) => Ok(code),
        (Err(error), Ok(())) => Err(error),
        (Ok(_), Err(error)) => Err(error.into()),
        (Err(error), Err(close)) => Err(Error::Multiple(format!("{error}; {close}"))),
    }
}
fn run_owned(
    args: Vec<OsString>,
    options: Options,
    terminal: (BorrowedFd<'_>, BorrowedFd<'_>),
    cancel: &Cancellation,
    watch_cancel: &CancellationToken,
    initial: project::Context,
    files: &RuntimeFiles,
) -> Result<u32> {
    let harness = activity::harness(&args[0], &options.harness)?;
    let name = if options.name.is_empty() {
        display_name(harness).into()
    } else {
        options.name
    };
    let state = header::State::new(initial);
    let since = i64::try_from(
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(io::Error::other)?
            .as_millis(),
    )
    .map_err(io::Error::other)?;
    let mut command = CommandBuilder::new(&args[0]);
    command.args(&args[1..]);
    command.cwd(&state.current.dir);
    let frame = Frame::try_new(
        command,
        Payload {
            state: state.clone(),
            name: format!("{name} · waiting for agent activity"),
        },
    )?;
    frame.header(header::ROWS, |draw| {
        header::draw(draw.view, &draw.data.state, &draw.data.name)
    })?;
    frame.terminal(terminal.0, terminal.1)?;
    let (started, ready) = tokio::sync::oneshot::channel();
    let mut started = Some(started);
    frame.observe_events([EventKind::Started], move |event| {
        if let Some(snapshot) = event.snapshot
            && let Some(sender) = started.take()
        {
            let _ = sender.send(snapshot.child.pid);
        }
    })?;
    let dispatch = files.logging.dispatch().clone();
    let thread_dispatch = dispatch.clone();
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .worker_threads(2)
        .enable_all()
        .on_thread_start(move || {
            DISPATCH.with(|slot| {
                *slot.borrow_mut() = Some(tracing::dispatcher::set_default(&thread_dispatch))
            })
        })
        .on_thread_stop(|| DISPATCH.with(|slot| drop(slot.borrow_mut().take())))
        .build()?;
    let mut config = files.config();
    config.harnesses = vec![harness];
    config.scan_interval = options.scan_interval;
    let controller = frame.controller();
    let token = watch_cancel.clone();
    let frame_cancel = cancel.clone();
    let worker = thread::spawn(move || {
        let _default = tracing::dispatcher::set_default(&dispatch);
        runtime.block_on(async move {
            let leader = tokio::select! {
                biased;
                _ = token.cancelled() => return Ok(()),
                result = ready => match result {
                    Ok(pid) => pid,
                    Err(_) => return Ok(()),
                }
            };
            config.process_snapshot_provider =
                Arc::new(activity::ScopedProvider { leader, harness });
            let observer = Arc::new(Mutex::new(Observer {
                leader,
                since,
                tracker: Tracker::new(state, options.default_branch, since),
                controller,
                name,
            }));
            let handler: Handler = Arc::new(move |cancel, event, ack| {
                let observer = observer.clone();
                let cancel = cancel.clone();
                Box::pin(async move {
                    tokio::task::spawn_blocking(move || {
                        observer
                            .lock()
                            .map_err(|error| io::Error::other(error.to_string()))?
                            .handle(&cancel, event, ack)
                    })
                    .await
                    .map_err(io::Error::other)?
                })
            });
            let result = agent_watcher::run(&token, config, handler).await;
            if result.is_err() {
                frame_cancel.cancel();
            }
            result
        })
    });
    let result = frame.run(cancel);
    watch_cancel.cancel();
    let watch = worker
        .join()
        .map_err(|_| io::Error::other("observation worker panicked"))?;
    let mut errors = Vec::new();
    if let Err(error) = watch
        && error.kind() != io::ErrorKind::Interrupted
    {
        errors.push(error.to_string());
    }
    let outcome = match result {
        Ok(result) => result,
        Err(failure) => {
            errors.push(failure.error.to_string());
            failure.result
        }
    };
    for error in [outcome.drain_error, outcome.cleanup_error]
        .into_iter()
        .flatten()
    {
        errors.push(error.to_string());
    }
    if !errors.is_empty() {
        return Err(Error::Multiple(errors.join("; ")));
    }
    Ok(outcome
        .process_state
        .map(|status| status.exit_code())
        .unwrap_or(1))
}
