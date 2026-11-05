#!/usr/bin/env bash
set -euo pipefail

project=$(cd "$(dirname "$0")/.." && pwd -P)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/agent-wrap-main-root.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null

init_repo() {
    git init --quiet "$1"
    git -C "$1" -c user.name=Test -c user.email=test@example.invalid commit --quiet --allow-empty -m initial
}

assert_root() {
    local actual
    actual=$(cd "$scratch" && just --justfile "$1/justfile" --evaluate main_root)
    if test "$actual" != "$2"; then
        printf 'Main checkout for %s: got %s, want %s\n' "$1" "$actual" "$2" >&2
        exit 1
    fi
}

main="$scratch/ordinary: checkout"
init_repo "$main"
cp "$project/justfile" "$main/justfile"
cp -R "$project/scripts" "$main/scripts"
git -C "$main" add justfile scripts
git -C "$main" -c user.name=Test -c user.email=test@example.invalid commit --quiet -m recipes
git -C "$main" worktree add --quiet --detach "$scratch/ordinary linked"
assert_root "$main" "$main"
assert_root "$scratch/ordinary linked" "$main"

super="$scratch/superproject"
init_repo "$super"
git -C "$super" -c protocol.file.allow=always submodule add --quiet "$main" 'absorbed module'
module="$super/absorbed module"
test -f "$module/.git"
git -C "$module" worktree add --quiet --detach "$scratch/submodule linked"
assert_root "$module" "$module"
assert_root "$scratch/submodule linked" "$module"

# Reject a misconfigured main checkout instead of using another repository's cache.
git -C "$module" config core.worktree "$main"
if just --justfile "$scratch/submodule linked/justfile" --evaluate main_root >"$scratch/invalid.log" 2>&1; then
    printf 'Accepted a main checkout from another repository.\n' >&2
    exit 1
fi
git --git-dir="$super/.git/modules/absorbed module" config core.worktree '../../../absorbed module'

# Inherited repository selection must not redirect a recipe to another checkout.
export GIT_DIR="$main/.git" GIT_WORK_TREE="$main" GIT_COMMON_DIR="$main/.git"
assert_root "$scratch/submodule linked" "$module"
printf 'Main checkout discovery passed for ordinary, absorbed, and linked checkouts.\n'
