set positional-arguments
set dotenv-load := false
set tempdir := '.tmp'

main_root := shell('bash "$1/scripts/main-root.sh" "$1"', module_directory())
native_root := main_root / '.cache/native'
host_arch := if arch() == 'x86_64' { 'amd64' } else if arch() == 'aarch64' { 'arm64' } else { error('Supported architectures: amd64 and arm64') }
host_target := if host_arch == 'amd64' { 'x86_64-linux-musl' } else { 'aarch64-linux-musl' }
native_target := if os() == 'linux' { host_target } else if os() == 'macos' { 'native' } else { error('Supported systems: Linux and macOS') }
native_prefix := native_root / if os() == 'linux' { 'linux-musl' / host_arch / 'prefix' } else { 'prefix' }
export CGO_ENABLED := '1'
export CC := if os() == 'linux' { 'zig cc -target ' + host_target } else { env('CC', 'cc') }
export PKG_CONFIG_PATH := native_prefix / 'share/pkgconfig'

default:
    @just --list

# Build the pinned native terminal archive with Zig 0.16.0.
native:
    bash scripts/with-build-scratch bash scripts/native.sh {{ quote(native_root) }} {{ quote(native_target) }} {{ quote(native_prefix) }}

# Preserve individual arguments, including spaces and flags after --.
run *args: build
    bin/agent-wrap "$@"

run-ai *args: build
    AGENT=1 bin/agent-wrap "$@"

build: native build-tools
    mkdir -p bin
    bash scripts/with-build-scratch go build {{ if os() == 'linux' { '-ldflags="-linkmode=external -extldflags=-static"' } else { '' } }} -o bin/agent-wrap ./cmd/agent-wrap

# Build the supervisor before running the native isolation diagnostic.
[private]
build-tools:
    mkdir -p bin
    bash scripts/with-build-scratch go build -o bin/native-isolation ./scripts/native-isolation

test: native
    bash scripts/check-main-root.sh
    bash scripts/check-coverage.sh -race ./...

lint: native
    bash scripts/with-build-scratch go mod tidy -diff
    bash scripts/with-build-scratch go vet ./...
    bash scripts/with-build-scratch golangci-lint run

vet: lint

check:
    just test
    just lint
    just build

fmt:
    bash scripts/with-build-scratch go fmt ./...

# Prepare dependencies and generated assets for local development
setup:
    bash scripts/with-build-scratch go mod download
    just native
    just build-tools
