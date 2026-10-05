package project

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	} else {
		var parseErr *url.Error
		if !errors.As(err, &parseErr) {
			t.Fatalf("malformed remote lost URL parse cause: %v", err)
		}
	}
}

func TestResolveGitFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*testing.T, string)
		want    string
	}{
		{"dubious ownership", func(t *testing.T, _ string) {
			t.Setenv("GIT_TEST_ASSUME_DIFFERENT_OWNER", "1")
			t.Setenv("GIT_CONFIG_COUNT", "1")
			t.Setenv("GIT_CONFIG_KEY_0", "safe.directory")
			t.Setenv("GIT_CONFIG_VALUE_0", "")
		}, "dubious ownership"},
		{"invalid config", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[invalid\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}, "bad config line"},
		{"explicit invalid git directory", func(t *testing.T, dir string) {
			t.Setenv("GIT_DIR", filepath.Join(dir, "missing"))
		}, "not a git repository:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t, "main")
			tc.prepare(t, dir)
			_, err := Resolve(context.Background(), dir, "")
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "git rev-parse --show-toplevel") {
				t.Fatalf("Git failure lost command or diagnostic: %v", err)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 128 {
				t.Fatalf("Git failure lost exit cause: %v", err)
			}
		})
	}
}

func TestMalformedRemoteDiagnostic(t *testing.T) {
	raw := "https://user:password-secret@[bad/repo?token=query-secret"
	_, err := normalizeRemote(raw, "/work")
	var parseErr *url.Error
	if !errors.As(err, &parseErr) || errors.Unwrap(err) != parseErr || parseErr.URL != raw || parseErr.Op != "parse" || parseErr.Err.Error() != "missing ']' in host" {
		t.Fatalf("malformed remote lost original parse cause: %v", err)
	}
	if !strings.Contains(err.Error(), parseErr.Err.Error()) || strings.Contains(err.Error(), "password-secret") || strings.Contains(err.Error(), "query-secret") {
		t.Fatalf("malformed remote diagnostic exposes raw URL or omits parse details: %v", err)
	}
}

func TestReadGitErrors(t *testing.T) {
	dir := repo(t, "main")
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	t.Setenv("LANGUAGE", "fr")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"stderr", []string{"rev-parse", "--verify", "missing-ref"}, "Needed a single revision"},
		{"silent", []string{"symbolic-ref", "--quiet", "refs/remotes/missing/HEAD"}, "exit status 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readGit(context.Background(), dir, tc.args...)
			if err == nil || !strings.Contains(err.Error(), "git "+strings.Join(tc.args, " ")) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Git failure lost command or diagnostic: %v", err)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("Git failure lost exit cause: %v", err)
			}
		})
	}
}

func TestReadGitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readGit(ctx, t.TempDir(), "rev-parse", "--show-toplevel")
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "git rev-parse --show-toplevel") {
		t.Fatalf("Git cancellation lost command or cause: %v", err)
	}
}

func TestResolveDetachedCommitFailure(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "--detach")
	if err := os.WriteFile(filepath.Join(dir, ".git", "packed-refs"), []byte("invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(context.Background(), dir, "")
	if err == nil || !strings.Contains(err.Error(), "git rev-parse --short HEAD") || !strings.Contains(err.Error(), "unexpected line in .git/packed-refs: invalid") {
		t.Fatalf("detached commit failure lost command or diagnostic: %v", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 128 || !strings.Contains(string(exitErr.Stderr), "unexpected line") {
		t.Fatalf("detached commit failure lost its exit cause: %v", err)
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
