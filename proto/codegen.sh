#!/usr/bin/env bash
# Regenerate all proto clients (TS, Go, Python) from proto/schemas/.
# Idempotent. Equivalent to `make proto` from the repo root.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

"$SCRIPT_DIR/codegen/ts.sh"
"$SCRIPT_DIR/codegen/go.sh"
"$SCRIPT_DIR/codegen/python.sh"

echo "proto: all clients regenerated."
