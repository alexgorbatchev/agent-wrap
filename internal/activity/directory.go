// Package activity selects the directory explicitly reported by agent activity.
package activity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	watcher "github.com/alexgorbatchev/agent-watcher"
)

// Harness selects the parser family for a command or explicit override.
func Harness(executable, override string) (watcher.Harness, error) {
	name := override
	if name == "" {
		name = filepath.Base(executable)
	}
	switch name {
	case "claude", "claude-code":
		return watcher.HarnessClaudeCode, nil
	case "pi":
		return watcher.HarnessPi, nil
	case "codex":
		return watcher.HarnessCodex, nil
	case "opencode":
		return watcher.HarnessOpencode, nil
	default:
		return "", fmt.Errorf("unknown agent %q; select --harness claude-code, pi, codex, or opencode", name)
	}
}

// Directory uses reported tool targets; non-tool events preserve the last target.
func IsTool(e watcher.Event) bool {
	if e.Harness == watcher.HarnessOpencode {
		return e.EventType == "tool_use"
	}
	return e.EventType == "tool_call"
}

func Directory(e watcher.Event, previous string) string {
	if !IsTool(e) {
		return previous
	}
	base := e.CWD
	if base == "" {
		base = previous
	}
	for _, key := range []string{"workdir", "cwd"} {
		if path, ok := e.Data.ToolInput[key].(string); ok {
			if dir := existingDirectory(path, base, false); dir != "" {
				return dir
			}
		}
	}
	for _, key := range []string{"file_path", "filePath", "notebook_path", "path"} {
		if path, ok := e.Data.ToolInput[key].(string); ok {
			if dir := existingDirectory(path, base, true); dir != "" {
				return dir
			}
		}
	}
	if e.Data.FileModification != nil {
		if dir := existingDirectory(e.Data.FileModification.FilePath, base, true); dir != "" {
			return dir
		}
	}
	return base
}

func existingDirectory(path, base string, file bool) string {
	if path == "" || strings.ContainsAny(path, "\x00\r\n") || strings.Contains(path, "://") {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return filepath.Clean(path)
	}
	if !file {
		return ""
	}
	parent := filepath.Dir(path)
	info, err = os.Stat(parent)
	if err == nil && info.IsDir() {
		return parent
	}
	return ""
}
