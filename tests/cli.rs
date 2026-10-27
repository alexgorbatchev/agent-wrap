use agent_wrap::cli;
use cli_command_kit::Mode;
#[test]
fn duration_values_preserve_go_units_limits_and_default_selection() {
    use std::time::Duration;
    for (value, expected) in [
        ("1µs", Duration::from_micros(1)),
        ("2μs", Duration::from_micros(2)),
        ("3us", Duration::from_micros(3)),
        ("1h2m3.5s", Duration::from_millis(3_723_500)),
        ("0", Duration::ZERO),
        ("+0", Duration::ZERO),
        ("-0", Duration::ZERO),
        ("-1s", Duration::ZERO),
        ("-9223372036854775808ns", Duration::ZERO),
        (
            "9223372036854775807ns",
            Duration::from_nanos(i64::MAX as u64),
        ),
        (
            "0.123456789123456789123456789s",
            Duration::from_nanos(123_456_789),
        ),
    ] {
        let matches = cli::command(Mode::Agent)
            .try_get_matches_from(["agent-wrap", "--scan-interval", value])
            .unwrap();
        assert_eq!(
            *matches.get_one::<Duration>("scan-interval").unwrap(),
            expected,
            "{value}"
        );
    }
    for value in [
        "bad",
        "",
        "1",
        "9223372036854775808ns",
        "-9223372036854775809ns",
        "1m-30s",
    ] {
        assert!(
            cli::command(Mode::Agent)
                .try_get_matches_from(["agent-wrap", "--scan-interval", value])
                .is_err(),
            "{value}"
        );
    }
}
#[test]
fn offline_metadata_help_validation_and_literal_child_arguments() {
    for mode in [Mode::Human, Mode::Agent] {
        let mut out = Vec::new();
        assert_eq!(
            cli::run(["agent-wrap", "skill"], mode, &mut out).unwrap(),
            0
        );
        assert_eq!(out, cli::SKILL.as_bytes());
        out.clear();
        cli::run(["agent-wrap", "--version"], mode, &mut out).unwrap();
        assert_eq!(out, b"0.1.0\n");
        for args in [
            vec!["agent-wrap"],
            vec!["agent-wrap", "--"],
            vec!["agent-wrap", "--name", "custom"],
            vec!["agent-wrap", "--version=false"],
            vec!["agent-wrap", "--help"],
            vec!["agent-wrap", "help", "skill"],
            vec!["agent-wrap", "completion", "--help"],
        ] {
            out.clear();
            cli::run(args, mode, &mut out).unwrap();
            let text = String::from_utf8_lossy(&out);
            if mode == Mode::Agent {
                assert!(text.starts_with("ALERT:"), "{text}");
            } else {
                assert!(!text.starts_with("ALERT:"));
            }
        }
    }
    for args in [
        vec!["agent-wrap", "pi"],
        vec!["agent-wrap", "--scan-interval", "bad", "--", "pi"],
        vec!["agent-wrap", "skill", "extra"],
    ] {
        assert!(
            cli::run(args.clone(), Mode::Agent, &mut Vec::new()).is_err(),
            "{args:?}"
        );
    }
    let matches = cli::command(Mode::Agent)
        .try_get_matches_from([
            "agent-wrap",
            "--harness",
            "pi",
            "--",
            "node",
            "--flag",
            "a b",
            "--",
            "$(touch unsafe)",
        ])
        .unwrap();
    assert_eq!(
        matches
            .get_many::<std::ffi::OsString>("agent")
            .unwrap()
            .cloned()
            .collect::<Vec<_>>(),
        ["node", "--flag", "a b", "--", "$(touch unsafe)"].map(std::ffi::OsString::from)
    );
    for shell in ["bash", "zsh", "fish", "powershell"] {
        let mut with_descriptions = Vec::new();
        let mut explicitly_enabled = Vec::new();
        cli::run(
            ["agent-wrap", "completion", shell],
            Mode::Agent,
            &mut with_descriptions,
        )
        .unwrap();
        cli::run(
            ["agent-wrap", "completion", shell, "--no-descriptions=false"],
            Mode::Agent,
            &mut explicitly_enabled,
        )
        .unwrap();
        assert_eq!(with_descriptions, explicitly_enabled);
        for stripped in [false, true] {
            let mut args = vec!["agent-wrap", "completion", shell];
            if stripped {
                args.push("--no-descriptions");
            }
            let mut out = Vec::new();
            cli::run(args, Mode::Agent, &mut out).unwrap();
            assert!(out.len() > 100);
        }
    }
    struct Broken;
    impl std::io::Write for Broken {
        fn write(&mut self, _: &[u8]) -> std::io::Result<usize> {
            Err(std::io::ErrorKind::BrokenPipe.into())
        }
        fn flush(&mut self) -> std::io::Result<()> {
            Ok(())
        }
    }
    for args in [
        vec!["agent-wrap", "skill"],
        vec!["agent-wrap", "--help"],
        vec!["agent-wrap", "--version"],
        vec!["agent-wrap", "completion", "fish"],
    ] {
        assert!(cli::run(args, Mode::Agent, &mut Broken).is_err());
    }
}

#[test]
fn binary_reports_one_error_on_stderr_without_usage_for_runtime_failures() {
    for mode in ["1", "0"] {
        let output = std::process::Command::new(env!("CARGO_BIN_EXE_agent-wrap"))
            .args(["--harness", "pi", "--dir", "/dev", "--", "pi"])
            .env("AGENT", mode)
            .output()
            .unwrap();
        assert_eq!(output.status.code(), Some(1));
        assert!(output.stdout.is_empty());
        let text = String::from_utf8(output.stderr).unwrap();
        assert_eq!(text.lines().count(), 1);
        assert!(text.starts_with(if mode == "1" { "ERR: " } else { "[ERROR] " }));
        assert!(text.contains("interactive terminal"));
    }
    let mut output = Vec::new();
    let error = agent_wrap::Error::Invalid("failure".into());
    cli::report(&error, Mode::Agent, &mut output).unwrap();
    assert_eq!(output, b"ERR: failure\n");
}

#[test]
fn embedded_skill_tracks_native_command_tree_and_raw_unix_arguments() {
    use std::{ffi::OsString, os::unix::ffi::OsStringExt};
    let root = cli::command(Mode::Agent);
    let omissions = cli_command_kit::skill_omissions(&root, &cli::help_options(), cli::SKILL);
    assert!(omissions.is_empty(), "{omissions:?}");
    let raw = OsString::from_vec(vec![b'a', 0xff, b'b']);
    let matches = root
        .try_get_matches_from([
            OsString::from("agent-wrap"),
            OsString::from("--name"),
            OsString::from("custom"),
            OsString::from("--"),
            OsString::from("pi"),
            raw.clone(),
        ])
        .unwrap();
    assert_eq!(matches.get_one::<String>("name").unwrap(), "custom");
    assert_eq!(
        matches
            .get_many::<OsString>("agent")
            .unwrap()
            .next_back()
            .unwrap(),
        &raw
    );
    assert!(
        cli::command(Mode::Agent)
            .try_get_matches_from(["agent-wrap", "pi"])
            .is_err()
    );
    for path in [
        vec!["completion", "bash"],
        vec!["completion", "zsh"],
        vec!["completion", "fish"],
        vec!["completion", "powershell"],
        vec!["skill"],
    ] {
        let mut args = vec!["agent-wrap"];
        args.extend(path);
        args.push("--help");
        let mut output = Vec::new();
        cli::run(args, Mode::Agent, &mut output).unwrap();
        assert!(output.starts_with(b"ALERT:"));
    }
}
