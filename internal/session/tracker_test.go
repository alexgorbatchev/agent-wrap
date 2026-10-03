package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	parser "github.com/alexgorbatchev/agent-parser"
	watcher "github.com/alexgorbatchev/agent-watcher"
	"github.com/alexgorbatchev/agent-wrap/internal/header"
	"github.com/alexgorbatchev/agent-wrap/internal/project"
)

func TestTrackerUsesReportedBaseAndKeepsLastTarget(t *testing.T) {
	base := t.TempDir()
	for _, dir := range []string{"src", "docs"} {
		if err := os.Mkdir(filepath.Join(base, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.Resolve(context.Background(), base, "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	tr := newTracker(header.New(p), "", now)
	e := watcher.Event{Harness: watcher.HarnessCodex, SessionID: "one", ParsedEvent: parser.ParsedEvent{EventType: "tool_call", Timestamp: now, CWD: base, Data: parser.EventData{ToolInput: map[string]interface{}{"path": "src/new.go"}}}}
	s, changed, err := tr.apply(context.Background(), e)
	if err != nil || !changed || s.Current.Dir != filepath.Join(base, "src") {
		t.Fatalf("first target = %+v, %v", s, err)
	}
	e.EventType = "thinking"
	e.Timestamp++
	s, changed, err = tr.apply(context.Background(), e)
	if err != nil || changed || s.Current.Dir != filepath.Join(base, "src") {
		t.Fatal("thinking reset last tool target")
	}
	e.EventType = "tool_call"
	e.Timestamp++
	e.Data.ToolInput = map[string]interface{}{"path": "docs/new.go"}
	s, _, err = tr.apply(context.Background(), e)
	if err != nil || s.Current.Dir != filepath.Join(base, "docs") {
		t.Fatal("relative path used previous tool directory instead of reported base")
	}
	e.Timestamp = now - 1
	e.Data.ToolInput = map[string]interface{}{"workdir": base}
	s, changed, err = tr.apply(context.Background(), e)
	if err != nil || changed || s.Current.Dir != filepath.Join(base, "docs") {
		t.Fatal("stale replay changed context")
	}
	e.Timestamp = now + 10
	e.EventType = "model_change"
	e.CWD = filepath.Join(base, "src")
	s, changed, err = tr.apply(context.Background(), e)
	if err != nil || !changed || s.Current.Dir != e.CWD {
		t.Fatal("reported task directory change ignored")
	}
}
