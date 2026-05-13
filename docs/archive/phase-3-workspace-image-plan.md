# Phase 3 — `workspace-image/` Implementation Plan

Companion to [`scaffolding-plan.md`](./scaffolding-plan.md) §3. This document specializes that section into a step-by-step build order for the **`workspace-image/`** subtree: the Docker image that becomes a Rommel workspace VM on Fly Machines.

**Status going in:** Phases 0–2 are complete (see [`docs/changelog.md`](../changelog.md)). The daemon binary builds statically, validates EdDSA session tokens, and answers `system.ping` + `fs.read`. `sandbox-daemon/Dockerfile` already produces a `debian:stable-slim + tini + daemon` image as a developer convenience; Phase 3 promotes that shape to a production artifact under a dedicated subtree, layered with the runtime tools and Fly metadata each workspace needs.

**Definition of "done" for Phase 3:**

1. `make -C workspace-image build` produces an image tagged `rommel-workspaces:<git-sha>` from the repo root.
2. `make -C workspace-image push` pushes it to Fly's registry under `registry.fly.io/rommel-workspaces:<git-sha>` (gated behind `fly auth login`).
3. `fly machine run` of that image listens on `:7777` over Flycast (no public service) and answers `system.ping` end-to-end with a valid EdDSA token.
4. Cold start (from `fly machine start` of a stopped machine to first WS frame round-trip) is measured and recorded; target ~250ms–1s per [`techstack.md`](../techstack.md).
5. `.github/workflows/workspace-image.yml` builds the image on PR (no push) and builds-and-pushes on `main`, gated on `workspace-image/Dockerfile` existing.

---

## 0. Decisions to settle before any code lands

These are calls the plan makes by default. Each one is reversible but cheaper to confirm now than to refactor later.

### 0.1 Single Dockerfile in `workspace-image/`; remove `sandbox-daemon/Dockerfile`

The daemon's own Dockerfile and the workspace image's Dockerfile would be 90% the same multi-stage build (Go builder → debian-slim runtime). The plan's §3 sketch already builds the daemon from source inside the workspace-image Dockerfile. **Recommendation:** make `workspace-image/Dockerfile` the canonical production artifact. Remove `sandbox-daemon/Dockerfile`; replace its README's "Building the Docker image" section with a pointer to `workspace-image/`. Local development still uses `make run-local` from source, which never needed Docker.

Trade-off considered: keep `sandbox-daemon/Dockerfile` as a "binary only" image and `FROM` it from `workspace-image/Dockerfile`. Rejected — Docker has no clean cross-Dockerfile `FROM` mechanism without first pushing the intermediate image somewhere, which adds CI plumbing for no benefit at our scale.

### 0.2 Pubkey baking strategy: `ARG` + `COPY` into rootfs at build time

The scaffolding plan's cross-cutting section recommends baking the EdDSA public key into the image at build time (over runtime fetch) to avoid a backend dependency at daemon startup. Concretely:

- `Dockerfile` accepts `ARG ROMMEL_TOKEN_PUBKEY_FILE=workspace-image/rootfs/etc/rommel/token.pubkey.example`.
- The PEM is `COPY`'d to `/etc/rommel/token.pubkey` inside the image.
- `entrypoint.sh` reads the file and exports `ROMMEL_TOKEN_PUBKEY="$(cat /etc/rommel/token.pubkey)"` before `exec`'ing the daemon.

This keeps the *key material* image-versioned (rotation = rebuild + redeploy, which is what we want), while `ROMMEL_WID` and other per-machine values stay env-driven from the Machines API at `fly machine run` time.

Trade-off considered: Fly app secret (`fly secrets set ROMMEL_TOKEN_PUBKEY=…`) wiring through `fly.toml`'s `[env]` block. Cleaner for rotation but adds a startup dependency on Fly's secrets being injected before the daemon's `config.Load` runs, and dilutes the "baked at deploy" decision recorded in Phase 1. Documented as a reversible follow-up.

### 0.3 No public services; reach via Flycast on `:7777`

`fly.toml` will declare an internal port (`internal_port = 7777`) but **no `[[services]]` block** — i.e., no public HTTP/TCP edge. The backend (Phase 4) brokers connections via Fly's internal `.flycast` DNS or `.internal` DNS. This is the security boundary: if a workspace machine is ever reachable from `0.0.0.0`, the EdDSA scope-gate is the *last* line of defense rather than defense-in-depth.

