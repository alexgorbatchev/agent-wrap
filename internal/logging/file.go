package logging

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	maxSizeMB  = 10
	maxBackups = 5
	maxAgeDays = 14
)

// NewFile creates a rotating writer with the wrapper's existing retention.
func NewFile(path string) (*lumberjack.Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	return &lumberjack.Logger{
		Filename: path, MaxSize: maxSizeMB, MaxBackups: maxBackups,
		MaxAge: maxAgeDays, Compress: true,
	}, nil
}
