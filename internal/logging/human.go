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
	attrs  string
	prefix string
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
	var buf bytes.Buffer
	buf.WriteString(h.attrs)
	for _, a := range attrs {
		appendHumanAttr(&buf, h.prefix, a)
	}
	next := *h
	next.attrs = buf.String()
	return &next
}

func (h *humanHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.prefix += name + "."
	return &next
}

func (h *humanHandler) Handle(_ context.Context, r slog.Record) error {
	var buf bytes.Buffer
	if !r.Time.IsZero() {
		buf.WriteString(r.Time.Format("15:04:05"))
		buf.WriteString(" ")
	}
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

	buf.WriteString(h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		appendHumanAttr(&buf, h.prefix, a)
		return true
	})
	buf.WriteString("\n")

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf.Bytes())
	return err
}

func appendHumanAttr(buf *bytes.Buffer, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	// Human log files belong to this wrapper; omit the logger's service field.
	if a.Key == "service" {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, child := range a.Value.Group() {
			appendHumanAttr(buf, prefix, child)
		}
		return
	}
	buf.WriteString(" ")
	buf.WriteString(prefix)
	buf.WriteString(a.Key)
	buf.WriteString("=")
	buf.WriteString(formatAttrValue(a.Value))
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
