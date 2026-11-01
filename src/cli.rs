use crate::{Error, Result, runtime};
use clap::{Arg, ArgAction, Command};
use cli_command_kit::{HelpOptions, Mode};
use std::{
    ffi::OsString,
    io::{self, Write},
    os::fd::AsFd,
    path::PathBuf,
    time::Duration,
};
pub const SKILL: &str = include_str!("../skills/agent-wrap/SKILL.md");
pub fn help_options() -> HelpOptions {
    HelpOptions {
        skill: Some(
            cli_command_kit::SkillSource::new("alexgorbatchev/agent-wrap-rust", "main")
                .expect("the fixed skill source names a GitHub repository"),
        ),
        ..Default::default()
    }
}
fn duration(value: &str) -> std::result::Result<Duration, String> {
    if matches!(value, "0" | "+0" | "-0") {
        return Ok(Duration::ZERO);
    }
    value
        .parse::<go_duration::GoDuration>()
        .map(|duration| Duration::from_nanos(u64::try_from(duration.nanoseconds()).unwrap_or(0)))
        .map_err(|error| error.to_string())
}
pub fn command(mode: Mode) -> Command {
    let mut root = Command::new("agent-wrap")
        .about("Identify agent projects, branches, and subtrees in a live terminal header")
        .version(env!("CARGO_PKG_VERSION"))
        .disable_version_flag(true)
        .subcommand_negates_reqs(true)
        .args_conflicts_with_subcommands(true)
        .arg(
            Arg::new("version")
                .long("version")
                .short('v')
                .action(ArgAction::Set)
                .num_args(0..=1)
                .require_equals(true)
                .default_missing_value("true")
                .default_value("false")
                .value_parser(clap::value_parser!(bool))
                .help("Print raw version"),
        )
        .arg(
            Arg::new("agent")
                .value_name("agent")
                .num_args(1..)
                .last(true)
                .value_parser(clap::value_parser!(OsString))
                .help("Agent executable and unchanged arguments after --"),
        );
    for (name, help) in [
        (
            "harness",
            "Agent family for an alias: claude-code, pi, codex, or opencode",
        ),
        ("dir", "Launch directory (default: current directory)"),
        (
            "default-branch",
            "Default branch when local origin/HEAD is unavailable",
        ),
        ("name", "Agent label (default: detected agent name)"),
        (
            "log-file",
            "Wrapper log file (default: a unique file under XDG state)",
        ),
    ] {
        root = root.arg(Arg::new(name).long(name).help(help).default_value(""));
    }
    root = root.arg(
        Arg::new("scan-interval")
            .long("scan-interval")
            .default_value("2s")
            .allow_hyphen_values(true)
            .value_parser(duration)
            .help("Interval between agent discovery scans; nonpositive selects 2s"),
    );
    root = root.subcommand(
        Command::new("completion")
            .about("Generate shell completion scripts")
            .subcommands(["bash", "zsh", "fish", "powershell"].map(|name| {
                Command::new(name)
                    .about(format!("Generate {name} completion"))
                    .arg(
                        Arg::new("no-descriptions")
                            .long("no-descriptions")
                            .action(ArgAction::Set)
                            .num_args(0..=1)
                            .require_equals(true)
                            .default_missing_value("true")
                            .default_value("false")
                            .value_parser(clap::value_parser!(bool))
                            .help("Omit descriptions"),
                    )
            })),
    );
    cli_command_kit::setup(root, mode, &help_options()).expect("the static command tree is valid")
}
fn strip_descriptions(command: Command) -> Command {
    command
        .mut_args(|arg| arg.help(None::<&str>).long_help(None::<&str>))
        .mut_subcommands(|child| {
            strip_descriptions(child.about(None::<&str>).long_about(None::<&str>))
        })
}
pub fn run(
    args: impl IntoIterator<Item = impl Into<OsString> + Clone>,
    mode: Mode,
    output: &mut dyn Write,
) -> Result<u32> {
    let mut root = command(mode);
    let args = args.into_iter().map(Into::into).collect::<Vec<OsString>>();
    let matches = match root.clone().try_get_matches_from(args) {
        Ok(matches) => matches,
        Err(error) if error.kind() == clap::error::ErrorKind::DisplayHelp => {
            write!(output, "{error}")?;
            return Ok(0);
        }
        Err(error) => return Err(error.into()),
    };
    if matches.get_flag("version") {
        writeln!(output, "{}", env!("CARGO_PKG_VERSION"))?;
        return Ok(0);
    }
    if matches.subcommand_name() == Some("skill") {
        output.write_all(SKILL.as_bytes())?;
        return Ok(0);
    }
    if let Some(("completion", matches)) = matches.subcommand() {
        let Some((shell, matches)) = matches.subcommand() else {
            write!(
                output,
                "{}",
                root.find_subcommand_mut("completion")
                    .expect("completion exists")
                    .render_help()
            )?;
            return Ok(0);
        };
        let shell = match shell {
            "bash" => clap_complete::Shell::Bash,
            "zsh" => clap_complete::Shell::Zsh,
            "fish" => clap_complete::Shell::Fish,
            _ => clap_complete::Shell::PowerShell,
        };
        if matches.get_flag("no-descriptions") {
            root = strip_descriptions(root);
        }
        let mut bytes = Vec::new();
        clap_complete::generate(shell, &mut root, "agent-wrap", &mut bytes);
        output.write_all(&bytes)?;
        return Ok(0);
    }
    let options = runtime::Options {
        harness: matches
            .get_one::<String>("harness")
            .expect("default")
            .clone(),
        directory: PathBuf::from(matches.get_one::<String>("dir").expect("default")),
        default_branch: matches
            .get_one::<String>("default-branch")
            .expect("default")
            .clone(),
        name: matches.get_one::<String>("name").expect("default").clone(),
        log_file: matches
            .get_one::<String>("log-file")
            .filter(|s| !s.is_empty())
            .map(PathBuf::from),
        scan_interval: *matches
            .get_one::<Duration>("scan-interval")
            .expect("default"),
        mode,
        ..Default::default()
    };
    let Some(args) = matches.get_many::<OsString>("agent") else {
        write!(output, "{}", root.render_help())?;
        return Ok(0);
    };
    let args = args.cloned().collect();
    let input = io::stdin();
    let terminal = io::stdout();
    runtime::run(
        args,
        options,
        input.as_fd(),
        terminal.as_fd(),
        &tui_frame::Cancellation::new(),
    )
}
pub fn report(error: &Error, mode: Mode, output: &mut dyn Write) -> io::Result<()> {
    writeln!(
        output,
        "{}{}",
        if mode == Mode::Agent {
            "ERR: "
        } else {
            "[ERROR] "
        },
        cli_command_kit::display_message(&error.to_string()).trim_end()
    )
}
