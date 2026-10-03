package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRuntimeFilePlacementAndErrors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	f, err := runtimeFiles("")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(f.cache) || filepath.Dir(f.logPath) != filepath.Join(root, ".local", "state", "agent-wrap") {
		t.Fatalf("wrong paths: %+v", f)
	}
	if err := f.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.cache); !os.IsNotExist(err) {
		t.Fatal("cache survives shutdown", err)
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", blocked)
	if _, err := runtimeFiles(""); err == nil {
		t.Fatal("accepted unwritable cache root")
	}
	t.Setenv("XDG_CACHE_HOME", root)
	if _, err := runtimeFiles(filepath.Join(blocked, "session.log")); err == nil {
		t.Fatal("accepted invalid log path")
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := runtimeFiles(""); err == nil {
		t.Fatal("accepted unresolved cache home")
	}
	t.Setenv("XDG_CACHE_HOME", root)
	if _, err := runtimeFiles(""); err == nil {
		t.Fatal("accepted unresolved state home")
	}
}

func TestExitStatus(t *testing.T) {
	if exitCode(nil) != 1 {
		t.Fatal("nil process state accepted")
	}
	for _, tc := range []struct {
		script string
		code   int
	}{{"exit 0", 0}, {"exit 37", 37}, {"kill -TERM $$", 143}} {
		cmd := exec.Command("sh", "-c", tc.script)
		err := cmd.Run()
		if err != nil && cmd.ProcessState == nil {
			t.Fatal(err)
		}
		if code := exitCode(cmd.ProcessState); code != tc.code {
			t.Fatalf("%q code=%d want=%d", tc.script, code, tc.code)
		}
	}
}
