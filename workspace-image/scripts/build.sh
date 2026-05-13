#!/usr/bin/env bash
# Build the workspace image with repo-root as the docker build context.
# Always runs from the repo root regardless of caller CWD; lets `make -C
# workspace-image build` and CI's direct invocation share one code path.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

SHA="$(git rev-parse --short HEAD)"
IMAGE="${IMAGE:-rommel-workspaces}"
TAG="${TAG:-$SHA}"

# Optional: caller can point ROMMEL_TOKEN_PUBKEY_FILE at a real EdDSA pubkey
# PEM (e.g. exported from the backend's Fly secret). Defaults to the in-tree
# placeholder so local builds never silently embed a stale key from disk.
PUBKEY_FILE="${ROMMEL_TOKEN_PUBKEY_FILE:-workspace-image/rootfs/etc/rommel/token.pubkey.example}"

if [ ! -r "$PUBKEY_FILE" ]; then
  echo "build: ROMMEL_TOKEN_PUBKEY_FILE not readable: $PUBKEY_FILE" >&2
  exit 1
fi

echo ">>> docker build $IMAGE:$TAG  (pubkey: $PUBKEY_FILE)"
docker build \
  -f workspace-image/Dockerfile \
  --build-arg "ROMMEL_TOKEN_PUBKEY_FILE=$PUBKEY_FILE" \
  -t "$IMAGE:$TAG" \
  .

# Tag :latest only when the caller asks (CI sets TAG_LATEST=true on main).
if [ "${TAG_LATEST:-false}" = "true" ]; then
  docker tag "$IMAGE:$TAG" "$IMAGE:latest"
  echo ">>> tagged $IMAGE:latest"
fi
