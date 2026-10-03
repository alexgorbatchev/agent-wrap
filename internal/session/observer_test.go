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
	frame "github.com/alexgorbatchev/go-tui-frame"
	"golang.org/x/sys/unix"
)

func TestObserverAcknowledgesOnlyAcceptedEvents(t *testing.T) {
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
	var log bytes.Buffer
	app := frame.New(exec.Command("true"), payload{State: state})
	o := observer{leader: leader, since: 1, tracker: newTracker(state, "", 1), app: app, logger: logging.New(&log, "agent-wrap")}
	e := watcher.Event{PID: os.Getpid(), ParsedEvent: parser.ParsedEvent{Timestamp: 2, EventType: "tool_call", CWD: initial.Dir}}
	acked := false
	err = o.handle(ctx, e, func() { acked = true })
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
