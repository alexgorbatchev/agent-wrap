#!/usr/bin/env bash
set -euo pipefail

# These pins are the build contract of the go-tui-frame revision in go.mod.
revision='33da6848d63b3bba2b4f31ab1531d618f2795192'
archive_sha='bbda18d6f6666ff05dec33974c134483b096fe321ff68a22eba9399b70354de6'
root="$1"
target="$2"
prefix="$3"
source="$root/ghostty"
test "$(zig version)" = '0.16.0' || { printf 'Zig 0.16.0 is required.\n' >&2; exit 1; }
command -v pkg-config >/dev/null
mkdir -p "$root"
if test -e "$source"; then
    test -f "$source/.frame-source-revision" && test "$(cat "$source/.frame-source-revision")" = "$revision" || { printf 'Native source revision mismatch: %s\n' "$source" >&2; exit 1; }
else
    archive="$root/ghostty-$revision.tar.gz"
    if ! test -f "$archive"; then
        curl --fail --location --retry 3 "https://codeload.github.com/ghostty-org/ghostty/tar.gz/$revision" --output "$archive"
    fi
    printf '%s  %s\n' "$archive_sha" "$archive" | shasum -a 256 --check
    extracted=$(mktemp -d "$root/source.XXXXXX")
    trap 'rm -rf "$extracted"' EXIT
    tar -xzf "$archive" --strip-components=1 -C "$extracted"
    printf '%s\n' "$revision" > "$extracted/.frame-source-revision"
    mv "$extracted" "$source"
    trap - EXIT
fi
cd "$source"
zig build -Demit-lib-vt -Demit-xcframework=false -Doptimize=ReleaseFast -Dtarget="$target" --prefix "$prefix" --cache-dir "$root/cache-$target" --global-cache-dir "$root/zig-global-cache"
PKG_CONFIG_PATH="$prefix/share/pkgconfig" pkg-config --static --libs --cflags libghostty-vt-static
