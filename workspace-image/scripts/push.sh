#!/usr/bin/env bash
# Push the locally-tagged workspace image to Fly's registry. Assumes the
# image was already built (typically by scripts/build.sh in the same CI job).
#
# Locally: needs `fly auth login` first. CI: needs FLY_API_TOKEN in env so
# `flyctl auth docker` can install a Docker credential helper for
# registry.fly.io.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

if ! command -v flyctl >/dev/null 2>&1; then
  echo "push: flyctl not found; install from https://fly.io/docs/flyctl/" >&2
  exit 1
fi

# `flyctl auth whoami` accepts either an interactive session (fly auth login)
# or the FLY_API_TOKEN env var. In CI we expect the env var; locally we
# expect the session. Either way, an unauthenticated flyctl fails fast here.
if ! flyctl auth whoami >/dev/null 2>&1; then
  echo "push: flyctl is not authenticated; run 'fly auth login' or set FLY_API_TOKEN" >&2
  exit 1
fi

SHA="$(git rev-parse --short HEAD)"
IMAGE="${IMAGE:-rommel-workspaces}"
TAG="${TAG:-$SHA}"
REGISTRY_IMAGE="registry.fly.io/$IMAGE:$TAG"

docker tag "$IMAGE:$TAG" "$REGISTRY_IMAGE"

# Installs a short-lived docker credential for registry.fly.io.
flyctl auth docker

docker push "$REGISTRY_IMAGE"
echo ">>> pushed $REGISTRY_IMAGE"

if [ "${TAG_LATEST:-false}" = "true" ]; then
  docker tag "$IMAGE:$TAG" "registry.fly.io/$IMAGE:latest"
  docker push "registry.fly.io/$IMAGE:latest"
  echo ">>> pushed registry.fly.io/$IMAGE:latest"
fi
