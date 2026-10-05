package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Retention is the age limit for inactive default logs and watcher caches.
const Retention = maxAgeDays * 24 * time.Hour

const (
	maxSizeMB  = 10
	maxBackups = 5
	maxAgeDays = 14
)

// NewFile rotates one log, retaining up to five compressed backups for 14 days.
func NewFile(path string) (*lumberjack.Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	return &lumberjack.Logger{
		Filename: path, MaxSize: maxSizeMB, MaxBackups: maxBackups,
		MaxAge: maxAgeDays, Compress: true,
	}, nil
}
