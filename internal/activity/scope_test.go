package activity

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"

	watcher "github.com/alexgorbatchev/agent-watcher"
)

func TestScopedProcessDiscovery(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.Dir = t.TempDir()
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
	p := ScopedProvider(cmd.Process.Pid, "")
	s, err := p.GetSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsProcessAlive(cmd.Process.Pid) || s.IsProcessAlive(os.Getpid()) || !s.VerifyProcessMatch(cmd.Process.Pid, 0) || s.VerifyProcessMatch(os.Getpid(), 0) {
		t.Fatal("scope included outside processes or lost child")
	}
	if _, ok := s.Process(os.Getpid()); ok {
		t.Fatal("outside process metadata leaked")
	}
	if _, ok := s.Process(cmd.Process.Pid); !ok {
		t.Fatal("child process missing")
	}
	if len(s.PiProcesses()) != 0 || len(s.CodexProcesses()) != 0 || len(s.OpencodeProcesses()) != 0 {
		t.Fatal("sleep classified as agent")
	}
	for _, harness := range []watcher.Harness{watcher.HarnessPi, watcher.HarnessCodex, watcher.HarnessOpencode} {
		s, err := ScopedProvider(cmd.Process.Pid, harness).GetSnapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		pid, cwd, started := 0, "", int64(0)
		switch harness {
		case watcher.HarnessPi:
			procs := s.PiProcesses()
			if len(procs) != 1 {
				t.Fatal(procs)
			}
			pid, cwd, started = procs[0].PID, procs[0].CWD, procs[0].CreateTime
		case watcher.HarnessCodex:
			procs := s.CodexProcesses()
			if len(procs) != 1 {
				t.Fatal(procs)
			}
			pid, cwd, started = procs[0].PID, procs[0].CWD, procs[0].CreateTime
		case watcher.HarnessOpencode:
			procs := s.OpencodeProcesses()
			if len(procs) != 1 {
				t.Fatal(procs)
			}
			pid, cwd, started = procs[0].PID, procs[0].CWD, procs[0].CreateTime
		}
		if pid != cmd.Process.Pid || cwd != cmd.Dir || started <= 0 {
			t.Fatalf("declared harness lost native launch metadata: %d %q %d", pid, cwd, started)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.GetSnapshot(ctx); err == nil {
		t.Fatal("ignored cancelled discovery")
	}
}
