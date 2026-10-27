default:
    @just --list

build:
    cargo build --locked --release --bin agent-wrap
    mkdir -p bin
    cp target/release/agent-wrap bin/agent-wrap

run *args:
    cargo run --locked --bin agent-wrap -- {{args}}

run-ai *args:
    AGENT=1 cargo run --locked --bin agent-wrap -- {{args}}

test:
    cargo test --locked --all-targets

fmt:
    cargo fmt --package agent-wrap

lint:
    cargo fmt --package agent-wrap -- --check
    cargo clippy --locked --all-targets -- -D warnings

coverage:
    cargo llvm-cov --locked --all-targets --fail-under-lines 90

doc:
    RUSTDOCFLAGS='-D warnings' cargo doc --locked --no-deps
    cargo test --locked --doc

check: lint test coverage doc
