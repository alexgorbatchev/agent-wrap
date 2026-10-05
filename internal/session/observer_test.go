package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	parser "github.com/alexgorbatchev/agent-parser"
	watcher "github.com/alexgorbatchev/agent-watcher"
	"github.com/alexgorbatchev/agent-wrap/internal/header"
	"github.com/alexgorbatchev/agent-wrap/internal/logging"
	"github.com/alexgorbatchev/agent-wrap/internal/project"
	frame "github.com/alexgorbatchev/go-tui-frame/v2"
	"golang.org/x/sys/unix"
)

func TestObserverAcknowledgesOnlyAcceptedEvents(t *testing.T) {
	ctx := context.Background()
	o, log := newTestObserver(t)
	initial := o.tracker.state.Current
	app := o.app
	e := watcher.Event{PID: os.Getpid(), ParsedEvent: parser.ParsedEvent{Timestamp: 2, EventType: "tool_call", CWD: initial.Dir}}
	acked := false
	err := o.handle(ctx, e, func() { acked = true })
	if !errors.Is(err, frame.ErrRegionNotConfigured) || acked {
		t.Fatalf("failed delivery acknowledged: err=%v ack=%v", err, acked)
	}
	app.Header(header.Rows, func(frame.DrawContext[payload]) {})
	if err := o.handle(ctx, e, func() { acked = true }); err != nil || !acked {
		t.Fatal("accepted delivery not acknowledged", err)
	}
	e.PID = 0
	acked = false
	if err := o.handle(ctx, e, func() { acked = true }); err != nil || !acked {
		t.Fatal("outside event not acknowledged", err)
	}
	e.PID = os.Getpid()
	e.Timestamp = 0
	acked = false
	if err := o.handle(ctx, e, func() { acked = true }); err != nil || !acked {
		t.Fatal("stale event not acknowledged", err)
	}
	e.Timestamp = 3
	e.CWD = "/directory/does/not/exist"
	acked = false
	if err := o.handle(ctx, e, func() { acked = true }); err != nil || !acked || !bytes.Contains(log.Bytes(), []byte("context_resolution_failed")) {
		t.Fatal("resolution error not logged and skipped", err, log.String())
	}
}

func newTestObserver(t *testing.T) (*observer, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	initial, err := project.Resolve(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	state := header.New(initial)
	leader, err := unix.Getsid(0)
	if err != nil {
		t.Fatal(err)
	}
	log := new(bytes.Buffer)
	app := frame.New(exec.Command("true"), payload{State: state})
	return &observer{leader: leader, since: 1, tracker: newTracker(state, "", 1), app: app, name: "Test agent", logger: logging.New(log, "agent-wrap")}, log
}

func TestObserverDeliversOnlyChangedPayloads(t *testing.T) {
	type step struct {
		name       string
		eventType  string
		otherDir   bool
		configured bool
		wantErr    bool
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{"announce and skip unchanged events", []step{
			{name: "first non-tool delivery", eventType: "text", configured: true},
			{name: "unchanged non-tool", eventType: "text"},
			{name: "unchanged tool", eventType: "tool_call"},
			{name: "changed tool", eventType: "tool_call", otherDir: true, wantErr: true},
		}},
		{"retry failed first announcement", []step{
			{name: "first attempt", eventType: "text", wantErr: true},
			{name: "redelivery", eventType: "text", wantErr: true},
			{name: "accepted retry", eventType: "text", configured: true},
			{name: "skip accepted payload", eventType: "text"},
		}},
		{"retry context change after announcement", []step{
			{name: "announce", eventType: "text", configured: true},
			{name: "changed context failure", eventType: "tool_call", otherDir: true, wantErr: true},
			{name: "redelivery still attempts", eventType: "tool_call", otherDir: true, wantErr: true},
			{name: "accepted retry", eventType: "tool_call", otherDir: true, configured: true},
			{name: "skip accepted context", eventType: "tool_call", otherDir: true},
		}},
		{"return to delivered context after failure", []step{
			{name: "announce", eventType: "text", configured: true},
			{name: "changed context failure", eventType: "tool_call", otherDir: true, wantErr: true},
			{name: "return to accepted payload", eventType: "tool_call"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, _ := newTestObserver(t)
			initial := o.tracker.state.Current.Dir
			other := t.TempDir()
			for _, s := range tt.steps {
				t.Run(s.name, func(t *testing.T) {
					// Swap real controllers so a missing region exposes every attempted delivery.
					o.app = frame.New(exec.Command("true"), payload{})
					if s.configured {
						o.app.Header(header.Rows, func(frame.DrawContext[payload]) {})
					}
					dir := initial
					if s.otherDir {
						dir = other
					}
					e := watcher.Event{PID: os.Getpid(), ParsedEvent: parser.ParsedEvent{Timestamp: 2, EventType: s.eventType, CWD: dir}}
					acked := false
					err := o.handle(context.Background(), e, func() { acked = true })
					if errors.Is(err, frame.ErrRegionNotConfigured) != s.wantErr || (err != nil && !s.wantErr) || acked == s.wantErr {
						t.Fatalf("handle: err=%v ack=%v, want delivery error=%v ack=%v", err, acked, s.wantErr, !s.wantErr)
					}
				})
			}
		})
	}
}

func TestObserverIgnoredEventsDoNotAnnounce(t *testing.T) {
	tests := []struct {
		name      string
		pid       int
		timestamp int64
	}{
		{"outside child session", 0, 2},
		{"pre-launch history", os.Getpid(), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, _ := newTestObserver(t)
			e := watcher.Event{PID: tt.pid, ParsedEvent: parser.ParsedEvent{Timestamp: tt.timestamp, EventType: "text"}}
			acked := false
			if err := o.handle(context.Background(), e, func() { acked = true }); err != nil || !acked {
				t.Fatalf("ignored event: err=%v ack=%v", err, acked)
			}
			e.PID, e.Timestamp = os.Getpid(), 2
			acked = false
			if err := o.handle(context.Background(), e, func() { acked = true }); !errors.Is(err, frame.ErrRegionNotConfigured) || acked {
				t.Fatalf("first owned event must still announce: err=%v ack=%v", err, acked)
			}
		})
	}
}
