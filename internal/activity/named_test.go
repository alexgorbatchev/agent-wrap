package activity

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	watcher "github.com/alexgorbatchev/agent-watcher"
)

func TestDeclaredHarnessPreservesNativeDiscovery(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range []watcher.Harness{watcher.HarnessPi, watcher.HarnessCodex, watcher.HarnessOpencode} {
		t.Run(string(harness), func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, string(harness))
			if err := os.Symlink(sleep, executable); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "30")
			cmd.Dir = dir
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := cmd.Process.Kill(); err != nil {
					t.Error(err)
				}
				_ = cmd.Wait() /* killed child has a nonzero exit */
			})
			s, err := ScopedProvider(cmd.Process.Pid, harness).GetSnapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			switch harness {
			case watcher.HarnessPi:
				procs := s.PiProcesses()
				if len(procs) != 1 || procs[0].PID != cmd.Process.Pid {
					t.Fatalf("duplicate or wrong native Pi process: %+v", procs)
				}
			case watcher.HarnessCodex:
				procs := s.CodexProcesses()
				if len(procs) != 1 || procs[0].PID != cmd.Process.Pid {
					t.Fatalf("duplicate or wrong native Codex process: %+v", procs)
				}
			case watcher.HarnessOpencode:
				procs := s.OpencodeProcesses()
				if len(procs) != 1 || procs[0].PID != cmd.Process.Pid {
					t.Fatalf("duplicate or wrong native OpenCode process: %+v", procs)
				}
			}
		})
	}
}
