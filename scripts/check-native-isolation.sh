#!/usr/bin/env bash
set -euo pipefail

# Exercise the real recipe in a separate repository, with independent native assets.
project=$(cd "$(dirname "$0")/.." && pwd -P)
mkdir -p "$project/.tmp"
scratch=$(mktemp -d "$project/.tmp/native-isolation.XXXXXX")
# A colon is legal in the source path, but is a separator in Git ceiling lists.
git clone --quiet --no-tags --no-hardlinks --local "$project" "$scratch/checkout:colon"
cp "$project/scripts/native.sh" "$scratch/checkout:colon/scripts/native.sh"
cd "$scratch/checkout:colon"
prefix="$scratch/prefix"
git tag v0.0.0-native-isolation
just --set native_prefix "$prefix" native > "$scratch/tagged.log" 2>&1 || { cat "$scratch/tagged.log" >&2; exit 1; }
PKG_CONFIG_PATH="$prefix/share/pkgconfig" pkg-config --static --libs --cflags libghostty-vt-static > "$scratch/tagged.pkg-config"
PKG_CONFIG_PATH="$prefix/share/pkgconfig" pkg-config --modversion libghostty-vt-static > "$scratch/tagged.version"
native_root=$(just --evaluate native_root)
target=$(just --evaluate native_target)
GIT_DIR="$PWD/.git" GIT_WORK_TREE="$PWD" GIT_COMMON_DIR="$PWD/.git" GIT_CEILING_DIRECTORIES=/ \
    bash scripts/native.sh "$native_root" "$target" "$prefix" > "$scratch/inherited.log" 2>&1 || { cat "$scratch/inherited.log" >&2; exit 1; }
git worktree add --quiet --detach "$scratch/worktree" HEAD
cp "$project/scripts/native.sh" "$scratch/worktree/scripts/native.sh"
(cd "$scratch/worktree" && just --set native_prefix "$prefix" native) > "$scratch/worktree.log" 2>&1 || { cat "$scratch/worktree.log" >&2; exit 1; }
test "$(cd "$scratch/worktree" && just --evaluate native_root)" = "$native_root"
git tag --delete v0.0.0-native-isolation > /dev/null
just --set native_prefix "$prefix" native > "$scratch/untagged.log" 2>&1 || { cat "$scratch/untagged.log" >&2; exit 1; }
PKG_CONFIG_PATH="$prefix/share/pkgconfig" pkg-config --static --libs --cflags libghostty-vt-static > "$scratch/untagged.pkg-config"
PKG_CONFIG_PATH="$prefix/share/pkgconfig" pkg-config --modversion libghostty-vt-static > "$scratch/untagged.version"
cmp "$scratch/tagged.pkg-config" "$scratch/untagged.pkg-config"
cmp "$scratch/tagged.version" "$scratch/untagged.version"
printf 'Tagged and untagged native builds match: %s\n' "$scratch"
