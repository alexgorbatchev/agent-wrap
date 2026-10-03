package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func repo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", branch)
	git(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
	git(t, dir, "remote", "add", "origin", "git@github.com:owner/project.git")
	git(t, dir, "update-ref", "refs/remotes/origin/"+branch, "HEAD")
	git(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+branch)
	return dir
}

func TestRemoteIdentity(t *testing.T) {
	for _, remote := range []string{"git@github.com:owner/project.git", "https://github.com/owner/project.git", "ssh://git@GitHub.com/owner/project/", "https://user:secret@github.com/owner/project.git?token=secret"} {
		t.Run(remote[:3], func(t *testing.T) {
			got, err := normalizeRemote(remote, "/work")
			if err != nil || got != "github.com/owner/project" {
				t.Fatalf("identity = %q, err = %v", got, err)
			}
		})
	}
	for _, tc := range []struct{ remote, want string }{
		{"ssh://git@example.com:2222/a.git", "example.com:2222/a"},
		{"../repo.git", "directory:/repo.git"},
		{"file:///work/repo.git", "directory:/work/repo.git"},
	} {
		got, err := normalizeRemote(tc.remote, "/work")
		if err != nil || got != tc.want {
			t.Fatalf("normalize = %q, err = %v, want %q", got, err, tc.want)
		}
	}
	if _, err := normalizeRemote("https://example.com", "/work"); err == nil {
		t.Fatal("accepted missing repository path")
	}
	if _, err := normalizeRemote("https://[bad", "/work"); err == nil {
		t.Fatal("accepted malformed remote")
	}
}

func TestResolveContexts(t *testing.T) {
	dir := repo(t, "trunk")
	ctx := context.Background()
	root, err := Resolve(ctx, dir, "")
	if err != nil || root.Identity != "github.com/owner/project" || root.Split() || root.Branch != "trunk" || root.DefaultBranch != "trunk" {
		t.Fatalf("root = %+v, err = %v", root, err)
	}
	sub := filepath.Join(dir, "src")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	nested, err := Resolve(ctx, sub, "")
	if err != nil || !nested.Split() || nested.Subtree != "src" || nested.Identity != root.Identity {
		t.Fatalf("subtree = %+v, err = %v", nested, err)
	}
	git(t, dir, "checkout", "-b", "feature")
	feature, err := Resolve(ctx, dir, "")
	if err != nil || !feature.Split() || feature.Branch != "feature" {
		t.Fatalf("feature = %+v, err = %v", feature, err)
	}
	worktree := filepath.Join(t.TempDir(), "worktree")
	git(t, dir, "worktree", "add", "-b", "other", worktree)
	linked, err := Resolve(ctx, worktree, "")
	if err != nil || linked.Identity != root.Identity || !linked.Worktree || !linked.Split() {
		t.Fatalf("worktree = %+v, err = %v", linked, err)
	}
	git(t, dir, "checkout", "--detach")
	detached, err := Resolve(ctx, dir, "")
	if err != nil || !detached.Split() || detached.Branch == "" {
		t.Fatalf("detached = %+v, err = %v", detached, err)
	}
}

func TestResolveFallbacks(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	ctx := context.Background()
	dir := repo(t, "master")
	git(t, dir, "remote", "remove", "origin")
	p, err := Resolve(ctx, dir, "master")
	if err != nil || p.Identity != "directory:"+dir || p.Split() {
		t.Fatalf("local = %+v, %v", p, err)
	}
	p, err = Resolve(ctx, dir, "")
	if err != nil || p.DefaultBranch != "" || !p.Split() {
		t.Fatalf("unknown default = %+v, %v", p, err)
	}
	plain := t.TempDir()
	p, err = Resolve(ctx, plain, "")
	if err != nil || p.Identity != "directory:"+plain || p.Split() {
		t.Fatalf("plain = %+v, %v", p, err)
	}
	if _, err := Resolve(ctx, filepath.Join(plain, "missing"), ""); err == nil {
		t.Fatal("accepted missing directory")
	}
	file := filepath.Join(plain, "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(ctx, file, ""); err == nil {
		t.Fatal("accepted file as working directory")
	}
	git(t, dir, "remote", "add", "origin", "https://example.com")
	if _, err := Resolve(ctx, dir, ""); err == nil {
		t.Fatal("accepted remote without identity")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Resolve(cancelled, dir, ""); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestResolveRequiresGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Resolve(context.Background(), t.TempDir(), ""); err == nil {
		t.Fatal("missing Git silently changed project identity")
	}
}

func TestDefaultBranchFallbackIsRepositorySpecific(t *testing.T) {
	dir := repo(t, "trunk")
	p, err := Resolve(context.Background(), dir, "main")
	if err != nil || p.DefaultBranch != "trunk" || p.Split() {
		t.Fatalf("fallback replaced a known repository default: %+v, %v", p, err)
	}
	git(t, dir, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	p, err = Resolve(context.Background(), dir, "trunk")
	if err != nil || p.DefaultBranch != "trunk" || p.Split() {
		t.Fatalf("missing repository default did not use fallback: %+v, %v", p, err)
	}
}
