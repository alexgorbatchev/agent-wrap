#!/usr/bin/env bash

# Explicit repository selection disables upward discovery for extracted archives,
# including paths containing Git ceiling-list separators such as ':'.
with_archive_git() (
    unset GIT_WORK_TREE GIT_COMMON_DIR
    GIT_DIR="$PWD/.git" "$@"
)
