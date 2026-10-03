// Package session owns one terminal frame and its independent watcher lifecycle.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	watcher "github.com/alexgorbatchev/agent-watcher"
	"github.com/alexgorbatchev/agent-wrap/internal/activity"
	"github.com/alexgorbatchev/agent-wrap/internal/header"
	"github.com/alexgorbatchev/agent-wrap/internal/project"
	frame "github.com/alexgorbatchev/go-tui-frame"
	"golang.org/x/term"
)

// Options configures launch context and local observation, without a daemon.
type Options struct {
	Harness, Directory, DefaultBranch, Name, LogFile string
	ScanInterval                                     time.Duration
	Input, Output                                    *os.File
}

type payload struct {
	State header.State
	Name  string
}

// Run preserves child arguments and exit status and joins all owned workers.
func Run(ctx context.Context, args []string, opts Options) (code int, err error) {
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	if len(args) == 0 {
		return 1, fmt.Errorf("an agent command is required")
	}
	harness, err := activity.Harness(args[0], opts.Harness)
	if err != nil {
		return 1, err
	}
	if opts.Directory == "" {
		opts.Directory, err = os.Getwd()
		if err != nil {
			return 1, err
		}
	}
	initial, err := project.Resolve(ctx, opts.Directory, opts.DefaultBranch)
	if err != nil {
		return 1, err
	}
	if opts.Input == nil {
		opts.Input = os.Stdin
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if !term.IsTerminal(int(opts.Input.Fd())) || !term.IsTerminal(int(opts.Output.Fd())) {
		return 1, fmt.Errorf("agent-wrap requires an interactive terminal")
	}
	files, err := runtimeFiles(opts.LogFile)
	if err != nil {
		return 1, err
	}
	defer func() { err = errors.Join(err, files.close()) }()
	name := opts.Name
	if name == "" {
		name = displayName(harness)
	}
	child := exec.Command(args[0], args[1:]...)
	child.Dir = initial.Dir
	state := header.New(initial)
	since := time.Now().UnixMilli()
	tr := newTracker(state, opts.DefaultBranch, since)
	app := frame.New(child, payload{State: state, Name: name + " · waiting for agent activity"}).
		Header(header.Rows, func(draw frame.DrawContext[payload]) { header.Draw(draw.View, draw.Data.State, draw.Data.Name) }).
		Terminal(opts.Input, opts.Output)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	started := make(chan int, 1)
	app.ObserveEvents([]frame.EventKind{frame.Started}, func(e frame.Event) {
		if e.Snapshot != nil {
			started <- e.Snapshot.Child.PID
		}
	})
	watchDone := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			watchDone <- nil
		case leader := <-started:
			cfg := files.cfg
			cfg.Harnesses = []watcher.Harness{harness}
			cfg.ScanInterval = opts.ScanInterval
			cfg.ProcessSnapshotProvider = activity.ScopedProvider(leader, harness)
			o := observer{leader: leader, since: since, tracker: tr, app: app, name: name, logger: files.logger}
			watchErr := watcher.Run(ctx, cfg, o.handle)
			watchDone <- watchErr
			if watchErr != nil {
				cancel()
			}
		}
	}()
	result, frameErr := app.Run(ctx)
	cancel()
	watchErr := <-watchDone
	if errors.Is(watchErr, context.Canceled) {
		watchErr = nil
	}
	err = errors.Join(frameErr, watchErr)
	if err == nil {
		err = errors.Join(result.DrainError, result.CleanupError)
	}
	if err != nil {
		return 1, err
	}
	return exitCode(result.ProcessState), nil
}

func displayName(h watcher.Harness) string {
	switch h {
	case watcher.HarnessClaudeCode:
		return "Claude Code"
	case watcher.HarnessPi:
		return "Pi"
	case watcher.HarnessCodex:
		return "Codex"
	default:
		return "OpenCode"
	}
}

func exitCode(state *os.ProcessState) int {
	if state == nil {
		return 1
	}
	if state.ExitCode() >= 0 {
		return state.ExitCode()
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return 1
}

// cacheRoot follows XDG cache placement on both supported operating systems.
func cacheRoot() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "agent-wrap"), nil
}
