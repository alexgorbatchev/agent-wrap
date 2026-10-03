package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRunValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		opts Options
	}{
		{"empty", nil, Options{}},
		{"unknown", []string{"unknown"}, Options{}},
		{"bad directory", []string{"claude"}, Options{Directory: "/directory/does/not/exist"}},
		{"nonterminal", []string{"claude"}, Options{Directory: t.TempDir(), Input: os.Stdin, Output: os.Stdout}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Run(context.Background(), tc.args, tc.opts); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, []string{"claude"}, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRuntimeIsolation(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, err := runtimeFiles("")
	if err != nil {
		t.Fatal(err)
	}
	b, err := runtimeFiles("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.close(); err != nil {
			t.Error(err)
		}
		if err := b.close(); err != nil {
			t.Error(err)
		}
	})
	if a.cache == b.cache || a.cfg.CursorPath == b.cfg.CursorPath {
		t.Fatal("watcher state is shared")
	}
	if filepath.Dir(a.cfg.CursorPath) != a.cache || a.cfg.OpencodeCheckpointPath != "" || a.cfg.SessionStatePath == "" {
		t.Fatal("incorrect watcher paths")
	}
	a.logger.Info("test_action")
	if _, err := os.Stat(a.logPath); err != nil {
		t.Fatal(err)
	}
}
