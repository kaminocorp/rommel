#!/usr/bin/env bash
# Workspace VM entrypoint. tini is PID 1; this script wires env and exec's
# the daemon. Two responsibilities:
#
#   1. Load the EdDSA pubkey from /etc/rommel/token.pubkey into
#      ROMMEL_TOKEN_PUBKEY (the daemon's config.FromEnv parses the PEM
#      contents directly, not a file path).
#   2. Fail loudly if ROMMEL_WID is absent. The backend sets it at machine
#      create time; a missing value almost always means the Machines API call
#      shape is wrong, and surfacing that here is much louder than letting
#      the daemon's own validator collapse the error into a multi-issue list.
#
# ROMMEL_WORKSPACE_ROOT defaults to /workspace via Dockerfile ENV. The
# backend can override it at machine create time if a different mount path
# is ever needed.

set -euo pipefail

if [ -z "${ROMMEL_TOKEN_PUBKEY:-}" ]; then
  if [ -r /etc/rommel/token.pubkey ]; then
    ROMMEL_TOKEN_PUBKEY="$(cat /etc/rommel/token.pubkey)"
    export ROMMEL_TOKEN_PUBKEY
  else
    echo "entrypoint: /etc/rommel/token.pubkey not present and ROMMEL_TOKEN_PUBKEY not set" >&2
    exit 1
  fi
fi

if [ -z "${ROMMEL_WID:-}" ]; then
  echo "entrypoint: ROMMEL_WID is required (set by backend at machine create time)" >&2
  exit 1
fi

exec /usr/local/bin/sandbox-daemon
