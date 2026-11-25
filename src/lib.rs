pub mod activity;
pub mod cli;
pub mod files;
pub mod header;
pub mod project;
pub mod runtime;
pub mod tracker;

#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("{0}")]
    Invalid(String),
    #[error(transparent)]
    Io(#[from] std::io::Error),
    #[error("git {args}: {diagnostic} (status {status})")]
    Git {
        args: String,
        diagnostic: String,
        status: i32,
    },
    #[error("invalid Git remote identity: {0}")]
    Remote(#[from] url::ParseError),
    #[error(transparent)]
    Frame(#[from] tui_frame::Error),
    #[error("{0}")]
    Multiple(String),
    #[error(transparent)]
    Arguments(#[from] clap::Error),
}
pub type Result<T> = std::result::Result<T, Error>;
