#!/usr/bin/env bash
set -euo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR
common=$(git -C "$1" rev-parse --path-format=absolute --git-common-dir)
# Ordinary repositories use the .git parent; absorbed submodules override it
# through core.worktree, resolved by Git relative to the common directory.
root=$(git -C "$common/.." --git-dir="$common" rev-parse --show-toplevel)
resolved=$(git -C "$root" rev-parse --path-format=absolute --git-common-dir)
if test "$resolved" != "$common"; then
    printf 'Main checkout does not belong to repository: %s\n' "$root" >&2
    exit 1
fi
printf '%s\n' "$root"
