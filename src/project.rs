use crate::{Error, Result};
use path_clean::PathClean;
use std::{
    ffi::{OsStr, OsString},
    io::{self, Read},
    os::unix::ffi::{OsStrExt, OsStringExt},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    thread,
    time::Duration,
};
use tokio_util::sync::CancellationToken;

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Context {
    pub identity: OsString,
    pub name: String,
    pub dir: PathBuf,
    pub root: PathBuf,
    pub branch: OsString,
    pub default_branch: OsString,
    pub subtree: OsString,
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
fn git(token: &CancellationToken, dir: &Path, args: &[&str]) -> Result<Vec<u8>> {
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
    Ok(out)
}
fn trim_git_value(value: &OsStr) -> &OsStr {
    let display = value.to_string_lossy();
    let without_leading = display.trim_start();
    let leading = display.len() - without_leading.len();
    let trailing = without_leading.len() - without_leading.trim_end().len();
    // Replacement characters are never whitespace, so trimmed edge lengths match the original.
    OsStr::from_bytes(&value.as_bytes()[leading..value.len() - trailing])
}
fn git_ref(token: &CancellationToken, dir: &Path, args: &[&str]) -> Result<OsString> {
    let out = git(token, dir, args)?;
    Ok(trim_git_value(OsStr::from_bytes(&out)).into())
}
fn git_path(token: &CancellationToken, dir: &Path, args: &[&str]) -> Result<PathBuf> {
    let mut out = git(token, dir, args)?;
    if out.last() == Some(&b'\n') {
        out.pop();
    }
    Ok(OsString::from_vec(out).into())
}
fn failure(error: &Error, code: i32, diagnostic: &str) -> bool {
    matches!(error,Error::Git{status,diagnostic:text,..} if *status==code && text==diagnostic)
}
fn directory_identity(path: &Path) -> OsString {
    let mut identity = OsString::from("directory:");
    identity.push(path);
    identity
}
pub fn resolve(
    token: &CancellationToken,
    dir: &Path,
    default_branch: impl AsRef<OsStr>,
) -> Result<Context> {
    cancelled(token)?;
    let dir = std::fs::canonicalize(dir)?;
    if !dir.is_dir() {
        return Err(Error::Invalid(format!(
            "directory unavailable: {}",
            cli_command_kit::display_path(&dir)
        )));
    }
    let mut ctx = Context {
        identity: directory_identity(&dir),
        name: dir
            .file_name()
            .unwrap_or(dir.as_os_str())
            .to_string_lossy()
            .into(),
        dir: dir.clone(),
        root: dir.clone(),
        ..Default::default()
    };
    ctx.root = match git_path(token, &dir, &["rev-parse", "--show-toplevel"]) {
        Ok(root) => root,
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
    let common = git_path(
        token,
        &dir,
        &["rev-parse", "--path-format=absolute", "--git-common-dir"],
    )?
    .clean();
    let gitdir = git_path(token, &dir, &["rev-parse", "--absolute-git-dir"])?.clean();
    ctx.worktree = common != gitdir;
    let identity = if common.file_name().is_some_and(|n| n == ".git") {
        common.parent().unwrap_or(&common)
    } else {
        &common
    };
    ctx.identity = directory_identity(identity);
    match git(token, &dir, &["remote", "get-url", "origin"]) {
        Ok(remote) => {
            let identity = normalize_remote(OsStr::from_bytes(&remote), &ctx.root)?;
            let display_identity = identity.to_string_lossy();
            ctx.name = display_identity
                .rsplit('/')
                .next()
                .unwrap_or(&display_identity)
                .trim_end_matches(".git")
                .into();
            ctx.identity = identity;
        }
        Err(e) if failure(&e, 2, "error: No such remote 'origin'") => {}
        Err(e) => return Err(e),
    };
    ctx.branch = match git_ref(token, &dir, &["symbolic-ref", "--quiet", "--short", "HEAD"]) {
        Ok(branch) => branch,
        Err(e) if failure(&e, 1, "") => {
            let mut branch = OsString::from("detached:");
            branch.push(git_ref(token, &dir, &["rev-parse", "--short", "HEAD"])?);
            branch
        }
        Err(e) => return Err(e),
    };
    ctx.default_branch = default_branch.as_ref().into();
    match git_ref(
        token,
        &dir,
        &["symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"],
    ) {
        Ok(reference) => {
            let bytes = reference.as_bytes();
            ctx.default_branch =
                OsStr::from_bytes(bytes.strip_prefix(b"refs/remotes/origin/").unwrap_or(bytes))
                    .into();
        }
        Err(e) if failure(&e, 1, "") => {}
        Err(e) => return Err(e),
    };
    ctx.subtree = dir
        .strip_prefix(&ctx.root)
        .map_err(|_| Error::Invalid("Git root does not contain the working directory".into()))?
        .as_os_str()
        .to_owned();
    cancelled(token)?;
    Ok(ctx)
}
pub fn normalize_remote(raw: impl AsRef<OsStr>, root: &Path) -> Result<OsString> {
    let raw = trim_git_value(raw.as_ref());
    let mut text = raw.to_string_lossy();
    if !text.contains("://") {
        if let Some((host, path)) = text
            .split_once(':')
            .filter(|(host, _)| !host.is_empty() && !host.contains('/'))
        {
            text = format!("ssh://{}/{}", host.rsplit('@').next().unwrap_or(host), path).into();
        } else {
            return Ok(directory_identity(&root.join(raw).clean()));
        }
    }
    let url = url::Url::parse(&text)?;
    if url.scheme() == "file" {
        let path = url
            .to_file_path()
            .map_err(|_| Error::Invalid("invalid local Git remote path".into()))?;
        return Ok(directory_identity(&path.clean()));
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
    Ok(format!("{host}/{path}").into())
}
