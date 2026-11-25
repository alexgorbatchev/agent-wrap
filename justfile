set positional-arguments
set dotenv-load := false
set tempdir := '.tmp'

default:
    @just --list

build:
    bash scripts/with-build-scratch cargo build --locked --release --bin agent-wrap
    mkdir -p bin
    cp target/release/agent-wrap bin/agent-wrap

run *args: build
    bin/agent-wrap "$@"

run-ai *args: build
    AGENT=1 bin/agent-wrap "$@"

setup:
    bash scripts/with-build-scratch cargo fetch --locked
    bash scripts/with-build-scratch cargo build --locked --lib

test:
    cargo test --locked --all-targets

test-affected base='origin/main':
    #!/usr/bin/env bash
    set -euo pipefail
    inputs=(Cargo.toml Cargo.lock justfile src tests vendor scripts skills)
    merge_base=$(git merge-base {{ quote(base) }} HEAD)
    paths=$(git diff --name-only "$merge_base" -- "${inputs[@]}")
    if [[ -z "$paths" ]] && [[ -z "$(git ls-files --others --exclude-standard -- "${inputs[@]}")" ]]; then
        printf 'No agent-wrap test inputs changed.\n'
        exit 0
    fi
    printf 'Agent Wrap sources, fixtures or build inputs changed. Running the package tests.\n'
    just test

fmt:
    cargo fmt --package agent-wrap

lint:
    cargo fmt --package agent-wrap -- --check
    bash scripts/with-build-scratch cargo clippy --locked --all-targets -- -D warnings

coverage:
    cargo llvm-cov --locked --all-targets --fail-under-lines 90 --fail-under-functions 90

doc:
    RUSTDOCFLAGS='-D warnings' cargo doc --locked --no-deps
    cargo test --locked --doc

check: lint coverage doc
