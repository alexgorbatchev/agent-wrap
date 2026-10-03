package session

import (
	"context"
	"sync"

	watcher "github.com/alexgorbatchev/agent-watcher"
	"github.com/alexgorbatchev/agent-wrap/internal/activity"
	"github.com/alexgorbatchev/agent-wrap/internal/header"
	"github.com/alexgorbatchev/agent-wrap/internal/project"
)

type tracker struct {
	mu            sync.Mutex
	state         header.State
	defaultBranch string
	latest        int64
	launchDir     string
	reported      map[string]string
}

func newTracker(state header.State, defaultBranch string, since int64) *tracker {
	return &tracker{state: state, defaultBranch: defaultBranch, latest: since, launchDir: state.Current.Dir, reported: make(map[string]string)}
}

func (t *tracker) apply(ctx context.Context, e watcher.Event) (header.State, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e.Timestamp < t.latest || e.EventType == "run_exited" {
		return t.state, false, nil
	}
	key := string(e.Harness) + ":" + e.SessionID
	if e.SubagentID != nil {
		key += ":" + *e.SubagentID
	}
	base := t.reported[key]
	if base == "" {
		base = t.launchDir
	}
	// Codex tool events inherit the watcher's launch CWD. Only turn_context
	// model_change events carry its subsequently reported task directory.
	if e.Harness == watcher.HarnessCodex && e.EventType != "model_change" && t.reported[key] != "" {
		e.CWD = base
	}
	baseChanged := e.CWD != "" && e.CWD != base
	if e.CWD != "" {
		base = e.CWD
	}
	t.reported[key] = base
	e.CWD = base
	dir := activity.Directory(e, t.state.Current.Dir)
	if baseChanged && !activity.IsTool(e) {
		dir = base
	}
	if !activity.IsTool(e) && e.EventType != "tool_result" && !baseChanged {
		return t.state, false, nil
	}
	p, err := project.Resolve(ctx, dir, t.defaultBranch)
	if err != nil {
		return t.state, false, err
	}
	t.latest = e.Timestamp
	if p == t.state.Current {
		return t.state, false, nil
	}
	t.state = t.state.Move(p)
	return t.state, true, nil
}
