use agent_wrap::project::{self, Context};
use std::{fs, path::Path, process::Command};
use tokio_util::sync::CancellationToken;
fn fixture() -> tempfile::TempDir {
    fs::create_dir_all(".tmp").unwrap();
    tempfile::tempdir_in(".tmp").unwrap()
}
fn git(path: &Path, args: &[&str]) {
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
            .starts_with("detached:")
    );
    token.cancel();
    assert!(project::resolve(&token, &path, "").is_err());
    assert!(project::resolve(&CancellationToken::new(), &path.join("absent"), "").is_err());
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