### 0.4 `ROMMEL_WORKSPACE_ROOT = /workspace`, baked as `ENV`

The image declares `ENV ROMMEL_WORKSPACE_ROOT=/workspace` and `WORKDIR /workspace`. The backend mounts a per-workspace Fly volume at `/workspace` when it creates the Machine. Volumes are **not** declared in `fly.toml` — they're created per-workspace by the backend via the Machines API. Calling this out because it's a common Fly footgun (declaring volumes in `fly.toml` makes them app-wide).

### 0.5 Repo-root build context; `.dockerignore` lives at repo root

The Dockerfile must `COPY proto/` and `COPY sandbox-daemon/` from above its own directory, so the build context is the repo root. Docker's default `.dockerignore` lookup is `<context-root>/.dockerignore`, not next to the Dockerfile. Create a **new repo-root `.dockerignore`** focused on keeping the workspace-image build context lean (excludes `node_modules/`, `.next/`, `frontend/`, `backend/`, `docs/`, `.git/`, `dist/`, etc.). Document at the top of the file that it exists for `workspace-image/`; future Dockerfiles can extend it.

BuildKit's per-Dockerfile ignore (`workspace-image/Dockerfile.dockerignore`) is an alternative but couples builds to a BuildKit flag — repo-root is more portable.

### 0.6 Image tagging: `git rev-parse --short HEAD`, plus a `latest`-on-main

`scripts/build.sh` tags the image with the short git SHA. On `main` only, CI additionally tags `latest`. PR builds never tag `latest`. This matches the "Fly Machine images are git-pinned" assumption the backend will make when calling `flyctl machine run --image <ref>`.

### 0.7 Tool surface: `git`, `curl`, `ca-certificates`, `tini` only

Per §3 of the scaffolding plan, the image gets the minimum to be a functional workspace: `git` (for the repo the backend clones in), `curl` (for users / agent tooling that needs HTTP), `ca-certificates` (TLS), `tini` (PID-1 reaper). **Explicitly out of scope for Phase 3:** language toolchains (Node, Python, Go). The "user installs their toolchain via `apt`/`asdf` inside their workspace" model is fine for v1 — adding pre-baked toolchains is a Phase-N optimization once we know which ones are hot.

---

## 1. Files to create

```
workspace-image/
├── README.md
├── Dockerfile
├── fly.toml
├── Makefile
├── .gitignore                      # ignore local image-build scratch (token.pubkey.local, etc.)
├── rootfs/
│   └── etc/
│       └── rommel/
│           ├── daemon.env.example  # documents every ROMMEL_* the daemon reads
│           └── token.pubkey.example # placeholder Ed25519 PEM (dev-keys only, NOT a real prod key)
└── scripts/
    ├── build.sh                    # docker build, repo-root context, tags <sha>
    ├── push.sh                     # flyctl auth check, docker push to registry.fly.io
    └── entrypoint.sh               # exec tini → daemon, with env wiring
```

And at the repo root:

```
.dockerignore                       # new; written for the workspace-image build context
.github/workflows/workspace-image.yml
```

---

## 2. Step-by-step implementation

### Step 1 — Subtree skeleton

Create the directory tree above with empty placeholder files. Commit before any content lands; this lets the path-filtered CI workflow gate cleanly without an "empty subtree" footgun.

### Step 2 — `Dockerfile`

Multi-stage; repo-root build context. Two stages: `build` (compile static daemon binary) and runtime (`debian:stable-slim` + tools + binary + rootfs).

