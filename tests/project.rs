use agent_wrap::{
    header::{self, State},
    project::{self, Context},
};
use std::{ffi::OsStr, fmt::Debug, fs, os::unix::ffi::OsStrExt, path::Path, process::Command};
use tokio_util::sync::CancellationToken;
fn fixture() -> tempfile::TempDir {
    fs::create_dir_all(".tmp").unwrap();
    tempfile::tempdir_in(".tmp").unwrap()
}
fn git(path: &Path, args: &[impl AsRef<OsStr> + Debug]) {
    let out = Command::new("git")
        .current_dir(path)
        .args(args)
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .output()
        .unwrap();
    assert!(
        out.status.success(),
        "{:?}: {}",
        args,
        String::from_utf8_lossy(&out.stderr)
    );
}
#[test]
fn real_git_context_tracks_origin_branch_subtree_worktree_and_detachment() {
    let dir = fixture();
    let path = fs::canonicalize(dir.path()).unwrap();
    let token = CancellationToken::new();
    let outside = project::resolve(&token, Path::new("/dev"), "").unwrap();
    assert_eq!(outside.root, Path::new("/dev"));
    assert!(!outside.split());
    git(&path, &["init", "-b", "main"]);
    git(
        &path,
        &[
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "-c",
            "commit.gpgsign=false",
            "commit",
            "--allow-empty",
            "-m",
            "fixture",
        ],
    );
    git(
        &path,
        &[
            "remote",
            "add",
            "origin",
            "git@example.invalid:owner/project.git",
        ],
    );
    git(
        &path,
        &[
            "symbolic-ref",
            "refs/remotes/origin/HEAD",
            "refs/remotes/origin/main",
        ],
    );
    let main = project::resolve(&token, &path, "trunk").unwrap();
    assert_eq!(main.identity, "example.invalid/owner/project");
    assert_eq!(main.name, "project");
    assert_eq!(main.default_branch, "main");
    assert!(!main.split());
    fs::create_dir(path.join("child")).unwrap();
    let sub = project::resolve(&token, &path.join("child"), "").unwrap();
    assert_eq!(sub.subtree, "child");
    assert!(sub.split());
    let work = path.join("linked");
    git(
        &path,
        &["worktree", "add", "-b", "feature", work.to_str().unwrap()],
    );
    let linked = project::resolve(&token, &work, "").unwrap();
    assert!(linked.worktree);
    assert_eq!(linked.identity, main.identity);
    assert_eq!(linked.branch, "feature");
    git(&path, &["checkout", "--detach"]);
    assert!(
        project::resolve(&token, &path, "")
            .unwrap()
            .branch
            .to_string_lossy()
            .starts_with("detached:")
    );
    token.cancel();
    assert!(project::resolve(&token, &path, "").is_err());
    assert!(project::resolve(&CancellationToken::new(), &path.join("absent"), "").is_err());
}
#[test]
fn git_paths_preserve_native_bytes_in_repository_and_worktree_roots() {
    let dir = fixture();
    let base = fs::canonicalize(dir.path()).unwrap();
    let path = base.join(OsStr::from_bytes(b"project-\xff"));
    fs::create_dir(&path).unwrap();
    git(&path, &["init", "-b", "main"]);
    git(
        &path,
        &[
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "-c",
            "commit.gpgsign=false",
            "commit",
            "--allow-empty",
            "-m",
            "fixture",
        ],
    );
    let token = CancellationToken::new();
    let child = path.join("child");
    fs::create_dir(&child).unwrap();
    let main = project::resolve(&token, &child, "main").unwrap();
    assert_eq!(main.root, path);
    assert_eq!(main.dir, child);
    assert_eq!(main.subtree, "child");
    assert!(!main.worktree);

    let linked = base.join(OsStr::from_bytes(b"linked-\xfe"));
    let output = Command::new("git")
        .current_dir(&path)
        .args(["worktree", "add", "-b", "feature"])
        .arg(&linked)
        .env("GIT_CONFIG_NOSYSTEM", "1")
        .output()
        .unwrap();
    assert!(output.status.success(), "{:?}", output.stderr);
    let worktree = project::resolve(&token, &linked, "main").unwrap();
    assert_eq!(worktree.root, linked);
    assert_eq!(worktree.identity, main.identity);
    assert!(worktree.worktree);
}

#[test]
fn native_byte_directories_and_git_roots_keep_separate_trail_entries() {
    let dir = tempfile::tempdir().unwrap();
    let left = dir.path().join(OsStr::from_bytes(b"project-\xff"));
    let right = dir.path().join(OsStr::from_bytes(b"project-\xfe"));
    fs::create_dir(&left).unwrap();
    fs::create_dir(&right).unwrap();
    let token = CancellationToken::new();

    for git_root in [false, true] {
        if git_root {
            git(&left, &["init", "-b", "main"]);
            git(&right, &["init", "-b", "main"]);
        }
        let left_context = project::resolve(&token, &left, "main").unwrap();
        let right_context = project::resolve(&token, &right, "main").unwrap();
        assert_ne!(left_context.identity, right_context.identity);
        assert_eq!(State::new(left_context).moved(right_context).trail.len(), 2);
    }
}

