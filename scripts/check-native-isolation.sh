#!/usr/bin/env bash
set -euo pipefail
project=$(cd "$(dirname "$0")/.." && pwd -P)
cd "$project"
# The foreground supervisor owns native scratch and joins its process group.
# The build wrapper removes this temporary executable only after it exits.
exec bash scripts/with-build-scratch bash -c '
    set -euo pipefail
    go build -o "$TMPDIR/native-isolation" ./scripts/native-isolation
    exec "$TMPDIR/native-isolation" "$1"
' native-isolation "$project"
