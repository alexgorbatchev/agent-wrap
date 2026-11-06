package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Exercise early native-build failure and signal cleanup through real Git,
// just, and the diagnostic. The full native assertions remain in the diagnostic;
// this fixture fails its first build after proving the child retains scratch.
func TestNativeIsolationFailureCleanup(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"justfile", "scripts/check-native-isolation.sh", "scripts/native-git.sh", "scripts/main-root.sh", "scripts/with-build-scratch"} {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := `set -eu
if test "$CHECK_SIGNAL" != 0; then kill -"$CHECK_SIGNAL" "$CHECK_PID"; fi
sleep 0.05
printf retained > "$TMPDIR/still-owned"
printf '%s\n' "$PWD" > "$CHECK_FINISHED"
printf 'native failure evidence\n' >&2
exit 7
`
	if err := os.WriteFile(filepath.Join(root, "scripts/native.sh"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	buildCommand(t, root, "git", "init", "-b", "main")
	buildCommand(t, root, "git", "add", ".")
	buildCommand(t, root, "git", "-c", "user.name=Build Test", "-c", "user.email=build@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
	before := buildCommand(t, root, "git", "rev-parse", "HEAD")
	for _, signal := range []int{0, 1, 2, 3, 15} {
		t.Run(strconv.Itoa(signal), func(t *testing.T) {
			t.Parallel()
			marker := filepath.Join(t.TempDir(), "finished")
			cmd := exec.Command("bash", "-c", `export CHECK_PID=$$; exec bash "$1"`, "probe", filepath.Join(root, "scripts/check-native-isolation.sh"))
			cmd.Env = append(os.Environ(), "CHECK_SIGNAL="+strconv.Itoa(signal), "CHECK_FINISHED="+marker)
			output, err := cmd.CombinedOutput()
			status := 1
			if signal != 0 {
				status = 128 + signal
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != status {
				t.Fatalf("status %d: %v: %s", status, err, output)
			}
			data, err := os.ReadFile(marker)
			if err != nil {
				t.Fatalf("native child lost scratch before finishing: %v: %s", err, output)
			}
			scratch := filepath.Dir(strings.TrimSpace(string(data)))
			canonicalRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Dir(scratch) != filepath.Join(canonicalRoot, ".tmp") || !strings.HasPrefix(filepath.Base(scratch), "native-isolation.") {
				t.Fatalf("scratch escaped checkout: %q", scratch)
			}
			if _, err := os.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("diagnostic scratch survived exit: %v", err)
			}
			if signal == 0 && !strings.Contains(string(output), "native failure evidence") {
				t.Fatalf("native failure evidence lost: %s", output)
			}
			if head := buildCommand(t, root, "git", "rev-parse", "HEAD"); head != before {
				t.Fatalf("source HEAD changed: %s", head)
			}
			if trees := buildCommand(t, root, "git", "worktree", "list", "--porcelain"); strings.Count(trees, "worktree ") != 1 {
				t.Fatalf("diagnostic registered a source worktree: %s", trees)
			}
		})
	}
}
