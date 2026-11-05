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
	files := make(map[string][]byte)
	for _, name := range []string{"justfile", "scripts/main-root.sh"} {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = data
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
				for name, data := range files {
					path := filepath.Join(dir, name)
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.MkdirAll(filepath.Join(dir, ".tmp"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			native := buildCommand(t, root, "just", "--evaluate", "native_root")
			want := filepath.Join(root, ".cache", "native")
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
			cache := buildCommand(t, worktree, "go", "env", "GOCACHE")
			if got := buildCommand(t, worktree, "just", "--command", "go", "env", "GOCACHE"); got != cache {
				t.Errorf("recipe cache = %q, want caller's Go cache %q", got, cache)
			}
			temp := buildCommand(t, worktree, "sh", "-c", `printf '%s' "${TMPDIR-}"`)
			if got := buildCommand(t, worktree, "just", "--command", "sh", "-c", `printf '%s' "${TMPDIR-}"`); got != temp {
				t.Errorf("recipe TMPDIR = %q, want caller's %q", got, temp)
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
