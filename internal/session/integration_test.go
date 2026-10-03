package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type runResult struct {
	code int
	err  error
}

func runInTerminal(t *testing.T, terminal *terminal, args []string, opts Options) (<-chan runResult, context.CancelFunc) {
	t.Helper()
	opts.Input, opts.Output = terminal.slave, terminal.slave
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan runResult, 1)
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			t.Error("wrapper did not join after cancellation")
		}
	})
	go func() { code, err := Run(ctx, args, opts); done <- runResult{code, err}; close(done) }()
	return done, cancel
}

func fixtureRepository(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "trunk"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial"}, {"remote", "add", "origin", "https://example.com/test/" + name + ".git"}, {"update-ref", "refs/remotes/origin/trunk", "HEAD"}, {"symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	return dir
}

func fixtureEnvironment(t *testing.T, harness string) string {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"CLAUDE_PROJECTS_DIR", "CLAUDE_SESSIONS_DIR", "PI_SESSIONS_DIR", "CODEX_SESSIONS_DIR", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		dir := filepath.Join(root, key)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, dir)
	}
	t.Setenv("OPENCODE_DB_PATH", filepath.Join(root, "opencode.db"))
	t.Setenv("WRAP_TEST_CHILD", "1")
	t.Setenv("WRAP_TEST_HARNESS", harness)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := harness
	if harness == "claude-code" {
		name = "claude"
	}
	child := filepath.Join(root, name)
	if err := os.Symlink(executable, child); err != nil {
		t.Fatal(err)
	}
	return child
}

func TestRunAllHarnesses(t *testing.T) {
	for _, harness := range []string{"claude-code", "pi", "codex", "opencode"} {
		for _, alias := range []bool{false, true} {
			name := harness
			if alias {
				name += "-alias"
			}
			t.Run(name, func(t *testing.T) {
				child := fixtureEnvironment(t, harness)
				if alias {
					renamed := filepath.Join(filepath.Dir(child), "custom-agent")
					if err := os.Rename(child, renamed); err != nil {
						t.Fatal(err)
					}
					child = renamed
				}
				root := t.TempDir()
				first := fixtureRepository(t, root, "first")
				second := fixtureRepository(t, root, "second")
				third := fixtureRepository(t, root, "third")
				terminal := newTerminal(t)
				done, _ := runInTerminal(t, terminal, []string{child, "-test.run=^TestWrappedChild$"}, Options{Directory: first, Harness: harness, ScanInterval: 50 * time.Millisecond})
				terminal.await(t, "FRAME CHILD ONLINE")
				for _, dir := range []string{second, third, first} {
					terminal.send(t, dir+"\n")
					terminal.await(t, filepath.Base(dir)+" · ")
					if strings.Contains(terminal.text(), "waiting for agent activity") {
						t.Fatal("agent event was not observed")
					}
				}
				terminal.send(t, "quit\n")
				select {
				case result := <-done:
					if result.err != nil || result.code != 37 {
						t.Fatalf("result=%+v", result)
					}
				case <-time.After(8 * time.Second):
					t.Fatal("wrapper failed to exit")
				}
				entries, err := os.ReadDir(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "agent-wrap"))
				if err != nil || len(entries) != 0 {
					t.Fatalf("watcher cache not removed: %v, %v", entries, err)
				}
			})
		}
	}
}

func TestRunStartupFailureAndCancellation(t *testing.T) {
	for _, failure := range []bool{true, false} {
		t.Run(map[bool]string{true: "startup", false: "cancellation"}[failure], func(t *testing.T) {
			child := fixtureEnvironment(t, "pi")
			if failure {
				child = filepath.Join(t.TempDir(), "missing")
			}
			terminal := newTerminal(t)
			done, cancel := runInTerminal(t, terminal, []string{child, "-test.run=^TestWrappedChild$"}, Options{Harness: "pi", Directory: t.TempDir()})
			if !failure {
				terminal.await(t, "FRAME CHILD ONLINE")
				cancel()
			}
			select {
			case result := <-done:
				if result.err == nil {
					t.Fatal("expected frame error")
				}
			case <-time.After(8 * time.Second):
				t.Fatal("wrapper failed to join")
			}
		})
	}
}
