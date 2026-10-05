package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestModes(t *testing.T) {
	for _, tc := range []struct {
		value string
		agent bool
	}{{"", false}, {"0", false}, {"false", false}, {"1", true}, {"true", true}, {" YES ", true}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("AGENT", tc.value)
			var buf bytes.Buffer
			logger := New(&buf, "agent-wrap")
			logger.Debug("hidden")
			logger.Info("session_attached", "cwd", "/project with spaces")
			out := buf.String()
			if IsAgentMode() != tc.agent || strings.Contains(out, "hidden") || !strings.Contains(out, `cwd="/project with spaces"`) {
				t.Fatalf("mode=%v output=%q", IsAgentMode(), out)
			}
			if tc.agent {
				if !strings.Contains(out, "action=session_attached") || !strings.Contains(out, "service=agent-wrap") {
					t.Fatal(out)
				}
			} else if !strings.Contains(out, "[INFO] session_attached") || strings.Contains(out, "service=") {
				t.Fatal(out)
			}
		})
	}
}

func TestHumanAttributes(t *testing.T) {
	var buf bytes.Buffer
	h := newHumanHandler(&buf, slog.LevelDebug)
	logger := slog.New(h)
	logger.Debug("details", "text", "quote \" newline\n", "n", int64(-2), "u", uint64(3), "f", 1.5,
		"bool", true, "duration", time.Second, "err", errors.New("with spaces"), "empty", "")
	logger.Warn("warning")
	logger.Error("failure")
	logger.WithGroup("meta").With("region", "local").Info("grouped", "pid", 42)
	if h.WithGroup("") != h {
		t.Fatal("empty group changed the handler")
	}
	r := slog.NewRecord(time.Time{}, slog.Level(12), "custom", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[DEBUG] details", "[WARN] warning", "[ERROR] failure", `text="quote \" newline\n"`,
		"n=-2", "u=3", "f=1.5", "bool=true", "duration=1s", `err="with spaces"`, `empty=""`,
		"meta.region=local", "meta.pid=42", "[ERROR+4] custom"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in %q", want, buf.String())
		}
	}
}

func TestHumanConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newHumanHandler(&buf, slog.LevelInfo))
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() { logger.With("worker", i).Info("event") })
	}
	wg.Wait()
	if strings.Count(buf.String(), "[INFO] event worker=") != 20 {
		t.Fatal(buf.String())
	}
}

func TestFileLog(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "session.log")
	writer, err := NewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT", "1")
	New(writer, "agent-wrap").Warn("context_resolution_failed", "err", errors.New("missing directory"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("action=context_resolution_failed")) {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if _, err := NewFile(filepath.Join(path, "blocked.log")); err == nil {
		t.Fatal("accepted file as parent directory")
	}
	closed, err := os.Create(filepath.Join(root, "closed.log"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := newHumanHandler(closed, slog.LevelInfo).Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "failure", 0)); err == nil {
		t.Fatal("lost writer failure")
	}
}