```dockerfile
# syntax=docker/dockerfile:1.7

# ---------- build stage: produce static sandbox-daemon binary ----------
FROM golang:1.22 AS build
WORKDIR /src

# proto Go client is gitignored; regenerate from schemas inside the build.
COPY proto ./proto
RUN bash ./proto/codegen/go.sh

COPY sandbox-daemon ./sandbox-daemon
WORKDIR /src/sandbox-daemon
RUN go mod download
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
      -o /out/sandbox-daemon ./cmd/daemon

# ---------- runtime stage: workspace image ----------
FROM debian:stable-slim

# Baseline tools. Pinned versionless on stable-slim; reproducibility comes from the digest pin below.
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

# EdDSA pubkey — build-arg controls source path; defaults to the placeholder example.
ARG ROMMEL_TOKEN_PUBKEY_FILE=workspace-image/rootfs/etc/rommel/token.pubkey.example
COPY ${ROMMEL_TOKEN_PUBKEY_FILE} /etc/rommel/token.pubkey

# Workspace mount point (Fly volume lands here at machine create time)
ENV ROMMEL_WORKSPACE_ROOT=/workspace
WORKDIR /workspace

# Entrypoint loads /etc/rommel/token.pubkey into env, then exec's the daemon under tini.
COPY workspace-image/scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/usr/bin/tini", "--", "/entrypoint.sh"]
```

**Notes:**

- The Go builder is the same shape as `sandbox-daemon/Dockerfile` today. After this lands we delete that file (see Step 8).
- `ROMMEL_PORT` defaults to `7777` in the daemon's `config.Load`; no need to set it as `ENV`.
- `ROMMEL_WID` is **not** set in the image — the backend injects it per-machine.

### Step 3 — `scripts/entrypoint.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

# Token pubkey is baked into the image at /etc/rommel/token.pubkey.
# The daemon's config.Load expects the PEM contents in ROMMEL_TOKEN_PUBKEY.
if [ -z "${ROMMEL_TOKEN_PUBKEY:-}" ]; then
  if [ -r /etc/rommel/token.pubkey ]; then
    ROMMEL_TOKEN_PUBKEY="$(cat /etc/rommel/token.pubkey)"
    export ROMMEL_TOKEN_PUBKEY
  else
    echo "entrypoint: /etc/rommel/token.pubkey not present and ROMMEL_TOKEN_PUBKEY not set" >&2
    exit 1
  fi
fi

# Per-machine values (ROMMEL_WID, optional ROMMEL_WORKSPACE_ROOT override) are
# expected from the Machines API. Fail fast if WID is absent so misconfig is loud.
if [ -z "${ROMMEL_WID:-}" ]; then
  echo "entrypoint: ROMMEL_WID is required (set by backend at machine create time)" >&2
  exit 1
fi

exec /usr/local/bin/sandbox-daemon
```

`tini` is the Docker `ENTRYPOINT`; this script is `exec`'d under it. That gives us PID-1 reaping for any child processes the daemon spawns later (PTYs, git subprocesses) without writing our own reaper.

### Step 4 — `scripts/build.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

# Run from any cwd; resolve repo root via git.
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

SHA="$(git rev-parse --short HEAD)"
IMAGE="${IMAGE:-rommel-workspaces}"
TAG="${TAG:-$SHA}"

# Optional override: caller can point at a real EdDSA pubkey PEM via env.
PUBKEY_FILE="${ROMMEL_TOKEN_PUBKEY_FILE:-workspace-image/rootfs/etc/rommel/token.pubkey.example}"

echo ">>> docker build $IMAGE:$TAG  (pubkey: $PUBKEY_FILE)"
docker build \
  -f workspace-image/Dockerfile \
  --build-arg "ROMMEL_TOKEN_PUBKEY_FILE=$PUBKEY_FILE" \
  -t "$IMAGE:$TAG" \
  .

# Tag latest only on main, only when called by CI.
if [ "${TAG_LATEST:-false}" = "true" ]; then
  docker tag "$IMAGE:$TAG" "$IMAGE:latest"
fi
```

### Step 5 — `scripts/push.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

if ! command -v flyctl >/dev/null 2>&1; then
  echo "push: flyctl not found; install from https://fly.io/docs/flyctl/" >&2
  exit 1
fi

if ! flyctl auth whoami >/dev/null 2>&1; then
  echo "push: not authenticated; run 'fly auth login' first" >&2
  exit 1
fi

SHA="$(git rev-parse --short HEAD)"
IMAGE="${IMAGE:-rommel-workspaces}"
TAG="${TAG:-$SHA}"
REGISTRY_IMAGE="registry.fly.io/$IMAGE:$TAG"

docker tag "$IMAGE:$TAG" "$REGISTRY_IMAGE"
flyctl auth docker
docker push "$REGISTRY_IMAGE"

