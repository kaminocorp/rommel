#!/usr/bin/env bash
# Regenerate the Go proto client from proto/schemas/.
# Outputs a single proto/clients/go/gen/proto.go (package: protogen). Gitignored. Idempotent.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROTO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SCHEMAS_DIR="$PROTO_DIR/schemas"
OUT_DIR="$PROTO_DIR/clients/go/gen"
OUT_FILE="$OUT_DIR/proto.go"

# atombender/go-jsonschema pinned. `go run` caches the build; second invocation is fast.
GOJSONSCHEMA="github.com/atombender/go-jsonschema@v0.18.0"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

# Collect all schema files, sorted for determinism (portable across Bash 3.2+/4+ and macOS).
schemas=()
while IFS= read -r -d '' f; do
  schemas+=("$f")
done < <(find "$SCHEMAS_DIR" -name "*.json" -type f -print0 | LC_ALL=C sort -z)

if [ "${#schemas[@]}" -eq 0 ]; then
  echo "go: no schemas found under $SCHEMAS_DIR" >&2
  exit 1
fi

# -t  → use schema 'title' as the generated struct name (so FsRead becomes type FsRead, not type Read)
# -p  → package name
# --capitalization keeps initialisms like ID/URL Go-idiomatic
go run "$GOJSONSCHEMA" \
  -p protogen \
  -t \
  --capitalization ID \
  --capitalization URL \
  --capitalization JTI \
  -o "$OUT_FILE" \
  "${schemas[@]}"

echo "go: regenerated ${#schemas[@]} schemas → $OUT_FILE"
