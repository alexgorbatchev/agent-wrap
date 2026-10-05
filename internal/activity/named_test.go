package activity

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	watcher "github.com/alexgorbatchev/agent-watcher"
	"github.com/alexgorbatchev/agent-watcher/scanner"
)

// agentExecutable copies the test binary to a file named after the harness.
// Native discovery classifies processes by their kernel command name, which
// Darwin takes from the executed file's own name (a symlink reports its
// target), and Darwin kills copies of its signed system binaries.
func agentExecutable(t *testing.T, harness watcher.Harness) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := src.Close(); err != nil {
			t.Error(err)
		}
	}()
	path := filepath.Join(t.TempDir(), string(harness))
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(errors.Join(err, dst.Close()))
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// startAgent runs executable as the leader of a new session and returns its
// PID once the helper reports that it runs under its new name.
func startAgent(t *testing.T, executable string) int {
	t.Helper()
	cmd := exec.Command(executable)
	cmd.Dir = filepath.Dir(executable)
	cmd.Env = append(os.Environ(), agentHelperEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The open pipe keeps the helper blocked; Wait closes it after the kill.
	if _, err := cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Error(err)
		}
		_ = cmd.Wait() /* killed child has a nonzero exit */
	})
	// Start returns when exec closes the child's close-on-exec descriptors,
	// which both Linux and Darwin do before renaming the process. The helper
	// writes its first byte only after exec has finished.
	ready := make([]byte, 1)
	if _, err := io.ReadFull(stdout, ready); err != nil {
		t.Fatalf("agent helper did not report ready: %v", err)
	}
	if ready[0] != agentReady {
		t.Fatalf("agent helper wrote %q, want %q", ready[0], agentReady)
	}
	return cmd.Process.Pid
}

func agentPIDs(s scanner.ProcessSnapshot, harness watcher.Harness) []int {
	var pids []int
	switch harness {
	case watcher.HarnessPi:
		for _, p := range s.PiProcesses() {
			pids = append(pids, p.PID)
		}
	case watcher.HarnessCodex:
		for _, p := range s.CodexProcesses() {
			pids = append(pids, p.PID)
		}
	case watcher.HarnessOpencode:
		for _, p := range s.OpencodeProcesses() {
			pids = append(pids, p.PID)
		}
	}
	return pids
}

func TestDeclaredHarnessPreservesNativeDiscovery(t *testing.T) {
	for _, harness := range []watcher.Harness{watcher.HarnessPi, watcher.HarnessCodex, watcher.HarnessOpencode} {
		t.Run(string(harness), func(t *testing.T) {
			executable := agentExecutable(t, harness)
			leader, outsider := startAgent(t, executable), startAgent(t, executable)
			native, err := scanner.CaptureProcessSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if pids := agentPIDs(native, harness); !slices.Contains(pids, leader) || !slices.Contains(pids, outsider) {
				t.Fatalf("native discovery missed %s processes %d and %d: %v", harness, leader, outsider, pids)
			}
			s, err := ScopedProvider(leader, harness).GetSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if pids := agentPIDs(s, harness); !slices.Equal(pids, []int{leader}) {
				t.Fatalf("scoped %s processes = %v, want only session leader %d once (outside session %d)", harness, pids, leader, outsider)
			}
		})
	}
}
