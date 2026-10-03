package session

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	watcher "github.com/alexgorbatchev/agent-watcher"
	"github.com/alexgorbatchev/agent-wrap/internal/activity"
	frame "github.com/alexgorbatchev/go-tui-frame"
)

type observer struct {
	mu      sync.Mutex
	leader  int
	since   int64
	tracker *tracker
	app     *frame.Frame[payload]
	name    string
	logger  *slog.Logger
}

// Handle serializes state transitions and drawing across concurrent transcripts.
func (o *observer) handle(ctx context.Context, e watcher.Event, ack func()) (err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	defer func() {
		if err == nil && ack != nil {
			ack()
		}
	}()
	if !activity.OwnedPID(e.PID, o.leader) || e.Timestamp < o.since {
		return nil
	}
	next, _, updateErr := o.tracker.apply(ctx, e)
	if updateErr != nil {
		o.logger.Warn("context_resolution_failed", "err", updateErr)
		return nil
	}
	if err := o.app.InvalidateHeader(payload{State: next, Name: o.name}); err != nil && !errors.Is(err, frame.ErrSessionClosed) {
		return err
	}
	return nil
}
