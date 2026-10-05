// Package project resolves the Git context of reported agent directories.
package project

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Context is the current project and its branch, worktree, and subtree.
type Context struct {
	Identity, Name, Dir, Root, Branch, DefaultBranch, Subtree string
	Worktree                                                  bool
}

// Split reports whether the context needs a project pin and a subcolor.
func (c Context) Split() bool {
	return c.Subtree != "" || c.Worktree || (c.Branch != "" && c.Branch != c.DefaultBranch)
}

// Resolve reads local Git metadata without contacting the remote.
func Resolve(ctx context.Context, dir, defaultBranch string) (Context, error) {
	if err := ctx.Err(); err != nil {
		return Context{}, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Context{}, fmt.Errorf("resolve directory: %w", err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Context{}, fmt.Errorf("resolve directory: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Context{}, fmt.Errorf("directory is unavailable: %s", abs)
	}
	c := Context{Identity: "directory:" + abs, Name: filepath.Base(abs), Dir: abs, Root: abs}
	root, err := readGit(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Context{}, fmt.Errorf("git is required on PATH to identify projects: %w", err)
		}
		if ctx.Err() != nil {
			return Context{}, ctx.Err()
		}
		if outsideRepository(err) {
			return c, nil
		}
		return Context{}, fmt.Errorf("read Git root: %w", err)
	}
	c.Root = root
	c.Name = filepath.Base(root)
	common, err := readGit(ctx, abs, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Context{}, fmt.Errorf("read Git common directory: %w", err)
	}
	gitDir, err := readGit(ctx, abs, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return Context{}, fmt.Errorf("read Git directory: %w", err)
	}
	c.Worktree = filepath.Clean(common) != filepath.Clean(gitDir)
	c.Identity = "directory:" + filepath.Clean(common)
	if filepath.Base(common) == ".git" {
		c.Identity = "directory:" + filepath.Dir(common)
	}
	remote, remoteErr := readGit(ctx, abs, "remote", "get-url", "origin")
	if remoteErr == nil {
		c.Identity, err = normalizeRemote(remote, root)
		if err != nil {
			return Context{}, err
		}
		c.Name = strings.TrimSuffix(filepath.Base(c.Identity), ".git")
	} else if !gitFailure(remoteErr, 2, "error: No such remote 'origin'") {
		return Context{}, fmt.Errorf("read Git remote: %w", remoteErr)
	}
	c.Branch, err = readGit(ctx, abs, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if !gitFailure(err, 1, "") {
			return Context{}, fmt.Errorf("read Git branch: %w", err)
		}
		commit, commitErr := readGit(ctx, abs, "rev-parse", "--short", "HEAD")
		if commitErr != nil {
			return Context{}, fmt.Errorf("read Git branch: %w", commitErr)
		}
		c.Branch = "detached:" + commit
	}
	c.DefaultBranch = defaultBranch
	ref, refErr := readGit(ctx, abs, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if refErr == nil {
		c.DefaultBranch = strings.TrimPrefix(ref, "refs/remotes/origin/")
	} else if !gitFailure(refErr, 1, "") {
		return Context{}, fmt.Errorf("read Git default branch: %w", refErr)
	}
	sub, err := filepath.Rel(root, abs)
	if err != nil {
		return Context{}, fmt.Errorf("resolve subtree: %w", err)
	}
	if sub != "." {
		c.Subtree = filepath.ToSlash(sub)
	}
	if err := ctx.Err(); err != nil {
		return Context{}, err
	}
	return c, nil
}

func readGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Classification of expected Git failures requires stable diagnostic text.
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANGUAGE=C")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), ctx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if diagnostic := strings.TrimSpace(string(exitErr.Stderr)); diagnostic != "" {
				return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), diagnostic, err)
			}
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitFailure(err error, code int, diagnostic string) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code && strings.TrimSpace(string(exitErr.Stderr)) == diagnostic
}

func outsideRepository(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 128 {
		return false
	}
	diagnostic := strings.TrimSpace(string(exitErr.Stderr))
	return diagnostic == "fatal: not a git repository (or any of the parent directories): .git" ||
		strings.HasPrefix(diagnostic, "fatal: not a git repository (or any parent up to mount point ")
}

type remoteIdentityError struct {
	err error
}

func (e remoteIdentityError) Error() string {
	// url.Parse wraps its cause in url.Error, whose display includes the raw
	// remote URL. Keep that original error inspectable without displaying secrets.
	return fmt.Sprintf("invalid Git remote identity: %v", errors.Unwrap(e.err))
}

func (e remoteIdentityError) Unwrap() error { return e.err }

func normalizeRemote(raw, root string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		colon := strings.IndexByte(raw, ':')
		if colon > 0 && !strings.Contains(raw[:colon], "/") {
			host := raw[:colon]
			if _, after, ok := strings.Cut(host, "@"); ok {
				host = after
			}
			raw = "ssh://" + host + "/" + raw[colon+1:]
		} else {
			if !filepath.IsAbs(raw) {
				raw = filepath.Join(root, raw)
			}
			return "directory:" + filepath.Clean(raw), nil
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", remoteIdentityError{err: err}
	}
	if u.Scheme == "file" {
		return "directory:" + filepath.Clean(u.Path), nil
	}
	host := strings.ToLower(u.Host)
	for _, suffix := range []string{":22", ":443", ":80"} {
		host = strings.TrimSuffix(host, suffix)
	}
	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if host == "" || path == "" {
		return "", errors.New("git remote has no repository identity")
	}
	return host + "/" + path, nil
}
