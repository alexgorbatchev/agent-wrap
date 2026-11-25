use agent_wrap::files::{Locations, RETENTION, RuntimeFiles, lock_file, prune};
use cli_command_kit::Mode;
use std::{
    fs,
    path::Path,
    time::{Duration, SystemTime},
};
fn fixture() -> tempfile::TempDir {
    fs::create_dir_all(".tmp").unwrap();
    tempfile::tempdir_in(".tmp").unwrap()
}
fn old(path: &Path) {
    filetime::set_file_mtime(
        path,
        filetime::FileTime::from_system_time(
            SystemTime::now() - RETENTION - Duration::from_secs(60),
        ),
    )
    .unwrap();
}
#[test]
fn ownership_retention_and_cleanup_preserve_live_sessions_and_explicit_destinations() {
    let dir = fixture();
    let paths = Locations {
        cache: dir.path().join("cache"),
        state: dir.path().join("state"),
    };
    let first = RuntimeFiles::open(&paths, None, Mode::Agent).unwrap();
    let cache = first.cache.clone();
    let log = first.log_path.clone();
    first
        .logging
        .with_default(|| tracing::info!("session_ready"));
    assert!(
        fs::read_to_string(&log)
            .unwrap()
            .contains("action=session_ready")
    );
    old(&cache);
    old(&log);
    let orphan = paths.cache.join("session-orphan");
    fs::create_dir(&orphan).unwrap();
    old(&orphan);
    let orphanlog = log.parent().unwrap().join("session-orphan.log");
    fs::write(&orphanlog, "old").unwrap();
    old(&orphanlog);
    let second = RuntimeFiles::open(&paths, None, Mode::Human).unwrap();
    assert!(cache.exists());
    assert!(log.exists());
    assert!(!orphan.exists());
    assert!(!orphanlog.exists());
    assert!(RuntimeFiles::open(&paths, Some(&log), Mode::Human).is_err());
    assert_eq!(fs::read_dir(&paths.cache).unwrap().count(), 2);
    second.close().unwrap();
    first.close().unwrap();
    assert!(!cache.exists());
    assert!(log.exists());
    assert!(fs::read_dir(&paths.cache).unwrap().next().is_none());
    let stale = log.parent().unwrap().join("session-stale.log");
    fs::write(&stale, "old").unwrap();
    old(&stale);
    let explicit = log.parent().unwrap().join("custom.log");
    let files = RuntimeFiles::open(&paths, Some(&explicit), Mode::Human).unwrap();
    assert!(stale.exists());
    files.close().unwrap();
    let guard = lock_file(
        &log.parent().unwrap().join("session-stale.log.lock"),
        true,
        true,
    )
    .unwrap();
    prune(&paths.cache, log.parent().unwrap(), true, SystemTime::now()).unwrap();
    assert!(stale.exists());
    drop(guard);
    prune(&paths.cache, log.parent().unwrap(), true, SystemTime::now()).unwrap();
    assert!(!stale.exists());
}
#[test]
fn lock_symlinks_and_blocked_log_paths_fail_without_leaking_cache() {
    let dir = fixture();
    let paths = Locations {
        cache: dir.path().join("cache"),
        state: dir.path().join("state"),
    };
    fs::create_dir_all(&paths.cache).unwrap();
    fs::write(dir.path().join("victim"), "untouched").unwrap();
    std::os::unix::fs::symlink(dir.path().join("victim"), dir.path().join("cache.lock")).unwrap();
    assert!(RuntimeFiles::open(&paths, None, Mode::Agent).is_err());
    assert_eq!(
        fs::read_to_string(dir.path().join("victim")).unwrap(),
        "untouched"
    );
    fs::remove_file(dir.path().join("cache.lock")).unwrap();
    fs::write(&paths.state, "blocked").unwrap();
    assert!(RuntimeFiles::open(&paths, None, Mode::Agent).is_err());
    assert_eq!(fs::read_dir(&paths.cache).unwrap().count(), 0);
}

#[test]
fn native_rotation_keeps_selected_name_bounds_archives_and_expires_old_gzip() {
    use std::io::Write;
    let dir = fixture();
    let path = dir.path().join("selected.log");
    let mut writer = agent_wrap::files::Rotating::open(&path, 8).unwrap();
    for index in 0..9 {
        writeln!(writer, "record-{index}-payload").unwrap();
    }
    writer.write_all(b"tail").unwrap();
    writer.flush().unwrap();
    assert!(path.exists());
    assert_eq!(fs::read_to_string(&path).unwrap(), "tail");
    for index in 0..5 {
        let archive = dir.path().join(format!("selected.{index}.log.gz"));
        assert_eq!(&fs::read(&archive).unwrap()[..2], &[0x1f, 0x8b]);
    }
    assert_eq!(fs::read_dir(dir.path()).unwrap().count(), 6);
    let expired = dir.path().join("selected.4.log.gz");
    old(&expired);
    writer.write_all(b"x").unwrap();
    assert!(!expired.exists());
    assert!(writer.write(&[0xff]).is_err());
    drop(writer);
    fs::create_dir(dir.path().join("blocked.log")).unwrap();
    assert!(agent_wrap::files::Rotating::open(&dir.path().join("blocked.log"), 8).is_err());
    assert!(agent_wrap::files::Rotating::open(Path::new("/"), 8).is_err());
}

#[test]
fn retention_preserves_fresh_unrelated_symlink_and_cross_cache_owned_logs() {
    let dir = fixture();
    let locations = Locations {
        cache: dir.path().join("cache"),
        state: dir.path().join("logs"),
    };
    let files = RuntimeFiles::open(&locations, None, Mode::Agent).unwrap();
    let fresh = locations.cache.join("session-fresh");
    fs::create_dir(&fresh).unwrap();
    let unrelated = locations.state.join("notes.txt");
    fs::write(&unrelated, "keep").unwrap();
    old(&unrelated);
    std::os::unix::fs::symlink(&fresh, locations.cache.join("session-link")).unwrap();
    let locked_log = files.log_path.clone();
    old(&locked_log);
    let different = Locations {
        cache: dir.path().join("another-cache"),
        state: locations.state.clone(),
    };
    let second = RuntimeFiles::open(&different, None, Mode::Human).unwrap();
    assert!(locked_log.exists());
    assert!(fresh.exists());
    assert!(unrelated.exists());
    assert!(locations.cache.join("session-link").exists());
    assert!(RuntimeFiles::open(&different, Some(&locked_log), Mode::Agent).is_err());
    second.close().unwrap();
    files.close().unwrap();
}

#[test]
fn rotation_treats_environment_tokens_braces_and_non_utf8_as_literal_paths() {
    use std::{ffi::OsString, io::Write, os::unix::ffi::OsStringExt};
    let tmp = fixture();
    let directory = tmp.path().join("$ENV{HOME}-{}");
    fs::create_dir(&directory).unwrap();
    let path = directory.join(OsString::from_vec(b"literal-{}-\xff.log".to_vec()));
    let mut writer = agent_wrap::files::Rotating::open(&path, 8).unwrap();
    writer.write_all(b"first-record").unwrap();
    writer.write_all(b"tail").unwrap();
    writer.flush().unwrap();
    assert_eq!(fs::read(&path).unwrap(), b"tail");
    let archive = directory.join(OsString::from_vec(b"literal-{}-\xff.0.log.gz".to_vec()));
    assert_eq!(&fs::read(archive).unwrap()[..2], &[0x1f, 0x8b]);
    assert_eq!(fs::read_dir(tmp.path()).unwrap().count(), 1);
}
