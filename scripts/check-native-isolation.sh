#!/usr/bin/env bash
set -euo pipefail
project=$(cd "$(dirname "$0")/.." && pwd -P)
if ! test -x "$project/bin/native-isolation"; then
    printf 'Run just setup or just build before the native isolation diagnostic.\n' >&2
    exit 1
fi
exec "$project/bin/native-isolation" "$project"
