package logging

import (
	"bytes"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/slogtest"
)

func TestHumanHandlerContract(t *testing.T) {
	var buf bytes.Buffer
	newHandler := func(t *testing.T) slog.Handler {
		buf.Reset()
		return newHumanHandler(&buf, slog.LevelInfo)
	}
	result := func(t *testing.T) map[string]any {
		return parseHumanRecord(t, buf.String())
	}
	slogtest.Run(t, newHandler, result)
}

// Parse the human representation, including its actual time and level fields,
// so slogtest verifies the emitted record rather than a second handler's output.
func parseHumanRecord(t *testing.T, line string) map[string]any {
	t.Helper()
	tokens := regexp.MustCompile(`(?:"(?:\\.|[^"\\])*"|[^\s"])+`).FindAllString(strings.TrimSpace(line), -1)
	m := make(map[string]any)
	if len(tokens) > 0 && !strings.HasPrefix(tokens[0], "[") {
		m[slog.TimeKey] = tokens[0]
		tokens = tokens[1:]
	}
	if len(tokens) < 2 || !strings.HasPrefix(tokens[0], "[") {
		t.Fatalf("invalid human record %q", line)
	}
	m[slog.LevelKey] = strings.Trim(tokens[0], "[]")
	m[slog.MessageKey] = tokens[1]
	for _, token := range tokens[2:] {
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			t.Fatalf("invalid attribute %q", token)
		}
		if strings.HasPrefix(value, `"`) {
			var err error
			value, err = strconv.Unquote(value)
			if err != nil {
				t.Fatal(err)
			}
		}
		group := m
		parts := strings.Split(key, ".")
		for _, name := range parts[:len(parts)-1] {
			if group[name] == nil {
				group[name] = make(map[string]any)
			}
			group = group[name].(map[string]any)
		}
		group[parts[len(parts)-1]] = value
	}
	return m
}

func TestHumanGroupRegressions(t *testing.T) {
	for _, tc := range []struct {
		name string
		log  func(*slog.Logger)
		want string
	}{
		{"pre-group attributes", func(l *slog.Logger) {
			l.With("service", "agent-wrap", "a", 1).WithGroup("g").Info("m", "b", 2, "", "emptykey", slog.Group("grp", "x", 1))
		}, "[INFO] m a=1 g.b=2 g.=emptykey g.grp.x=1"},
		{"zero and inline group", func(l *slog.Logger) {
			l.Info("zero", slog.Attr{}, slog.Group("", "inl", 1))
		}, "[INFO] zero inl=1"},
		{"service in groups", func(l *slog.Logger) {
			l.WithGroup("g").With("service", "bound", "a", 1).Info("m", "service", "record", slog.Group("nested", "service", "nested", "x", 2))
		}, "[INFO] m g.a=1 g.nested.x=2"},
		{"empty nested groups", func(l *slog.Logger) {
			l.With(slog.Group("empty", slog.Attr{})).WithGroup("g").Info("m", slog.Group("empty", slog.Group("nested")), "x", 1)
		}, "[INFO] m g.x=1"},
		{"ordinary ID fields", func(l *slog.Logger) {
			l.With("env", "machine", "envId", "e").Info("m", "project", "repo", "projectId", "p")
		}, "[INFO] m env=machine envId=e project=repo projectId=p"},
		{"independent derived loggers", func(l *slog.Logger) {
			left := l.With("a", 1).WithGroup("left").With("b", 2)
			l.WithGroup("right").With("c", 3).Info("right", "d", 4)
			left.Info("left", "e", 5)
		}, "[INFO] right right.c=3 right.d=4\n[INFO] left a=1 left.b=2 left.e=5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.log(slog.New(newHumanHandler(&buf, slog.LevelInfo)))
			var records []string
			for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
				_, record, ok := strings.Cut(line, " ")
				if !ok {
					t.Fatalf("invalid output %q", line)
				}
				records = append(records, record)
			}
			if got := strings.Join(records, "\n"); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
