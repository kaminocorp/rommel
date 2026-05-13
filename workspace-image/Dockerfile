# syntax=docker/dockerfile:1.7
#
# Rommel workspace image. Becomes the Fly Machine VM that hosts a single
# workspace; bakes the sandbox-daemon binary plus a minimal Linux toolchain
# (git, curl, ca-certificates, tini) and the EdDSA pubkey that gates session
# tokens.
#
# Build context MUST be the repo root — the Dockerfile copies sibling
# subtrees (proto/, sandbox-daemon/). Use:
#
#     docker build -f workspace-image/Dockerfile -t rommel-workspaces:<sha> .
#
# scripts/build.sh enforces the invariant.

# ---------- build stage: produce static sandbox-daemon binary --------------
#
# Builder is golang:1.23 (one minor ahead of the daemon's go.mod). Required
# because `go run github.com/atombender/go-jsonschema@v0.18.0` invoked by
# proto/codegen/go.sh requires Go >= 1.23. The compiled daemon binary is
# the only artifact promoted to the runtime stage, so the builder's Go
# version is independent of the daemon's go.mod (which declares `go 1.22`
# as the minimum — a 1.23 toolchain compiles it fine).

FROM golang:1.23 AS build
WORKDIR /src

# proto Go client is gitignored; regenerate from schemas inside the build so
# the image never depends on an out-of-tree generated artifact.
COPY proto ./proto
RUN bash ./proto/codegen/go.sh

COPY sandbox-daemon ./sandbox-daemon
WORKDIR /src/sandbox-daemon
RUN go mod download
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
      -o /out/sandbox-daemon ./cmd/daemon

# ---------- runtime stage: workspace VM image -------------------------------

FROM debian:stable-slim

# Baseline tools. Reproducibility comes from the base-image digest; we keep
# the apt packages versionless on stable-slim per Decision 0.7.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      git \
      tini \
 && rm -rf /var/lib/apt/lists/*

# Daemon binary
COPY --from=build /out/sandbox-daemon /usr/local/bin/sandbox-daemon

# Bake rootfs (env example, pubkey placeholder)
COPY workspace-image/rootfs/ /

# EdDSA pubkey. Build-arg controls source path; defaults to the in-tree
# placeholder PEM (dev only — production builds pass a real key path).
ARG ROMMEL_TOKEN_PUBKEY_FILE=workspace-image/rootfs/etc/rommel/token.pubkey.example
COPY ${ROMMEL_TOKEN_PUBKEY_FILE} /etc/rommel/token.pubkey

# Workspace mount point. The backend attaches a per-workspace Fly volume here
# at machine create time; the bare directory exists in-image so the daemon's
# config.Load is satisfied even on `docker run` without any mount.
ENV ROMMEL_WORKSPACE_ROOT=/workspace
WORKDIR /workspace

# Entrypoint loads /etc/rommel/token.pubkey into ROMMEL_TOKEN_PUBKEY,
# fail-fasts on missing per-machine env, then exec's the daemon under tini.
COPY workspace-image/scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/usr/bin/tini", "--", "/entrypoint.sh"]
