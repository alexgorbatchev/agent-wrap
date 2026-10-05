#!/usr/bin/env bash
set -euo pipefail

# Exercise the real recipe in a separate repository, with independent native assets.
project=$(cd "$(dirname "$0")/.." && pwd -P)
source "$project/scripts/native-git.sh"
mkdir -p "$project/.tmp"
scratch=$(mktemp -d "$project/.tmp/native-isolation.XXXXXX")
git clone --quiet --no-tags --no-hardlinks --local "$project" "$scratch/checkout"
cp "$project/scripts/native.sh" "$project/scripts/native-git.sh" "$scratch/checkout/scripts/"
cd "$scratch/checkout"
git tag v0.0.0-native-isolation
just native > "$scratch/tagged.log" 2>&1 || { cat "$scratch/tagged.log" >&2; exit 1; }
prefix=$(just --evaluate native_prefix)
PKG_CONFIG_PATH="$prefix/share/pkgconfig" pkg-config --static --libs --cflags libghostty-vt-static > "$scratch/tagged.pkg-config"
PKG_CONFIG_PATH="$prefix/share/pkgconfig" pkg-config --modversion libghostty-vt-static > "$scratch/tagged.version"
native_root=$(just --evaluate native_root)
target=$(just --evaluate native_target)
# Run actual Ghostty version/config detection from a colon-containing source.
# Compilation on these paths separately fails in the pinned Zig C toolchain.
colon_source="$PWD/.tmp/native:colon/ghostty"
mkdir -p "$(dirname "$colon_source")"
cp -R "$native_root/ghostty" "$colon_source"
(cd "$colon_source" && with_archive_git zig build --help -Demit-lib-vt -Demit-xcframework=false -Doptimize=ReleaseFast -Dtarget="$target" --prefix "$prefix" --cache-dir "$native_root/cache-$target" --global-cache-dir "$native_root/zig-global-cache") > "$scratch/colon-config.log" 2>&1 || { cat "$scratch/colon-config.log" >&2; exit 1; }
GIT_DIR="$PWD/.git" GIT_WORK_TREE="$PWD" GIT_COMMON_DIR="$PWD/.git" GIT_CEILING_DIRECTORIES=/ \
    bash scripts/native.sh "$native_root" "$target" "$prefix" > "$scratch/inherited.log" 2>&1 || { cat "$scratch/inherited.log" >&2; exit 1; }
git worktree add --quiet --detach "$scratch/worktree" HEAD
cp "$project/scripts/native.sh" "$project/scripts/native-git.sh" "$scratch/worktree/scripts/"
(cd "$scratch/worktree" && just native) > "$scratch/worktree.log" 2>&1 || { cat "$scratch/worktree.log" >&2; exit 1; }
test "$(cd "$scratch/worktree" && just --evaluate native_root)" = "$native_root"
git tag --delete v0.0.0-native-isolation > /dev/null
just native > "$scratch/untagged.log" 2>&1 || { cat "$scratch/untagged.log" >&2; exit 1; }
PKG_CONFIG_PATH="$prefix/share/pkgconfig" pkg-config --static --libs --cflags libghostty-vt-static > "$scratch/untagged.pkg-config"
PKG_CONFIG_PATH="$prefix/share/pkgconfig" pkg-config --modversion libghostty-vt-static > "$scratch/untagged.version"
cmp "$scratch/tagged.pkg-config" "$scratch/untagged.pkg-config"
cmp "$scratch/tagged.version" "$scratch/untagged.version"
printf 'Tagged and untagged native builds match: %s\n' "$scratch"
