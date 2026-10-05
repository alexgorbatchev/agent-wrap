package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRuntimeFilesRetention(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
			cache := filepath.Join(root, "cache", "agent-wrap")
			logs := filepath.Join(root, "state", "agent-wrap")
			old := time.Now().Add(-15 * 24 * time.Hour)
			for _, dir := range []string{cache, logs} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"session-dead", "session-live", "session-fresh"} {
				path := filepath.Join(cache, name)
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if name != "session-fresh" {
					if err := os.Chtimes(path, old, old); err != nil {
						t.Fatal(err)
					}
				}
			}
			// A native advisory lock, rather than age or PID alone, proves ownership.
			owner, err := os.OpenFile(filepath.Join(cache, "session-live", "owner.lock"), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := owner.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := unix.Flock(int(owner.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(filepath.Join(cache, "session-live"), old, old); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"session-dead.log", "session-dead-2026-01-01T00-00-00.000.log.gz", "session-live.log", "session-fresh.log", "other.log"} {
				path := filepath.Join(logs, name)
				if err := os.WriteFile(path, []byte("record"), 0600); err != nil {
					t.Fatal(err)
				}
				if name != "session-fresh.log" {
					if err := os.Chtimes(path, old, old); err != nil {
						t.Fatal(err)
					}
				}
			}
			logPath := ""
			if explicit {
				logPath = filepath.Join(root, "custom.log")
			}
			f, err := runtimeFiles(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.close(); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"session-dead", "session-live", "session-fresh"} {
				_, err := os.Stat(filepath.Join(cache, name))
				if exists := err == nil; exists != (name != "session-dead") {
					t.Errorf("cache %s exists=%v", name, exists)
				}
			}
			for _, name := range []string{"session-dead.log", "session-dead-2026-01-01T00-00-00.000.log.gz", "session-live.log", "session-fresh.log", "other.log"} {
				_, err := os.Stat(filepath.Join(logs, name))
				want := explicit || !strings.HasPrefix(name, "session-dead")
				if exists := err == nil; exists != want {
					t.Errorf("log %s exists=%v want=%v", name, exists, want)
				}
			}
		})
	}
}

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

func TestRuntimeFilesConcurrentOwnership(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	f, err := runtimeFiles("")
	if err != nil {
		t.Fatal(err)
	}
	f.logger.Info("idle owner")
	old := time.Now().Add(-15 * 24 * time.Hour)
	for _, path := range []string{f.cache, f.logPath} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			other, err := runtimeFiles("")
			if err == nil {
				err = other.close()
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(f.logPath); err != nil {
		t.Fatal("live idle log deleted", err)
	}
	// State and cache locations are independent; shared logs remain protected.
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "second-cache"))
	second, err := runtimeFiles("")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.logPath); err != nil {
		t.Fatal("live log with different cache root deleted", err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	// Closing the descriptor models the kernel releasing a crashed owner's lock.
	if err := f.log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.logOwner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(f.logOwner.Name(), old, old); err != nil {
		t.Fatal(err)
	}
	other, err := runtimeFiles("")
	if err != nil {
		t.Fatal(err)
	}
	if err := other.close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{f.cache, f.logPath, f.logOwner.Name()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("orphan survives: %s: %v", path, err)
		}
	}
}

func TestRuntimeFilesRetentionFailures(t *testing.T) {
	for _, location := range []string{"cache-guard", "log-guard", "cache-owner"} {
		t.Run(location, func(t *testing.T) {
			root := t.TempDir()
			cache := filepath.Join(root, "cache", "agent-wrap")
			logs := filepath.Join(root, "state", "agent-wrap")
			t.Setenv("XDG_CACHE_HOME", filepath.Dir(cache))
			t.Setenv("XDG_STATE_HOME", filepath.Dir(logs))
			for _, dir := range []string{cache, logs} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			blocked := cache + ".lock"
			if location == "log-guard" {
				blocked = filepath.Join(logs, ".cleanup.lock")
			}
			if location == "cache-owner" {
				dir := filepath.Join(cache, "session-blocked")
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				blocked = filepath.Join(dir, "owner.lock")
			}
			if err := os.Symlink(filepath.Join(root, "missing"), blocked); err != nil {
				t.Fatal(err)
			}
			if _, err := runtimeFiles(""); err == nil {
				t.Fatal("followed a symlink ownership lock")
			}
			entries, err := os.ReadDir(cache)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != "session-blocked" {
					t.Errorf("failed startup leaked cache %s", entry.Name())
				}
			}
		})
	}
	root := t.TempDir()
	if err := pruneRuntimeFiles(filepath.Join(root, "missing"), root, true, time.Now()); err == nil {
		t.Fatal("accepted missing cache")
	}
	if err := pruneRuntimeFiles(root, filepath.Join(root, "missing"), true, time.Now()); err == nil {
		t.Fatal("accepted missing logs")
	}
}

func TestStartupLockReleaseFailureClosesFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("XDG_STATE_HOME", root)
	f, err := runtimeFiles("")
	if err != nil {
		t.Fatal(err)
	}
	guard, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := releaseStartupLock(guard, f, nil)
	if result != nil || err == nil {
		t.Fatalf("result=%v err=%v", result, err)
	}
	for _, path := range []string{f.cache, f.logOwner.Name()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("failed release leaked %s: %v", path, err)
		}
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
