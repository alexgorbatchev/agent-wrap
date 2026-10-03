package session

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	watcher "github.com/alexgorbatchev/agent-watcher"
	"github.com/alexgorbatchev/agent-wrap/internal/logging"
)

type files struct {
	cache, logPath string
	log            io.Closer
	logger         *slog.Logger
	cfg            watcher.Config
}

func runtimeFiles(logPath string) (*files, error) {
	root, err := cacheRoot()
	if err != nil {
		return nil, fmt.Errorf("resolve watcher cache: %w", err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create watcher cache: %w", err)
	}
	cache, err := os.MkdirTemp(root, "session-")
	if err != nil {
		return nil, fmt.Errorf("create session cache: %w", err)
	}
	f := &files{cache: cache}
	if logPath == "" {
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return nil, errors.Join(homeErr, f.close())
			}
			base = filepath.Join(home, ".local", "state")
		}
		logPath = filepath.Join(base, "agent-wrap", filepath.Base(cache)+".log")
	}
	f.logPath = logPath
	log, err := logging.NewFile(logPath)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open wrapper log: %w", err), f.close())
	}
	f.log = log
	f.logger = logging.New(log, "agent-wrap")
	f.cfg = watcher.Config{CursorPath: filepath.Join(cache, "cursors.json"), SessionStatePath: filepath.Join(cache, "sessions.json"), Logger: f.logger}
	return f, nil
}

func (f *files) close() error {
	var err error
	if f.log != nil {
		err = f.log.Close()
	}
	return errors.Join(err, os.RemoveAll(f.cache))
}
