package session

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	watcher "github.com/alexgorbatchev/agent-watcher"
	"github.com/alexgorbatchev/agent-wrap/internal/logging"
	"golang.org/x/sys/unix"
)

type files struct {
	cache, logPath string
	log            io.Closer
	owner          *os.File
	logOwner       *os.File
	logger         *slog.Logger
	cfg            watcher.Config
}

func runtimeFiles(logPath string) (result *files, err error) {
	root, err := cacheRoot()
	if err != nil {
		return nil, fmt.Errorf("resolve watcher cache: %w", err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create watcher cache: %w", err)
	}
	// Serialize creation and ownership registration with other startup sweeps.
	guard, err := lockFile(root+".lock", unix.LOCK_EX)
	if err != nil {
		return nil, fmt.Errorf("lock watcher cache root: %w", err)
	}
	defer func() { result, err = releaseStartupLock(guard, result, err) }()
	cache, err := os.MkdirTemp(root, "session-")
	if err != nil {
		return nil, fmt.Errorf("create session cache: %w", err)
	}
	f := &files{cache: cache}
	f.owner, err = lockFile(filepath.Join(cache, "owner.lock"), unix.LOCK_EX)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("lock session cache: %w", err), f.close())
	}
	defaultLog := logPath == ""
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
	// Any session-shaped destination can share a default state directory with
	// another wrapper, even when selected explicitly and using another cache root.
	if match := sessionLog.FindStringSubmatch(filepath.Base(logPath)); match != nil {
		logGuard, lockErr := lockFile(filepath.Join(filepath.Dir(logPath), ".cleanup.lock"), unix.LOCK_EX)
		if lockErr != nil {
			return nil, errors.Join(lockErr, f.close())
		}
		defer func() { result, err = releaseStartupLock(logGuard, result, err) }()
		f.logOwner, err = openLockedFile(filepath.Join(filepath.Dir(logPath), match[1]+".log.lock"), unix.O_CREAT, unix.LOCK_EX|unix.LOCK_NB)
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errors.Join(fmt.Errorf("log destination %q is already in use: %w", logPath, err), f.close())
		}
		if err != nil {
			return nil, errors.Join(err, f.close())
		}
	}
	f.logger = logging.New(log, "agent-wrap")
	f.cfg = watcher.Config{CursorPath: filepath.Join(cache, "cursors.json"), SessionStatePath: filepath.Join(cache, "sessions.json"), Logger: f.logger}
	if err := pruneRuntimeFiles(root, filepath.Dir(logPath), defaultLog, time.Now()); err != nil {
		return nil, errors.Join(fmt.Errorf("prune runtime files: %w", err), f.close())
	}
	return f, nil
}

func releaseStartupLock(guard *os.File, result *files, err error) (*files, error) {
	if closeErr := guard.Close(); closeErr != nil {
		if result != nil {
			closeErr = errors.Join(closeErr, result.close())
			result = nil
		}
		err = errors.Join(err, closeErr)
	}
	return result, err
}

func (f *files) close() error {
	var err error
	if f.log != nil {
		err = f.log.Close()
	}
	err = errors.Join(err, os.RemoveAll(f.cache))
	if f.owner != nil {
		err = errors.Join(err, f.owner.Close())
	}
	if f.logOwner != nil {
		err = errors.Join(err, os.Remove(f.logOwner.Name()), f.logOwner.Close())
	}
	return err
}
