#!/usr/bin/env bash
# Regenerate the Python proto client (Pydantic v2 models) from proto/schemas/.
# Outputs to proto/clients/python/gen/ (gitignored). Idempotent.
#
# datamodel-code-generator lives in a self-contained venv at proto/codegen/.venv/.
# Created on first run, reused thereafter. No global Python install required.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROTO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SCHEMAS_DIR="$PROTO_DIR/schemas"
OUT_DIR="$PROTO_DIR/clients/python/gen"
VENV_DIR="$SCRIPT_DIR/.venv"

DATAMODEL_VERSION="0.31.2"

PY="${PYTHON:-python3}"

# Bootstrap a hermetic venv on first run. The marker file pins the installed
# version so version bumps trigger a clean reinstall.
MARKER="$VENV_DIR/.installed-$DATAMODEL_VERSION"
if [ ! -f "$MARKER" ]; then
  echo "python: bootstrapping codegen venv at $VENV_DIR (datamodel-code-generator $DATAMODEL_VERSION)"
  rm -rf "$VENV_DIR"
  "$PY" -m venv "$VENV_DIR"
  "$VENV_DIR/bin/pip" install --quiet --upgrade pip
  "$VENV_DIR/bin/pip" install --quiet "datamodel-code-generator==$DATAMODEL_VERSION"
  touch "$MARKER"
fi

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

"$VENV_DIR/bin/datamodel-codegen" \
  --input "$SCHEMAS_DIR" \
  --input-file-type jsonschema \
  --output "$OUT_DIR" \
  --output-model-type pydantic_v2.BaseModel \
  --disable-timestamp \
  --use-schema-description \
  --target-python-version 3.12 \
  --use-double-quotes

# Schema count for the log line.
schema_count="$(find "$SCHEMAS_DIR" -name '*.json' -type f | wc -l | tr -d ' ')"
echo "python: regenerated $schema_count schemas → $OUT_DIR"
