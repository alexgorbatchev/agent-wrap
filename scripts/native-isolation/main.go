package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const shutdownGrace = 5 * time.Second

func main() { os.Exit(mainExit(os.Args[1:])) }

func mainExit(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: native-isolation PROJECT")
		return 1
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	defer signal.Stop(signals)
	status, err := run(args[0], signals, shutdownGrace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return status
}

func run(project string, signals <-chan os.Signal, grace time.Duration) (int, error) {
	root := filepath.Join(project, ".tmp")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return 1, err
	}
	scratch, err := os.MkdirTemp(root, "native-isolation.")
	if err != nil {
		return 1, err
	}
	cmd := exec.Command("bash", filepath.Join(project, "scripts/native-isolation/run.sh"), project, scratch)
	cmd.Env = append(os.Environ(), "TMPDIR="+scratch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 1, errors.Join(err, os.RemoveAll(scratch))
	}
	status, stopped, err := joinGroup(cmd, signals, grace)
	if !stopped {
		return 1, fmt.Errorf("native consumers not confirmed stopped; retained %s: %w", scratch, err)
	}
	return status, errors.Join(err, os.RemoveAll(scratch))
}

func sendGroup(pid int, sig syscall.Signal) error {
	err := syscall.Kill(-pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// Wait for both the direct child and the entire process group. A dead shell
// alone says nothing about native compilers that outlive it during shutdown.
func joinGroup(cmd *exec.Cmd, signals <-chan os.Signal, grace time.Duration) (int, bool, error) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	var deadline <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	status, joined, cancelling, killed := 0, false, false, false
	for {
		if joined {
			err := syscall.Kill(-cmd.Process.Pid, 0)
			if errors.Is(err, syscall.ESRCH) {
				return status, true, nil
			}
			if err != nil {
				return 1, false, err
			}
		}
		select {
		case err := <-done:
			joined = true
			done = nil
			if !cancelling && err != nil {
				status = 1
				var exit *exec.ExitError
				if errors.As(err, &exit) && exit.ExitCode() > 0 {
					status = exit.ExitCode()
				}
			}
			if timer == nil {
				timer = time.NewTimer(grace)
				deadline = timer.C
			}
		case sig := <-signals:
			if !cancelling {
				cancelling = true
				status = 128 + int(sig.(syscall.Signal))
				if err := sendGroup(cmd.Process.Pid, syscall.SIGTERM); err != nil {
					return 1, false, err
				}
				if timer == nil {
					timer = time.NewTimer(grace)
				} else {
					timer.Reset(grace)
				}
				deadline = timer.C
			}
		case <-deadline:
			if killed {
				return 1, false, errors.New("process group survived forced shutdown")
			}
			sig := syscall.SIGTERM
			if cancelling {
				sig, killed = syscall.SIGKILL, true
			}
			cancelling = true
			if status == 0 {
				status = 1
			}
			if err := sendGroup(cmd.Process.Pid, sig); err != nil {
				return 1, false, err
			}
			timer.Reset(grace)
		case <-poll.C:
		}
	}
}
