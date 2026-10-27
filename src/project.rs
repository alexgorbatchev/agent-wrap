use crate::{Error, Result};
use path_clean::PathClean;
use std::{
    io::{self, Read},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    thread,
    time::Duration,
};
use tokio_util::sync::CancellationToken;

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Context {
    pub identity: String,
    pub name: String,
    pub dir: PathBuf,
    pub root: PathBuf,
    pub branch: String,
    pub default_branch: String,
    pub subtree: String,
    pub worktree: bool,
}
impl Context {
    pub fn split(&self) -> bool {
        !self.subtree.is_empty()
            || self.worktree
            || (!self.branch.is_empty() && self.branch != self.default_branch)
    }
}
fn cancelled(token: &CancellationToken) -> Result<()> {
    if token.is_cancelled() {
        Err(io::Error::new(io::ErrorKind::Interrupted, "operation cancelled").into())
    } else {
        Ok(())
    }
}
fn git(token: &CancellationToken, dir: &Path, args: &[&str]) -> Result<String> {
    cancelled(token)?;
    let mut child = Command::new("git")
        .current_dir(dir)
        .args(args)
        .env("LC_ALL", "C")
        .env("LANGUAGE", "C")
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| io::Error::new(e.kind(), format!("starting git (required on PATH): {e}")))?;
    let mut stdout = child.stdout.take().expect("piped stdout");
    let mut stderr = child.stderr.take().expect("piped stderr");
    let (status, out, err) = thread::scope(|scope| -> io::Result<_> {
        let out = scope.spawn(move || {
            let mut b = Vec::new();
            stdout.read_to_end(&mut b).map(|_| b)
        });
        let err = scope.spawn(move || {
            let mut b = Vec::new();
            stderr.read_to_end(&mut b).map(|_| b)
        });
        let status = loop {
            if token.is_cancelled() {
                child.kill()?;
                child.wait()?;
                return Err(io::Error::new(
                    io::ErrorKind::Interrupted,
                    "operation cancelled",
                ));
            }
            if let Some(status) = child.try_wait()? {
                break status;
            }
            thread::sleep(Duration::from_millis(5));
        };
        Ok((
            status,
            out.join()
                .map_err(|_| io::Error::other("Git stdout reader panicked"))??,
            err.join()
                .map_err(|_| io::Error::other("Git stderr reader panicked"))??,
        ))
    })?;
    if !status.success() {
        return Err(Error::Git {
            args: args.join(" "),
            status: status.code().unwrap_or(-1),
            diagnostic: String::from_utf8_lossy(&err).trim().into(),
        });
    }
    cancelled(token)?;
    Ok(String::from_utf8_lossy(&out).trim().into())
}
fn failure(error: &Error, code: i32, diagnostic: &str) -> bool {
    matches!(error,Error::Git{status,diagnostic:text,..} if *status==code && text==diagnostic)
}
pub fn resolve(token: &CancellationToken, dir: &Path, default_branch: &str) -> Result<Context> {
    cancelled(token)?;
    let dir = std::fs::canonicalize(dir)?;
    if !dir.is_dir() {
        return Err(Error::Invalid(format!(
            "directory unavailable: {}",
            cli_command_kit::display_path(&dir)
        )));
    }
    let mut ctx = Context {
        identity: format!("directory:{}", dir.display()),
        name: dir
            .file_name()
            .unwrap_or(dir.as_os_str())
            .to_string_lossy()
            .into(),
        dir: dir.clone(),
        root: dir.clone(),
        ..Default::default()
    };
    ctx.root = match git(token, &dir, &["rev-parse", "--show-toplevel"]) {
        Ok(root) => root.into(),
        Err(Error::Git {
            status: 128,
            ref diagnostic,
            ..
        }) if diagnostic
            == "fatal: not a git repository (or any of the parent directories): .git"
            || diagnostic
                .starts_with("fatal: not a git repository (or any parent up to mount point ") =>
        {
            return Ok(ctx);
        }
        Err(e) => return Err(e),
    };
    ctx.name = ctx
        .root
        .file_name()
        .unwrap_or(ctx.root.as_os_str())
        .to_string_lossy()
        .into();
    let common = PathBuf::from(git(
        token,
        &dir,
        &["rev-parse", "--path-format=absolute", "--git-common-dir"],
    )?)
    .clean();
    let gitdir = PathBuf::from(git(token, &dir, &["rev-parse", "--absolute-git-dir"])?).clean();
    ctx.worktree = common != gitdir;
    let identity = if common.file_name().is_some_and(|n| n == ".git") {
        common.parent().unwrap_or(&common)
    } else {
        &common
    };
    ctx.identity = format!("directory:{}", identity.display());
    match git(token, &dir, &["remote", "get-url", "origin"]) {
        Ok(remote) => {
            ctx.identity = normalize_remote(&remote, &ctx.root)?;
            ctx.name = ctx
                .identity
                .rsplit('/')
                .next()
                .unwrap_or(&ctx.identity)
                .trim_end_matches(".git")
                .into();
        }
        Err(e) if failure(&e, 2, "error: No such remote 'origin'") => {}
        Err(e) => return Err(e),
    };
    ctx.branch = match git(token, &dir, &["symbolic-ref", "--quiet", "--short", "HEAD"]) {
        Ok(branch) => branch,
        Err(e) if failure(&e, 1, "") => format!(
            "detached:{}",
            git(token, &dir, &["rev-parse", "--short", "HEAD"])?
        ),
        Err(e) => return Err(e),
    };
    ctx.default_branch = default_branch.into();
    match git(
        token,
        &dir,
        &["symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"],
    ) {
        Ok(reference) => {
            ctx.default_branch = reference.trim_start_matches("refs/remotes/origin/").into()
        }
        Err(e) if failure(&e, 1, "") => {}
        Err(e) => return Err(e),
    };
    ctx.subtree = dir
        .strip_prefix(&ctx.root)
        .map_err(|_| Error::Invalid("Git root does not contain the working directory".into()))?
        .to_string_lossy()
        .replace(std::path::MAIN_SEPARATOR, "/");
    cancelled(token)?;
    Ok(ctx)
}
pub fn normalize_remote(raw: &str, root: &Path) -> Result<String> {
    let mut raw = raw.trim().to_owned();
    if !raw.contains("://") {
        if let Some((host, path)) = raw
            .split_once(':')
            .filter(|(host, _)| !host.is_empty() && !host.contains('/'))
        {
            raw = format!("ssh://{}/{}", host.rsplit('@').next().unwrap_or(host), path);
        } else {
            return Ok(format!("directory:{}", root.join(raw).clean().display()));
        }
    }
    let url = url::Url::parse(&raw)?;
    if url.scheme() == "file" {
        let path = url
            .to_file_path()
            .map_err(|_| Error::Invalid("invalid local Git remote path".into()))?;
        return Ok(format!("directory:{}", path.clean().display()));
    }
    let mut host = url.host_str().unwrap_or_default().to_ascii_lowercase();
    if let Some(port) = url.port().filter(|p| ![22, 80, 443].contains(p)) {
        host.push_str(&format!(":{port}"));
    }
    let path = url.path().trim_matches('/').trim_end_matches(".git");
    if host.is_empty() || path.is_empty() {
        return Err(Error::Invalid(
            "Git remote has no repository identity".into(),
        ));
    }
    Ok(format!("{host}/{path}"))
}