if [ "${TAG_LATEST:-false}" = "true" ]; then
  docker tag "$IMAGE:$TAG" "registry.fly.io/$IMAGE:latest"
  docker push "registry.fly.io/$IMAGE:latest"
fi

echo ">>> pushed $REGISTRY_IMAGE"
```

### Step 6 — `fly.toml`

```toml
# Fly app for the Rommel workspace machines. Machines are created per-workspace
# by the backend via the Machines API; this file declares the app-level shape only.
app = "rommel-workspaces"
primary_region = "iad"   # override per deploy as needed

[build]
  # Image is pushed via scripts/push.sh; no Fly-side image build.
  image = "registry.fly.io/rommel-workspaces:latest"

[env]
  # ROMMEL_TOKEN_PUBKEY is baked into the image (Decision 0.2).
  # ROMMEL_WID and other per-machine values are set by the backend at machine create time.

# No [[services]] block: workspaces are not exposed to the public internet.
# Internal reachability uses Fly's .flycast / .internal DNS on the daemon port (7777).
# Backend (Phase 4) is the only thing that connects.

[[restart]]
  policy = "on-failure"
  retries = 3

# Volumes are intentionally NOT declared here. The backend creates one volume per
# workspace via the Machines API and attaches it as a mount at /workspace.
```

**Auto-stop / auto-start:** in the Machines API world these are per-machine flags (`auto_stop_machines = "stop"`, `auto_start_machines = true`), set when the backend calls `machine_create`. They don't belong in `fly.toml` for this app since `fly.toml` describes the app, not individual machines.

### Step 7 — `Makefile`

```make
.PHONY: help build push run-local clean

IMAGE ?= rommel-workspaces
TAG   ?= $(shell git rev-parse --short HEAD)

help:
	@echo "workspace-image — targets:"
	@echo "  make build         docker build $(IMAGE):$(TAG) from repo root"
	@echo "  make push          tag + push to registry.fly.io (needs flyctl + fly auth)"
	@echo "  make run-local     run the image locally on :7777 (smoke test)"
	@echo "  make clean         remove local image tags"

build:
	IMAGE=$(IMAGE) TAG=$(TAG) bash scripts/build.sh

push:
	IMAGE=$(IMAGE) TAG=$(TAG) bash scripts/push.sh

run-local: build
	@if [ -z "$$ROMMEL_WID" ]; then echo "set ROMMEL_WID for local smoke test"; exit 1; fi
	docker run --rm -p 7777:7777 \
	  -e ROMMEL_WID="$$ROMMEL_WID" \
	  $(IMAGE):$(TAG)

clean:
	-docker image rm $(IMAGE):$(TAG) 2>/dev/null || true
