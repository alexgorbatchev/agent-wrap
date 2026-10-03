package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// IsAgentMode accepts the CLI's supported truthy values for AGENT.
func IsAgentMode() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("AGENT")))
	return v == "1" || v == "true" || v == "yes"
}

// New preserves the shared logger's human and agent formats at info level.
func New(w io.Writer, service string) *slog.Logger {
	var handler slog.Handler
	if IsAgentMode() {
		handler = slog.NewTextHandler(w, &slog.HandlerOptions{
			Level: slog.LevelInfo,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if len(groups) == 0 && a.Key == slog.MessageKey {
					a.Key = "action"
				}
				return a
			},
		})
	} else {
		handler = newHumanHandler(w, slog.LevelInfo)
	}
	return slog.New(handler).With("service", service)
}
