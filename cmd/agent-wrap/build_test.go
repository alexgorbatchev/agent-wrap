package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildCommand(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestBuildSharesNativeDependencies(t *testing.T) {
	justfile, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatal(err)
	}
	for _, location := range []string{"nested", "external"} {
		t.Run(location, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "main checkout")
			buildCommand(t, base, "git", "init", "-b", "main", root)
			buildCommand(t, root, "git", "-c", "user.name=Build Test", "-c", "user.email=build@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "fixture")
			worktree := filepath.Join(root, ".workspaces", "linked checkout")
			if location == "external" {
				worktree = filepath.Join(base, ".workspaces", "linked checkout")
			}
			buildCommand(t, root, "git", "worktree", "add", "--detach", worktree, "main")
			for _, dir := range []string{root, worktree} {
				if err := os.WriteFile(filepath.Join(dir, "justfile"), justfile, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(dir, ".tmp"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			native := buildCommand(t, root, "just", "--evaluate", "native_root")
			want := filepath.Join(root, ".tmp", "native")
			if native != want {
				t.Fatalf("main native root = %q, want %q", native, want)
			}
			for _, variable := range []string{"native_root", "native_prefix", "PKG_CONFIG_PATH"} {
				mainValue := buildCommand(t, root, "just", "--evaluate", variable)
				linkedValue := buildCommand(t, worktree, "just", "--evaluate", variable)
				if mainValue != linkedValue {
					t.Errorf("%s differs: main %q, worktree %q", variable, mainValue, linkedValue)
				}
			}
			for variable, suffix := range map[string]string{"TMPDIR": ".tmp", "GOCACHE": ".tmp/go-build-cache"} {
				got := buildCommand(t, worktree, "just", "--evaluate", variable)
				if want := filepath.Join(worktree, suffix); got != want {
					t.Errorf("%s = %q, want worktree-local %q", variable, got, want)
				}
			}
			// Execute with the recipe exports, proving consumers can read the same artifact.
			pkgConfig := buildCommand(t, root, "just", "--evaluate", "PKG_CONFIG_PATH")
			if err := os.MkdirAll(pkgConfig, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pkgConfig, "artifact"), []byte("shared dependency"), 0o600); err != nil {
				t.Fatal(err)
			}
			got := buildCommand(t, worktree, "just", "--command", "sh", "-c", `cat "$PKG_CONFIG_PATH/artifact"`)
			if got != "shared dependency" {
				t.Fatalf("worktree artifact = %q", got)
			}
		})
	}
}