#[test]
fn native_byte_git_subtrees_keep_separate_color_keys() {
    let dir = fixture();
    git(dir.path(), &["init", "-b", "main"]);
    git(
        dir.path(),
        &[
            "remote",
            "add",
            "origin",
            "git@example.invalid:owner/project.git",
        ],
    );
    let left = dir.path().join(OsStr::from_bytes(b"subtree-\xff"));
    let right = dir.path().join(OsStr::from_bytes(b"subtree-\xfe"));
    fs::create_dir(&left).unwrap();
    fs::create_dir(&right).unwrap();
    let token = CancellationToken::new();
    let left = project::resolve(&token, &left, "main").unwrap();
    let right = project::resolve(&token, &right, "main").unwrap();
    assert_eq!(left.identity, right.identity);
    assert_ne!(left.subtree, right.subtree);
    assert_ne!(header::background(&left), header::background(&right));
}

#[test]
fn native_byte_relative_origin_identities_remain_distinct() {
    let dir = fixture();
    let left = dir.path().join(OsStr::from_bytes(b"project-\xff"));
    let right = dir.path().join(OsStr::from_bytes(b"project-\xfe"));
    for path in [&left, &right] {
        fs::create_dir(path).unwrap();
        git(path, &["init", "-b", "main"]);
        git(path, &["remote", "add", "origin", "./remote.git"]);
    }
    let token = CancellationToken::new();
    let left = project::resolve(&token, &left, "main").unwrap();
    let right = project::resolve(&token, &right, "main").unwrap();
    assert_eq!(left.name, "remote");
    assert_eq!(right.name, "remote");
    assert_ne!(left.identity, right.identity);
}

#[test]
fn native_byte_file_origin_identities_remain_distinct() {
    let root = Path::new("/repo");
    let left = project::normalize_remote("file:///repos/project-%FF.git", root).unwrap();
    let right = project::normalize_remote("file:///repos/project-%FE.git", root).unwrap();
    assert_ne!(left, right);
}

#[test]
fn native_byte_configured_origin_identities_remain_distinct() {
    let dir = fixture();
    git(dir.path(), &["init", "-b", "main"]);
    let token = CancellationToken::new();
    let resolve_with_origin = |origin: &[u8]| {
        git(
            dir.path(),
            &[
                OsStr::new("config"),
                OsStr::new("remote.origin.url"),
                OsStr::from_bytes(origin),
            ],
        );
        project::resolve(&token, dir.path(), "main").unwrap()
    };
    let left = resolve_with_origin(b"/repos/project-\xff.git");
    let right = resolve_with_origin(b"/repos/project-\xfe.git");
    assert_eq!(left.name, right.name);
    assert_ne!(left.identity, right.identity);
}

#[test]
fn native_byte_branch_refs_distinguish_the_default_and_color_keys() {
    let dir = fixture();
    git(dir.path(), &["init", "-b", "main"]);
    git(
        dir.path(),
        &[
            "remote",
            "add",
            "origin",
            "git@example.invalid:owner/project.git",
        ],
    );
    let token = CancellationToken::new();
    let set_ref = |name: &str, target: &[u8]| {
        git(
            dir.path(),
            &[
                OsStr::new("symbolic-ref"),
                OsStr::new(name),
                OsStr::from_bytes(target),
            ],
        );
    };
    set_ref("HEAD", b"refs/heads/topic-\xff");
    set_ref(
        "refs/remotes/origin/HEAD",
        b"refs/remotes/origin/topic-\xfe",
    );
    let left = project::resolve(&token, dir.path(), "main").unwrap();
    assert!(left.split());
    let child = dir.path().join("child");
    fs::create_dir(&child).unwrap();
    let left_child = project::resolve(&token, &child, "main").unwrap();
    set_ref("HEAD", b"refs/heads/topic-\xfe");
    let right = project::resolve(&token, dir.path(), "main").unwrap();
    assert!(!right.split());
    let right_child = project::resolve(&token, &child, "main").unwrap();
    assert_ne!(
        header::background(&left_child),
        header::background(&right_child)
    );

    git(
        dir.path(),
        &["symbolic-ref", "--delete", "refs/remotes/origin/HEAD"],
    );
    let fallback = OsStr::from_bytes(b"topic-\xfe");
    let supplied = project::resolve(&token, dir.path(), fallback).unwrap();
    assert_eq!(supplied.default_branch, fallback);
    assert!(!supplied.split());
}

#[test]
fn remote_identity_matches_protocols_and_redacts_invalid_credentials() {
    for remote in [
        "https://user:secret@EXAMPLE.invalid:443/owner/project.git",
        "ssh://git@example.invalid:22/owner/project.git",
        "git@example.invalid:owner/project.git",
    ] {
        assert_eq!(
            project::normalize_remote(remote, Path::new("/repo")).unwrap(),
            "example.invalid/owner/project"
        );
    }
    assert_eq!(
        project::normalize_remote("../local.git", Path::new("/repo")).unwrap(),
        "directory:/local.git"
    );
    assert_eq!(
        project::normalize_remote("file:///repo", Path::new("/repo")).unwrap(),
        "directory:/repo"
    );
    let err =
        project::normalize_remote("https://user:secret@[bad/repo", Path::new("/repo")).unwrap_err();
    assert!(!err.to_string().contains("secret"));
    assert!(project::normalize_remote("https://example.invalid/", Path::new("/repo")).is_err());
    assert!(
        Context {
            branch: "main".into(),
            ..Default::default()
        }
        .split()
    );
}
