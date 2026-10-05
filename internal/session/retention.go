package session

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alexgorbatchev/agent-wrap/internal/logging"
	"golang.org/x/sys/unix"
)

var sessionLog = regexp.MustCompile(`^(session-[A-Za-z0-9]+)(-[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}\.[0-9]{3})?\.log(\.gz)?$`)

func lockFile(path string, mode int) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if mode&unix.LOCK_NB == 0 {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	for {
		err = unix.Flock(fd, mode)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

// The caller holds the root lock, so a new session cannot appear unowned
// between MkdirTemp and taking its lifetime lock. Closing sessions remove their
// directories before releasing ownership, and never write logs afterwards.
func pruneRuntimeFiles(cache, logs string, defaultLog bool, now time.Time) error {
	live, err := pruneCaches(cache, now)
	if err != nil || !defaultLog {
		return err
	}
	return pruneLogs(logs, live, now)
}

func pruneCaches(cache string, now time.Time) (map[string]bool, error) {
	entries, err := os.ReadDir(cache)
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "session-") {
			continue
		}
		path := filepath.Join(cache, entry.Name())
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		owner, err := lockFile(filepath.Join(path, "owner.lock"), unix.LOCK_EX|unix.LOCK_NB)
		if errors.Is(err, unix.EWOULDBLOCK) {
			live[entry.Name()] = true
			continue
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		err = nil
		if now.Sub(info.ModTime()) > logging.Retention {
			err = os.RemoveAll(path)
		}
		if owner != nil {
			err = errors.Join(err, owner.Close())
		}
		if err != nil {
			return nil, err
		}
	}
	return live, nil
}

func pruneLogs(logs string, live map[string]bool, now time.Time) error {
	entries, err := os.ReadDir(logs)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		match := sessionLog.FindStringSubmatch(entry.Name())
		if match == nil || live[match[1]] || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) <= logging.Retention {
			continue
		}
		owner, err := lockFile(filepath.Join(logs, match[1]+".log.lock"), unix.LOCK_EX|unix.LOCK_NB)
		if errors.Is(err, unix.EWOULDBLOCK) {
			continue
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		err = os.Remove(filepath.Join(logs, entry.Name()))
		if owner != nil {
			err = errors.Join(err, owner.Close())
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return pruneLogLocks(logs, entries, now)
}

func pruneLogLocks(logs string, entries []os.DirEntry, now time.Time) error {
	// Crash leftovers have no live owner; fresh lock files receive the same grace.
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".log.lock") || sessionLog.FindStringSubmatch(strings.TrimSuffix(entry.Name(), ".lock")) == nil {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) <= logging.Retention {
			continue
		}
		owner, err := lockFile(filepath.Join(logs, entry.Name()), unix.LOCK_EX|unix.LOCK_NB)
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		err = errors.Join(os.Remove(owner.Name()), owner.Close())
		if err != nil {
			return err
		}
	}
	return nil
}
