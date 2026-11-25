use crate::{Result, activity, header::State, project};
use agent_watcher::{Acknowledgement, Event, Harness};
use std::{collections::HashMap, io, path::PathBuf};
use tokio_util::sync::CancellationToken;
use tui_frame::frame::{Controller, Edge};

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Payload {
    pub state: State,
    pub name: String,
}
pub struct Tracker {
    state: State,
    default_branch: String,
    latest: i64,
    launch_dir: PathBuf,
    reported: HashMap<(Harness, String, Option<String>), String>,
}
impl Tracker {
    pub fn new(state: State, default_branch: String, since: i64) -> Self {
        Self {
            launch_dir: state.current.dir.clone(),
            state,
            default_branch,
            latest: since,
            reported: HashMap::new(),
        }
    }
    pub fn apply(&mut self, cancel: &CancellationToken, mut event: Event) -> Result<(State, bool)> {
        if event.parsed.timestamp < self.latest || event.parsed.event_type == "run_exited" {
            return Ok((self.state.clone(), false));
        }
        let key = (
            event.harness,
            event.session_id.clone(),
            event.subagent_id.clone(),
        );
        let known = self.reported.get(&key);
        let mut base = known
            .cloned()
            .unwrap_or_else(|| self.launch_dir.to_string_lossy().into());
        if event.harness == Harness::Codex
            && event.parsed.event_type != "model_change"
            && known.is_some()
        {
            event.parsed.cwd = base.clone();
        }
        let base_changed = !event.parsed.cwd.is_empty() && event.parsed.cwd != base;
        if !event.parsed.cwd.is_empty() {
            base = event.parsed.cwd.clone();
        }
        event.parsed.cwd = base.clone();
        let tool = activity::is_tool(&event);
        let mut dir = activity::directory(&event, &self.state.current.dir);
        if base_changed && !tool {
            dir = PathBuf::from(&base);
        }
        if !tool && event.parsed.event_type != "tool_result" && !base_changed {
            self.reported.insert(key, base);
            return Ok((self.state.clone(), false));
        }
        let context = project::resolve(cancel, &dir, &self.default_branch)?;
        // Commit the reported base only after resolution succeeds. A transient bad
        // path cannot contaminate future relative tool paths for this session.
        self.reported.insert(key, base);
        self.latest = event.parsed.timestamp;
        if context == self.state.current {
            return Ok((self.state.clone(), false));
        }
        self.state = self.state.moved(context);
        Ok((self.state.clone(), true))
    }
}
pub struct Observer {
    pub leader: i32,
    pub since: i64,
    pub tracker: Tracker,
    pub controller: Controller<Payload>,
    pub name: String,
    delivered: Option<Payload>,
}
impl Observer {
    pub fn new(
        leader: i32,
        since: i64,
        tracker: Tracker,
        controller: Controller<Payload>,
        name: String,
    ) -> Self {
        Self {
            leader,
            since,
            tracker,
            controller,
            name,
            delivered: None,
        }
    }

    pub fn handle(
        &mut self,
        cancel: &CancellationToken,
        event: Event,
        ack: Option<Acknowledgement>,
    ) -> io::Result<()> {
        if activity::owned_pid(event.pid, self.leader) && event.parsed.timestamp >= self.since {
            match self.tracker.apply(cancel, event) {
                Ok((state, _)) => {
                    let payload = Payload {
                        state,
                        name: self.name.clone(),
                    };
                    // The tracker advances on a delivery failure. Keep the last
                    // accepted payload so the same event can retry publication.
                    deliver(&mut self.delivered, payload, |payload| {
                        self.controller.invalidate(Edge::Header, payload)
                    })?;
                }
                Err(error) => tracing::warn!(err=%error,"context_resolution_failed"),
            }
        }
        if let Some(ack) = ack {
            ack.acknowledge()?;
        }
        Ok(())
    }
}

fn deliver(
    delivered: &mut Option<Payload>,
    payload: Payload,
    invalidate: impl FnOnce(Payload) -> tui_frame::Result<()>,
) -> io::Result<()> {
    if delivered.as_ref() == Some(&payload) {
        return Ok(());
    }
    match invalidate(payload.clone()) {
        Ok(()) => *delivered = Some(payload),
        Err(tui_frame::Error::SessionClosed) => {}
        Err(error) => return Err(io::Error::other(error)),
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn duplicate_headers_are_skipped_and_failed_updates_retry() {
        let payload = Payload {
            state: State::new(project::Context::default()),
            name: "Test".into(),
        };
        let mut delivered = None;
        deliver(&mut delivered, payload.clone(), |_| Ok(())).unwrap();
        deliver(&mut delivered, payload.clone(), |_| {
            panic!("duplicate invalidation")
        })
        .unwrap();
        let mut changed = payload.clone();
        changed.state.current.branch = "next".into();
        assert!(
            deliver(&mut delivered, changed.clone(), |_| {
                Err(tui_frame::Error::RegionNotConfigured)
            })
            .is_err()
        );
        assert_eq!(delivered.as_ref(), Some(&payload));
        deliver(&mut delivered, changed.clone(), |_| Ok(())).unwrap();
        assert_eq!(delivered.as_ref(), Some(&changed));
    }
}
