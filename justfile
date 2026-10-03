set positional-arguments
set dotenv-load := false
set tempdir := '.tmp'

native_root := module_directory() / '.tmp/native'
host_arch := if arch() == 'x86_64' { 'amd64' } else if arch() == 'aarch64' { 'arm64' } else { error('Supported architectures: amd64 and arm64') }
host_target := if host_arch == 'amd64' { 'x86_64-linux-musl' } else { 'aarch64-linux-musl' }
native_target := if os() == 'linux' { host_target } else if os() == 'macos' { 'native' } else { error('Supported systems: Linux and macOS') }
native_prefix := native_root / if os() == 'linux' { 'linux-musl' / host_arch / 'prefix' } else { 'prefix' }
export CGO_ENABLED := '1'
export CC := if os() == 'linux' { 'zig cc -target ' + host_target } else { env('CC', 'cc') }
export PKG_CONFIG_PATH := native_prefix / 'share/pkgconfig'
export TMPDIR := module_directory() / '.tmp'
export GOCACHE := module_directory() / '.tmp/go-build-cache'

default:
    @just --list

# Build the pinned native terminal archive with Zig 0.16.0.
native:
    mkdir -p .tmp
    bash scripts/native.sh {{quote(native_root)}} {{quote(native_target)}} {{quote(native_prefix)}}

# Preserve individual arguments, including spaces and flags after --.
run *args: native
    go run ./cmd/agent-wrap "$@"

run-ai *args: native
    AGENT=1 go run ./cmd/agent-wrap "$@"

build: native
    mkdir -p bin
    go build {{if os() == 'linux' {'-ldflags="-linkmode=external -extldflags=-static"'} else {''}}} -o bin/agent-wrap ./cmd/agent-wrap

test: native
    bash scripts/check-coverage.sh -race ./...

lint: native
    go mod tidy -diff
    go vet ./...
    golangci-lint run

vet: lint

check:
    just test
    just lint
    just build

fmt:
    go fmt ./...
