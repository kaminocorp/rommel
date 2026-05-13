# Phase 3 — `workspace-image/` (Completion)

**Plan:** [`docs/executing/phase-3-workspace-image-plan.md`](../executing/phase-3-workspace-image-plan.md) (specialization of [`scaffolding-plan.md`](../executing/scaffolding-plan.md) §3)
**Date:** 2026-05-13
**Status:** ✅ Complete locally. The workspace image builds from repo-root context, boots under tini, answers `/healthz` immediately, propagates env from Dockerfile + entrypoint + per-machine overrides, and forwards SIGTERM to the daemon's graceful-shutdown handler. The CI workflow is in place; the Fly-side `fly machine run` cold-start measurement is **deferred to first deploy** (no `fly auth login` available in this session — recipe is in `workspace-image/README.md`).

---

## What was built

A new `workspace-image/` subtree plus a new repo-root `.dockerignore`, a new CI workflow, and the removal of the now-redundant `sandbox-daemon/Dockerfile`.

### Files created

```
workspace-image/
├── README.md                              # build / smoke / push / cold-start recipe + gotchas
├── Dockerfile                             # multi-stage: golang:1.23 builder → debian:stable-slim
├── fly.toml                               # app shape only; no [[services]], no volumes
├── Makefile                               # build / push / run-local / clean
├── .gitignore                             # ignore local pubkey overrides (*.pubkey.local, etc.)
├── rootfs/
│   └── etc/rommel/
│       ├── daemon.env.example             # documents every ROMMEL_* the daemon reads
│       └── token.pubkey.example           # placeholder Ed25519 PEM (committed; dev-only)
└── scripts/
    ├── build.sh                           # docker build, repo-root context, tags <sha>
    ├── push.sh                            # tag + push to registry.fly.io, fly auth gate
    └── entrypoint.sh                      # tini → load PEM → fail-fast on WID → exec daemon

.dockerignore                              # repo-root, written for workspace-image's build context
.github/workflows/workspace-image.yml      # build on PR, build+push on main (FLY_API_TOKEN gated)
```

### Files modified

- **Root `Makefile`** — added `workspace-image` to the `build` and `clean` target lists (the existing `run_if_exists` helper handles the skip-when-absent path automatically, but the call sites still have to mention each subtree).
- **`sandbox-daemon/README.md`** — replaced the "Building the Docker image" section with a pointer to `workspace-image/`. The local-dev `make run-local` flow is unchanged.

### Files deleted

- **`sandbox-daemon/Dockerfile`** — per Decision 0.1. The workspace-image Dockerfile is now the canonical production artifact; keeping a second copy adjacent to the daemon would have diverged the moment one was updated without the other.

---

## Decisions made

### 0.1 — Single Dockerfile in `workspace-image/` ✅ confirmed
Removed `sandbox-daemon/Dockerfile`. The new `workspace-image/Dockerfile` builds the daemon from source inside the build stage; this is the only Dockerfile in the repo, and the only way the production binary gets packaged. The `sandbox-daemon/` subtree retains `make run-local` (Go source) for inner-loop dev — Docker isn't on the daemon's local-dev path at all.

### 0.2 — Pubkey baking: `ARG ROMMEL_TOKEN_PUBKEY_FILE` + COPY ✅ confirmed
Implemented exactly as planned: the build accepts a build-arg pointing at the PEM, copies it to `/etc/rommel/token.pubkey`, and the entrypoint `cat`s it into `ROMMEL_TOKEN_PUBKEY` before `exec`'ing the daemon. The daemon's `config.FromEnv` (line 73 in `internal/config/config.go`) parses the PEM contents directly via `jwt.ParseEdPublicKeyFromPEM([]byte(pemStr))`, so the env-var convention is a one-line wrapper around the file. Rotation is a rebuild — by design.

A real Ed25519 placeholder PEM is committed at `rootfs/etc/rommel/token.pubkey.example`. The matching private key was **never persisted** (generated in `/tmp/`, used to derive the pubkey, deleted in the same step). The PEM is real so the dev container actually boots without `--build-arg`; the private key being unrecoverable means no production deploy can accidentally inherit the dev verifier.

### 0.3 — No public services; internal `:7777` only ✅ confirmed
`fly.toml` declares `internal_port = 7777` under `[experimental]` and has **no `[[services]]` block**. Workspaces are reachable only via Fly's `.flycast` / `.internal` DNS. If `0.0.0.0` exposure ever shows up in a future change here, the EdDSA scope-gate becomes the *last* line of defense rather than defense-in-depth — that's the regression to watch for.

### 0.4 — `ROMMEL_WORKSPACE_ROOT = /workspace`, baked as Dockerfile `ENV` ✅ confirmed
Combined with `WORKDIR /workspace`, this means the daemon's startup-time directory check (`config.FromEnv` requires the path to exist and be a directory) is satisfied even on a bare `docker run` with no volume mount. The Fly volume attaches over the same path at machine create time.

