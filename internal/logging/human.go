package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

// Human formatting is copied from agent-status's MIT-licensed logging package.
type humanHandler struct {
	w      io.Writer
	level  slog.Level
	attrs  []slog.Attr
	groups []string
	mu     *sync.Mutex
}

func newHumanHandler(w io.Writer, level slog.Level) *humanHandler {
	return &humanHandler{
		w:     w,
		level: level,
		mu:    &sync.Mutex{},
	}
}

func (h *humanHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *humanHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs), len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	newAttrs = append(newAttrs, attrs...)
	return &humanHandler{
		w:      h.w,
		level:  h.level,
		attrs:  newAttrs,
		groups: h.groups,
		mu:     h.mu,
	}
}

func (h *humanHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	newGroups := make([]string, len(h.groups), len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups = append(newGroups, name)
	return &humanHandler{
		w:      h.w,
		level:  h.level,
		attrs:  h.attrs,
		groups: newGroups,
		mu:     h.mu,
	}
}

func (h *humanHandler) Handle(_ context.Context, r slog.Record) error {
	allAttrs := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())

	addAttr := func(a slog.Attr) {
		a.Value = a.Value.Resolve()
		if len(h.groups) > 0 {
			a.Key = strings.Join(h.groups, ".") + "." + a.Key
		}
		// In human mode, hide redundant service=... attribute since dev-stack / runner provides service prefix
		if a.Key == "service" {
			return
		}
		allAttrs = append(allAttrs, a)
	}

	for _, a := range h.attrs {
		addAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		addAttr(a)
		return true
	})

	hasEnv := false
	hasProject := false
	for _, a := range allAttrs {
		if a.Key == "env" && a.Value.String() != "" {
			hasEnv = true
		}
		if a.Key == "project" && a.Value.String() != "" {
			hasProject = true
		}
	}

	var buf bytes.Buffer
	t := r.Time
	if t.IsZero() {
		buf.WriteString("00:00:00")
	} else {
		buf.WriteString(t.Format("15:04:05"))
	}

	buf.WriteString(" ")
	switch r.Level {
	case slog.LevelDebug:
		buf.WriteString("[DEBUG]")
	case slog.LevelInfo:
		buf.WriteString("[INFO]")
	case slog.LevelWarn:
		buf.WriteString("[WARN]")
	case slog.LevelError:
		buf.WriteString("[ERROR]")
	default:
		_, _ = fmt.Fprintf(&buf, "[%s]", r.Level.String())
	}

	if r.Message != "" {
		buf.WriteString(" ")
		buf.WriteString(r.Message)
	}

	for _, a := range allAttrs {
		if hasEnv && a.Key == "envId" {
			continue
		}
		if hasProject && a.Key == "projectId" {
			continue
		}

		buf.WriteString(" ")
		buf.WriteString(a.Key)
		buf.WriteString("=")
		buf.WriteString(formatAttrValue(a.Value))
	}
	buf.WriteString("\n")

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf.Bytes())
	return err
}

func formatAttrValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if strings.ContainsAny(s, " \t\n\"\\=") || s == "" {
			return strconv.Quote(s)
		}
		return s
	case slog.KindInt64:
		return strconv.FormatInt(v.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(v.Float64(), 'f', -1, 64)
	case slog.KindBool:
		return strconv.FormatBool(v.Bool())
	case slog.KindDuration:
		return v.Duration().String()
	default:
		s := fmt.Sprint(v.Any())
		if strings.ContainsAny(s, " \t\n\"\\=") || s == "" {
			return strconv.Quote(s)
		}
		return s
	}
}
