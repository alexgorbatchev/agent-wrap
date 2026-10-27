fn main() {
    let mode = cli_command_kit::Mode::from_env();
    let code = match agent_wrap::cli::run(std::env::args_os(), mode, &mut std::io::stdout().lock())
    {
        Ok(code) => code,
        Err(error) => {
            let _ = agent_wrap::cli::report(&error, mode, &mut std::io::stderr().lock());
            1
        }
    };
    std::process::exit(i32::try_from(code).unwrap_or(1));
}