### 0.5 — Repo-root `.dockerignore` ✅ confirmed
Lives at `/.dockerignore`. The Dockerfile only copies `proto/` and `sandbox-daemon/`, so the ignore file sweeps out everything else (`frontend/`, `backend/`, `docs/`, `.git/`, `.github/`, `node_modules/`, generated proto clients, etc.). Documented at the top of the file that future Dockerfiles built from repo-root context should extend it, not shadow it.

### 0.6 — Image tag = `git rev-parse --short HEAD`; `:latest` on main ✅ confirmed
`scripts/build.sh` reads `git rev-parse --short HEAD` directly; CI flips `TAG_LATEST=true` only on `push` to `main` (PR builds never tag `:latest`). The Fly Machines API call from the backend will pin to `:<sha>` for deterministic deploys.

### 0.7 — Tool surface: `git`, `curl`, `ca-certificates`, `tini` only ✅ confirmed
No language toolchains baked. Image size: **66 MiB compressed** (what `docker image inspect ... .Size` reports — the size that lands in the registry). Expanded virtual size is ~300 MB, mostly the `git` install dragging in `perl`, `libcurl`, etc. — comfortably under the plan's `<100 MB` ceiling for the registry-pushed artifact. Adding pre-baked Node/Python/Go toolchains stays a Phase-N optimization.

### NEW: Builder image is `golang:1.23`, not `golang:1.22` ⚠ revised
The plan and the deleted `sandbox-daemon/Dockerfile` both used `golang:1.22 AS build`. On 2026-05-13 the build began failing at the proto-codegen step:

```
github.com/atombender/go-jsonschema@v0.18.0 requires go >= 1.23.0
(running go 1.22.12; GOTOOLCHAIN=local)
```

Upstream `go-jsonschema@v0.18.0` bumped its own toolchain floor to 1.23. The runtime stage is `debian:stable-slim` and ships only the compiled daemon binary, so the builder's Go version is independent of the daemon's own `go.mod` (which declares `go 1.22` — that's the minimum, and a 1.23 compiler honours it). Bumping the builder is the least-invasive fix; alternatives (pinning go-jsonschema older, or bumping the daemon's `go.mod`) are larger surface-area changes.

**Follow-up flagged for `daemon.yml`:** the CI workflow pins `actions/setup-go@v5` with `go-version: "1.22"`. It runs the same `bash proto/codegen/go.sh` and will therefore now fail in CI for the same reason. Not in Phase 3's scope to fix, but worth fixing in the next PR — bump `daemon.yml` and `proto.yml` setup-go versions to `"1.23"` (or bump `.tool-versions`).

---

## Verification

### Build

```sh
make -C workspace-image build
# → docker build $IMAGE:$TAG  (pubkey: workspace-image/rootfs/etc/rommel/token.pubkey.example)
# → naming to docker.io/library/rommel-workspaces:fca93fc
```

