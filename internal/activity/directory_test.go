package activity

import (
	"os"
	"path/filepath"
	"testing"

	parser "github.com/alexgorbatchev/agent-parser"
	watcher "github.com/alexgorbatchev/agent-watcher"
)

func TestReportedDirectory(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "src")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		input map[string]interface{}
		want  string
	}{
		{"workdir", map[string]interface{}{"workdir": sub}, sub},
		{"read", map[string]interface{}{"file_path": "src/missing.go"}, sub},
		{"pi", map[string]interface{}{"path": "src/file.ts"}, sub},
		{"opencode", map[string]interface{}{"filePath": filepath.Join(sub, "file.go")}, sub},
		{"search", map[string]interface{}{"path": "src"}, sub},
		{"no target", map[string]interface{}{"query": "hello"}, base},
		{"invalid directory", map[string]interface{}{"workdir": "missing"}, base},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := watcher.Event{ParsedEvent: parser.ParsedEvent{EventType: "tool_call", CWD: base, Data: parser.EventData{ToolInput: tc.input}}}
			if got := Directory(e, base); got != tc.want {
				t.Fatalf("directory=%q want=%q", got, tc.want)
			}
		})
	}
	e := watcher.Event{ParsedEvent: parser.ParsedEvent{EventType: "tool_result", CWD: base}}
	if Directory(e, sub) != sub {
		t.Fatal("result reset tool directory")
	}
	e.Harness = watcher.HarnessOpencode
	e.EventType = "tool_use"
	e.Data.ToolInput = map[string]interface{}{"filePath": filepath.Join(sub, "file.go")}
	if Directory(e, base) != sub {
		t.Fatal("OpenCode's native tool_use event was ignored")
	}
	e.Harness = watcher.HarnessCodex
	e.Data.ToolInput = nil
	e.EventType = "tool_call"
	e.CWD = ""
	e.Data.FileModification = &parser.FileModification{FilePath: filepath.Join(sub, "new.go")}
	if Directory(e, base) != sub {
		t.Fatal("ignored normalized file modification")
	}
	for _, path := range []string{"", "https://example.com/file", "bad\npath", "missing/parent/file"} {
		if existingDirectory(path, base, true) != "" {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
}

func TestHarnessSelection(t *testing.T) {
	for _, name := range []string{"claude", "pi", "codex", "opencode"} {
		if _, err := Harness("/bin/"+name, ""); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := Harness("/bin/my-agent", "pi"); err != nil || got != watcher.HarnessPi {
		t.Fatal(got, err)
	}
	if _, err := Harness("unknown", ""); err == nil {
		t.Fatal("accepted unknown agent without override")
	}
	if _, err := Harness("claude", "unknown"); err == nil {
		t.Fatal("accepted unknown harness")
	}
}