```

Also extend the root `Makefile` so `make build` at the repo root picks up the new subtree via the existing `run_if_exists` helper — no edit needed; the helper already does this automatically when `workspace-image/Makefile` lands.

### Step 8 — Remove `sandbox-daemon/Dockerfile`; update its README

Per Decision 0.1. The daemon README's "Building the Docker image" section gets replaced with:

> The daemon binary is consumed by [`workspace-image/`](../workspace-image/), which produces the production image. For local development, use `make run-local` (Go source); for image-level smoke tests, see `workspace-image/`.

### Step 9 — Repo-root `.dockerignore`

```
# Sweep out everything heavy that workspace-image's repo-root build context
# doesn't need. The Dockerfile only COPYs proto/ and sandbox-daemon/.
.git/
.github/
.claude/
.rommel/
docs/
frontend/
backend/
infra/
node_modules/
**/node_modules/
**/.next/
**/dist/
**/.venv/
**/__pycache__/
**/*.log
.DS_Store
```

### Step 10 — `.github/workflows/workspace-image.yml`

Path-filtered, gates on `workspace-image/Dockerfile` existing. PR builds image locally for smoke; `main` push additionally pushes to Fly registry (gated on a `FLY_API_TOKEN` repo secret).

```yaml
name: workspace-image

on:
  push:
    branches: [main]
    paths:
      - "workspace-image/**"
      - "sandbox-daemon/**"
      - "proto/**"
      - ".dockerignore"
      - ".github/workflows/workspace-image.yml"
  pull_request:
    paths:
      - "workspace-image/**"
      - "sandbox-daemon/**"
      - "proto/**"
      - ".dockerignore"
      - ".github/workflows/workspace-image.yml"

jobs:
  image:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Skip if workspace-image not yet scaffolded
        id: gate
        run: |
          if [ -f workspace-image/Dockerfile ]; then
            echo "scaffolded=true" >> "$GITHUB_OUTPUT"
          else
            echo "scaffolded=false" >> "$GITHUB_OUTPUT"
            echo "workspace-image/ not yet scaffolded; nothing to do."
          fi

      - uses: docker/setup-buildx-action@v3
        if: steps.gate.outputs.scaffolded == 'true'

      - name: Build (PR or main)
        if: steps.gate.outputs.scaffolded == 'true'
        run: bash workspace-image/scripts/build.sh

      - name: Setup flyctl
        if: steps.gate.outputs.scaffolded == 'true' && github.event_name == 'push'
        uses: superfly/flyctl-actions/setup-flyctl@master

      - name: Push to Fly registry (main only)
        if: steps.gate.outputs.scaffolded == 'true' && github.event_name == 'push'
        env:
          FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
          TAG_LATEST: "true"
        run: bash workspace-image/scripts/push.sh
```

---

## 3. Verification recipe

The image is "done" when each of the following passes from a clean clone.

### 3.1 Local build

```sh
make -C workspace-image build
docker image inspect rommel-workspaces:$(git rev-parse --short HEAD) >/dev/null
```

Expectations: build completes in <2 min on a warm Docker daemon; final image size <100 MB (sanity check on the slim base + binary).

### 3.2 Local smoke test (full WS round-trip)

```sh
# 1) Generate a dev EdDSA keypair (matches sandbox-daemon/README.md).
openssl genpkey -algorithm ed25519 -out /tmp/rommel-dev.pem
openssl pkey -in /tmp/rommel-dev.pem -pubout -out /tmp/rommel-dev.pub

# 2) Rebuild the image with the real pubkey baked in.
ROMMEL_TOKEN_PUBKEY_FILE=/tmp/rommel-dev.pub \
  make -C workspace-image build

# 3) Run the image.
docker run --rm -p 7777:7777 \
  -e ROMMEL_WID="dev-workspace" \
  rommel-workspaces:$(git rev-parse --short HEAD) &

# 4) Mint a token with the matching private key and round-trip system.ping
#    (steal the token-minting helper from sandbox-daemon/internal/ws/server_test.go).
curl -fsS http://localhost:7777/healthz       # → "ok"
# WS-level smoke is easiest via a tiny Go helper or websocat; record the
# command that works on your machine in workspace-image/README.md.
```

### 3.3 Fly deploy + cold-start measurement

```sh
fly auth login
fly apps create rommel-workspaces   # once, idempotent
make -C workspace-image push        # → registry.fly.io/rommel-workspaces:<sha>

# Create a single throwaway machine to validate the image.
fly machine run registry.fly.io/rommel-workspaces:<sha> \
  --app rommel-workspaces \
  --region iad \
  -e ROMMEL_WID="cold-start-probe" \
  --autostop=stop

# Measure cold start: stop the machine, then time the next `start` to first /healthz.
fly machine stop <id>
time ( fly machine start <id> && \
       until flyctl ssh console --app rommel-workspaces --machine <id> -C 'curl -fsS localhost:7777/healthz'; do sleep 0.05; done )
# Record the number. Target: 250ms-1s.
```

Cold-start results go into the completion doc as a number, not "roughly fast" — measuring is the point.

### 3.4 CI dry-run

Push a PR that touches `workspace-image/Dockerfile`. Expect `workspace-image.yml` to build (no push) and pass. Merge to main and confirm the `Push to Fly registry` step runs and a new `:latest` tag appears in the Fly registry.

---

## 4. Risks and gotchas

### 4.1 Repo-root build context surprises

`docker build -f workspace-image/Dockerfile .` is the only invocation shape that works. Running it from inside `workspace-image/` (`docker build .`) will fail at `COPY proto`. `scripts/build.sh` enforces `cd "$(git rev-parse --show-toplevel)"` to keep this invariant honest.

### 4.2 The `.dockerignore` location footgun

If someone adds a `workspace-image/.dockerignore` later thinking it's per-Dockerfile, it will be silently ignored (without BuildKit's per-file `<dockerfile>.dockerignore` feature). The repo-root `.dockerignore` is the only ignore file Docker reads for this build. Document this in `workspace-image/README.md`.

### 4.3 Tini as PID 1, daemon as the WS server

`tini` reaps zombies but doesn't forward signals to the child by default. We rely on tini's default behavior with `--`: SIGTERM → daemon → graceful shutdown via the existing signal handler in `cmd/daemon/main.go`. Phase 2 already verified `SIGINT/SIGTERM` graceful shutdown works at the binary level; confirm again with `docker stop` in §3.2.

### 4.4 `flyctl auth docker` token lifetime

The push script uses `flyctl auth docker` to install a short-lived Docker credential for `registry.fly.io`. In CI, the `FLY_API_TOKEN` secret stands in. Locally, expect to re-run `fly auth login` periodically. Not a bug — call it out in `workspace-image/README.md`.

### 4.5 Per-machine env vs. baked env

`ROMMEL_WORKSPACE_ROOT=/workspace` is baked as `ENV` in the Dockerfile. If the backend ever needs a different mount point (e.g., `/repo`), it can override via `-e ROMMEL_WORKSPACE_ROOT=…` at `machine create`. Don't move it into the entrypoint or `fly.toml` — `ENV` in the Dockerfile is the single source of truth.

### 4.6 Pubkey rotation requires image rebuild

By design (Decision 0.2). The trade-off: every rotation is a redeploy, but tokens can never outlive the deploy that minted the verifier. If this proves painful in practice, the Fly-secret variant in 0.2 is the documented escape hatch.

### 4.7 No language toolchains baked in (yet)

Workspaces can `apt install` what they need, but `apt update` over the public Debian mirrors from a freshly started Fly Machine takes ~5–10s. If "first command in a new workspace" UX matters, a Phase-N follow-up bakes the most-common toolchains (Node 20, Python 3.12, Go 1.22 — already pinned by `.tool-versions`). Out of scope for Phase 3.

---

## 5. Sequencing (suggested)

A reasonable per-PR carve-up if we want to land this in chunks:

1. **PR-1 — Skeleton + Dockerfile + entrypoint.** Tree, `Dockerfile`, `entrypoint.sh`, `rootfs/etc/rommel/*`, root `.dockerignore`. Verified locally with `docker build` + `docker run` + `/healthz`.
2. **PR-2 — Makefile + scripts.** `Makefile`, `scripts/build.sh`, `scripts/push.sh`. Verified by `make build` (no Fly login required).
3. **PR-3 — `fly.toml` + first Fly deploy.** Includes the cold-start number in the PR description as the gate.
4. **PR-4 — CI workflow.** `workspace-image.yml` — build on PR, push on main. Requires `FLY_API_TOKEN` repo secret.
5. **PR-5 — Cleanup.** Remove `sandbox-daemon/Dockerfile`, update its README, update the root README's subtree table.

Each PR is independently revertable; PR-3 is the "Phase 3 functionally complete" milestone.

---

## 6. Out of scope (deferred to later phases)

- Real `git clone` of the user's repo into `/workspace` at machine create — Phase 4 (backend) territory; the image just needs to mount the volume.
- Pre-baked language toolchains (see §4.7).
- Image signing / SBOM generation — security hardening pass, deferred.
- Multi-arch builds — Fly machines are amd64; one arch is fine until we add a local-dev arm64 story.
- Snapshot-and-resume (E2B-style) — explicitly future per `techstack.md`.
- `process.exec` daemon primitive — out of scope for the image; lives in `sandbox-daemon/` when it lands.

---

## 7. Completion doc target

When Phase 3 lands, write `docs/completions/phase-3-workspace-image.md` mirroring the structure of `phase-2-sandbox-daemon.md`:

- **What was built** — file tree + summary.
- **Decisions made** — every 0.X above, marked confirmed/revised.
- **Verification** — copy of §3 with real numbers (cold-start ms, image size MB, registry tag URL).
- **Cross-cutting** — note the production token-pubkey baking path is now live.
- **What's next** — `backend/` per scaffolding-plan §4.

Update `docs/changelog.md` with the `0.1.3` entry pointing at the completion doc.