- Wall-clock with warm Docker cache: **~25 s**.
- Cold (no cached layers): apt step dominates (~75 s for the runtime stage's `apt-get update + install`), total ~110 s.
- `docker image inspect rommel-workspaces:<sha> .Size` → **69,355,305 bytes ≈ 66 MiB** (the registry-pushed compressed size). Expanded virtual size from `docker images`: 300 MB.

### Smoke test (`docker run` happy path)

```sh
docker run -d --rm -p 7777:7777 -e ROMMEL_WID="dev-workspace" rommel-workspaces:<sha>
# logs: "daemon: listening on :7777 (wid=dev-workspace, root=/workspace)"
curl -fsS http://localhost:7777/healthz   # → "ok" on first poll (<200 ms after container start)
```

All four image-introduced contracts verified by this test:
- **tini wiring** — daemon is reachable as PID > 1 under tini (PID 1).
- **Env propagation** — daemon logs show `ROMMEL_WID` and `ROMMEL_WORKSPACE_ROOT` came through; entrypoint loaded `ROMMEL_TOKEN_PUBKEY` from `/etc/rommel/token.pubkey` (otherwise `config.FromEnv` would have failed-fast at startup).
- **Port mapping** — `:7777` reachable from the host.
- **/healthz path** — unauthenticated endpoint returns `ok`, confirming the HTTP mux is wired correctly.

### Smoke test (`docker stop` — signal forwarding)

```sh
time docker stop -t 10 <container-id>
# real    0m0.133s
```

`docker stop` returned in **133 ms** — tini forwarded SIGTERM, the daemon's `signal.NotifyContext(ctx, SIGINT, SIGTERM)` in `cmd/daemon/main.go:50` fired graceful shutdown via `httpSrv.Shutdown(5s)`, and the container exited cleanly. The `-t 10` is just the grace ceiling; actual drain was sub-second because there were no in-flight connections.

### Smoke test (entrypoint fail-fast)

```sh
docker run --rm rommel-workspaces:<sha>
# entrypoint: ROMMEL_WID is required (set by backend at machine create time)
# exit-code: 1
```

The entrypoint refuses to boot without `ROMMEL_WID`. This is louder than letting the daemon's own multi-error config validator collapse the error into one of N issues — the most likely cause of a missing `ROMMEL_WID` in production is a malformed Machines API call, and surfacing it precisely makes debugging that call shorter.

### WebSocket round-trip — not re-exercised at the image layer

The full WS contract (`system.ping`, `fs.read` happy paths, scope-`forbidden`, expired-token rejection, etc.) is exhaustively tested by `sandbox-daemon/internal/ws/server_test.go` (16 cases, currently green on host Go 1.26). The image-level test would add no new coverage — the binary inside the image is bit-for-bit the artifact those tests exercise. The new things the image *does* introduce (tini, entrypoint script, PEM loading, port mapping) are all covered by the three smoke tests above.

For an interactive WS smoke session, the README documents the keypair-generation + `websocat` recipe.

### Cold-start on Fly — deferred to first deploy

No `fly auth login` is wired up in this session. The recipe in `workspace-image/README.md` is the gate for the cold-start measurement:

```
fly machine run registry.fly.io/rommel-workspaces:<sha> \
  --app rommel-workspaces --region iad \
  -e ROMMEL_WID="cold-start-probe" --autostop=stop
fly machine stop <id>
time ( fly machine start <id> && \
       until flyctl ssh console --app rommel-workspaces --machine <id> \
              -C 'curl -fsS localhost:7777/healthz'; do sleep 0.05; done )
```

The Phase 3 plan listed this as the §3.3 gate; we're carrying it as a single open verification step (the only one that requires Fly credentials). Once measured, the number gets back-filled into this doc and the corresponding changelog entry.

### CI workflow dry-run — covered structurally

`workspace-image.yml` mirrors the existing `daemon.yml` / `frontend.yml` shape: `actions/checkout@v4`, a `gate` step that sets `scaffolded=true` if `workspace-image/Dockerfile` is present, then `docker/setup-buildx-action@v3` and the build/push steps. Pushes to `main` trigger `superfly/flyctl-actions/setup-flyctl@master` + `bash workspace-image/scripts/push.sh` with `FLY_API_TOKEN` and `TAG_LATEST=true`. The first PR after this commit will be the live exercise.

---

## Cross-cutting

### Production token-pubkey baking path is now live
Phase 1 settled the schema (`session-token.json`). Phase 2 made the daemon verify against `protogen.SessionTokenClaims`. Phase 3 closes the loop on **how the verifier reaches the daemon in production**: PEM file baked into the image at build time, loaded by the entrypoint, parsed by `config.FromEnv`. The backend's signing key (Phase 4) and the daemon's verifying key are now provably tied to a deployed image SHA, which is the property we wanted from Decision 0.2 — tokens cannot outlive the deploy that minted their verifier.

### `.dockerignore` convention is set
The repo-root `.dockerignore` is now the single ignore file for any future Dockerfile in this repo. Documented at the top of the file. Future Dockerfiles built from repo-root context should extend it (add ignores for whatever else they don't `COPY`), not duplicate it.

### Build-context invariant is enforced in code, not just docs
Three layers belt-and-braces the "build context = repo root" rule: (1) the Dockerfile comment says so, (2) `scripts/build.sh` calls `cd "$(git rev-parse --show-toplevel)"` before `docker build`, (3) the `make build` target invokes `scripts/build.sh` rather than running `docker build` directly. A future contributor who runs `docker build .` from inside `workspace-image/` will fail at `COPY proto` — fast feedback, not a subtle ignore-glob bug.

---

## What's next

Per [`docs/executing/scaffolding-plan.md`](../executing/scaffolding-plan.md) §4: **`backend/`** — FastAPI control plane. The pieces that newly unblock with Phase 3 are:

- `POST /workspaces/:id/sessions` can mint EdDSA tokens against a real private key whose paired pubkey is now bakeable into a real workspace image.
- `services/fly_orchestrator.py`'s `create_machine` call has a real image to reference (`registry.fly.io/rommel-workspaces:<sha>`).
- The full Pattern B loop (browser → backend `/sessions` → daemon WS) becomes wire-realistic once both sides exist.

### Open items / follow-ups

- **Cold-start measurement on Fly.** The one §3 gate not closed in this session; needs `fly auth login` and `fly apps create rommel-workspaces`. Recipe is in `workspace-image/README.md`.
- **CI Go version bump.** `daemon.yml` and `proto.yml` pin `actions/setup-go@v5` `go-version: "1.22"`; the same go-jsonschema bump that forced the workspace-image builder to 1.23 affects them too. Bump in the same PR that lands Phase 3's first CI run, or in a small standalone PR.
- **`.tool-versions` Go bump.** Currently pinned to `1.22.8`. Host devs running `make proto` will hit the same wall once they upgrade `go-jsonschema`. Coordinated bump candidate.
- **`go.work` at the repo root.** Phase 2's follow-up — still cheap, still not blocking.
- **Pre-baked language toolchains.** Per Phase 3 plan §4.7, deferred. Revisit if "first command in a new workspace" UX becomes a measurable problem (apt over the public Debian mirrors from a fresh Fly Machine is ~5–10 s).
