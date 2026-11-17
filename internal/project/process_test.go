package project

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveGitProcessCount(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, string) string
		processes int
		revParses int
		branch    string
		worktree  bool
		subtree   string
	}{
		{name: "root", processes: 4, revParses: 1, branch: "main"},
		{name: "subdirectory", prepare: func(t *testing.T, dir string) string {
			sub := filepath.Join(dir, "source files")
			if err := os.Mkdir(sub, 0755); err != nil {
				t.Fatal(err)
			}
			return sub
		}, processes: 4, revParses: 1, branch: "main", subtree: "source files"},
		{name: "detached", prepare: func(t *testing.T, dir string) string {
			git(t, dir, "checkout", "--detach")
			return dir
		}, processes: 5, revParses: 2},
		{name: "linked worktree", prepare: func(t *testing.T, dir string) string {
			linked := filepath.Join(t.TempDir(), "linked tree")
			git(t, dir, "worktree", "add", "-b", "feature", linked)
			return linked
		}, processes: 4, revParses: 1, branch: "feature", worktree: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := repo(t, "main")
			if tt.prepare != nil {
				dir = tt.prepare(t, dir)
			}
			trace := filepath.Join(t.TempDir(), "git-trace.jsonl")
			t.Setenv("GIT_TRACE2_EVENT", trace)
			got, err := Resolve(context.Background(), dir, "")
			if err != nil {
				t.Fatal(err)
			}
			if got.Identity != "github.com/owner/project" || got.Worktree != tt.worktree || got.Subtree != tt.subtree || got.DefaultBranch != "main" {
				t.Fatalf("resolved context = %+v", got)
			}
			if tt.branch != "" && got.Branch != tt.branch {
				t.Fatalf("branch = %q, want %q", got.Branch, tt.branch)
			}
			commands := tracedGitCommands(t, trace)
			revParses := 0
			for _, args := range commands {
				if len(args) > 1 && args[1] == "rev-parse" {
					revParses++
				}
			}
			if len(commands) != tt.processes || revParses != tt.revParses {
				t.Fatalf("Resolve spawned %d Git processes (%d rev-parse), want %d (%d rev-parse); commands=%v", len(commands), revParses, tt.processes, tt.revParses, commands)
			}
		})
	}
}

func TestResolveNewlineDirectories(t *testing.T) {
	for _, linked := range []bool{false, true} {
		name := "primary"
		if linked {
			name = "linked worktree"
		}
		t.Run(name, func(t *testing.T) {
			dir := repo(t, "main")
			primary := filepath.Join(t.TempDir(), "project\nname")
			if err := os.Rename(dir, primary); err != nil {
				t.Fatal(err)
			}
			dir = primary
			branch := "main"
			if linked {
				dir = filepath.Join(t.TempDir(), "worktree\nname")
				branch = "feature"
				git(t, primary, "worktree", "add", "-b", branch, dir)
			}
			got, err := Resolve(context.Background(), dir, "")
			if err != nil || got.Root != dir || got.Dir != dir || got.Branch != branch || got.Worktree != linked || got.Identity != "github.com/owner/project" || got.DefaultBranch != "main" {
				t.Fatalf("newline directory context = %+v, err=%v", got, err)
			}
		})
	}
}

// Trace2 observes the real Git processes without replacing Git or its responses.
func tracedGitCommands(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	dec := json.NewDecoder(f)
	var commands [][]string
	for {
		var event struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if err := dec.Decode(&event); errors.Is(err, io.EOF) {
			return commands
		} else if err != nil {
			t.Fatal(err)
		}
		if event.Event == "start" {
			commands = append(commands, event.Argv)
		}
	}
}
