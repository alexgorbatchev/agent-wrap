package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	ghostty "go.mitchellh.com/libghostty"
	"golang.org/x/sys/unix"
)

const signalHelperRole = "AGENT_WRAP_SIGNAL_HELPER"

type signalProcesses struct{ Leader, Background int }

// Re-execute the real entry point rather than replacing its signal context.
func TestSignalSubprocessHelper(t *testing.T) {
	switch os.Getenv(signalHelperRole) {
	case "wrapper":
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		os.Args = []string{"agent-wrap", "--harness", "pi", "--", executable, "-test.run=^TestSignalSubprocessHelper$"}
		if err := os.Setenv(signalHelperRole, "leader"); err != nil {
			t.Fatal(err)
		}
		os.Exit(execute())
	case "leader":
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM)
		defer signal.Stop(stop)
		child := exec.Command("sleep", "60")
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		pids := signalProcesses{os.Getpid(), child.Process.Pid}
		data, err := json.Marshal(pids)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("AGENT_WRAP_SIGNAL_PIDS"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		<-stop
		// The frame signals both groups. Reap the background job before the
		// leader exits so a container's PID 1 need not reap an orphan zombie.
		if err := child.Wait(); err == nil {
			t.Fatal("background job exited without a signal")
		} else if exit, ok := errors.AsType[*exec.ExitError](err); !ok || !exit.ProcessState.Sys().(syscall.WaitStatus).Signaled() {
			t.Fatalf("background wait: %v", err)
		}
	}
}

func TestSignalsShutDownOwnedSession(t *testing.T) {
	for _, tc := range []struct {
		name       string
		signal     syscall.Signal
		disconnect bool
	}{
		{"hangup", syscall.SIGHUP, false},
		{"interrupt", syscall.SIGINT, false},
		{"terminate", syscall.SIGTERM, false},
		{"terminal closed", syscall.SIGHUP, true},
	} {
		t.Run(tc.name, func(t *testing.T) { testSignalShutdown(t, tc.signal, tc.disconnect) })
	}
}

func testSignalShutdown(t *testing.T, sig syscall.Signal, disconnect bool) {
	t.Helper()
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	pidPath := filepath.Join(dir, "pids.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestSignalSubprocessHelper$")
	cmd.Env = append(os.Environ(), signalHelperRole+"=wrapper", "AGENT_WRAP_SIGNAL_PIDS="+pidPath,
		"XDG_CACHE_HOME="+cache, "XDG_STATE_HOME="+filepath.Join(dir, "state"), "PI_SESSIONS_DIR="+filepath.Join(dir, "pi"))
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var pids signalProcesses
	t.Cleanup(func() {
		// Let the leader reap its job even when the wrapper failed to clean up.
		for _, pid := range []int{pids.Background, pids.Leader} {
			if pid > 0 {
				if err := unix.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
					t.Error(err)
				}
			}
		}
		deadline := time.Now().Add(time.Second)
		for pids.Leader > 0 && unix.Kill(-pids.Leader, 0) == nil && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		for _, pid := range []int{pids.Background, pids.Leader, cmd.Process.Pid} {
			if pid > 0 {
				if err := unix.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
					t.Error(err)
				}
			}
		}
	})
	closeTerminal := drainSignalTerminal(t, master)
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(pidPath)
		if err == nil && json.Unmarshal(data, &pids) == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned child session did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, pid := range []int{pids.Leader, pids.Background} {
		sid, err := unix.Getsid(pid)
		if err != nil || sid != pids.Leader {
			t.Fatalf("process %d session=%d, want=%d: %v", pid, sid, pids.Leader, err)
		}
		pgid, err := unix.Getpgid(pid)
		if err != nil || pgid != pid {
			t.Fatalf("process %d group=%d: %v", pid, pgid, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(cache, "agent-wrap"))
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("live session cache=%v: %v", entries, err)
	}
	if disconnect {
		// Closing the controlling terminal generates the real kernel SIGHUP
		// and makes terminal restoration fail; cleanup must still finish.
		closeTerminal()
	} else if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		exit, ok := errors.AsType[*exec.ExitError](err)
		if !ok || exit.ExitCode() != 1 {
			t.Errorf("wrapper skipped orderly cancellation: %v; diagnostic=%q", err, diagnostic.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("wrapper did not exit after signal")
	}
	if disconnect && !strings.Contains(diagnostic.String(), "restore termios") {
		t.Errorf("missing terminal restoration error: %q", diagnostic.String())
	}
	entries, err = os.ReadDir(filepath.Join(cache, "agent-wrap"))
	if err != nil || len(entries) != 0 {
		t.Errorf("session cache remains after shutdown: %v: %v", entries, err)
	}
	for _, pid := range []int{pids.Leader, pids.Background} {
		if err := unix.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Errorf("owned process group %d remains after shutdown: %v", pid, err)
		}
	}
}

func drainSignalTerminal(t *testing.T, master *os.File) func() {
	t.Helper()
	if err := unix.SetNonblock(int(master.Fd()), true); err != nil {
		t.Fatal(err)
	}
	em, err := ghostty.NewTerminal(ghostty.WithSize(80, 24), ghostty.WithWritePty(func(_ *ghostty.Terminal, data []byte) {
		if _, err := master.Write(data); err != nil && !errors.Is(err, syscall.EIO) && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	done, stop := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			select {
			case <-stop:
				return
			default:
			}
			fds := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN}}
			if _, err := unix.Poll(fds, 20); err != nil {
				if errors.Is(err, unix.EINTR) {
					continue
				}
				t.Error(err)
				return
			}
			if fds[0].Revents&unix.POLLIN == 0 {
				if fds[0].Revents&unix.POLLHUP != 0 {
					return
				}
				continue
			}
			n, err := unix.Read(int(master.Fd()), buf)
			if err != nil {
				if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
					continue
				}
				if !errors.Is(err, syscall.EIO) && !errors.Is(err, os.ErrClosed) {
					t.Error(err)
				}
				return
			}
			if _, err := em.Write(buf[:n]); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	var once sync.Once
	closeTerminal := func() {
		once.Do(func() {
			close(stop)
			<-done
			if err := master.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Error(err)
			}
			em.Close()
		})
	}
	t.Cleanup(closeTerminal)
	return closeTerminal
}
