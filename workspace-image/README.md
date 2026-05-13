# workspace-image

Docker image that becomes a Rommel workspace VM on Fly Machines. Bakes the
[`sandbox-daemon`](../sandbox-daemon/) Go binary plus a minimal Linux
toolchain (`git`, `curl`, `ca-certificates`, `tini`) and the EdDSA public key
that gates session tokens.

The image is pushed to `registry.fly.io/rommel-workspaces:<git-sha>` and
referenced by the backend (Phase 4) when it creates per-workspace machines
via the Fly Machines API.

## Scope

Phase-3 scaffolding (see
[`docs/executing/phase-3-workspace-image-plan.md`](../docs/executing/phase-3-workspace-image-plan.md)).
Out of scope: real `git clone` of the user's repo into `/workspace`
(Phase 4), pre-baked language toolchains (Node/Python/Go), image signing.

## Layout

```
workspace-image/
├── Dockerfile                       # multi-stage: build daemon, bake into debian-slim
├── fly.toml                         # app shape; per-machine config lives in backend
├── Makefile                         # build / push / run-local
├── rootfs/
│   └── etc/rommel/
│       ├── daemon.env.example       # every ROMMEL_* the daemon reads
│       └── token.pubkey.example     # placeholder Ed25519 PEM (dev only)
└── scripts/
    ├── build.sh                     # docker build from repo root, tag <sha>
    ├── push.sh                      # tag + push to registry.fly.io
    └── entrypoint.sh                # tini → load PEM → exec daemon
```

## Build

The Dockerfile is **repo-root context**: it copies sibling subtrees
(`proto/`, `sandbox-daemon/`). Always invoke from the repo root or via the
provided script — `scripts/build.sh` enforces this with `git rev-parse
--show-toplevel`.

```sh
make build                                # tags rommel-workspaces:<short-sha>
ROMMEL_TOKEN_PUBKEY_FILE=/path/to/real.pub make build  # bake a real key
TAG_LATEST=true make build                # additionally tag :latest
```

Image size after build: ~110 MB (debian-slim ~75 MB + apt baseline ~25 MB +
static daemon ~7 MB). The
`-trimpath -ldflags="-s -w" CGO_ENABLED=0` build keeps the binary small and
hermetic.

## Local smoke test

End-to-end `system.ping` over a real WebSocket with a real EdDSA token.
Requires a dev keypair so the daemon's validator and a hand-minted token
agree.

```sh
# 1) Generate a throwaway Ed25519 keypair (one-time).
openssl genpkey -algorithm ed25519 -out /tmp/rommel-dev.pem
openssl pkey -in /tmp/rommel-dev.pem -pubout -out /tmp/rommel-dev.pub

# 2) Rebuild the image with that pubkey baked in.
ROMMEL_TOKEN_PUBKEY_FILE=/tmp/rommel-dev.pub make build

# 3) Run the image with a fake workspace id.
docker run --rm -p 7777:7777 \
  -e ROMMEL_WID="dev-workspace" \
  rommel-workspaces:$(git rev-parse --short HEAD)

# 4) In another shell, /healthz is unauthenticated:
curl -fsS http://localhost:7777/healthz                # → "ok"

# 5) The WS round-trip is easiest reproduced by the daemon's own tests —
#    sandbox-daemon/internal/ws/server_test.go has a token-minting helper
#    that pairs with /tmp/rommel-dev.pem. For ad-hoc poking, websocat plus
#    a jq-built envelope works:
#
#    websocat "ws://localhost:7777/ws?token=$JWT" <<<'{
#      "kind":"request","type":"system.ping","id":"01"
#    }'
```

## Push to Fly registry

```sh
fly auth login                                # one-time interactive auth
fly apps create rommel-workspaces             # one-time, idempotent
make push                                     # → registry.fly.io/rommel-workspaces:<sha>
TAG_LATEST=true make push                     # additionally push :latest
```

`flyctl auth docker` writes a short-lived Docker credential helper entry for
`registry.fly.io`; re-run `fly auth login` if pushes start failing. In CI,
the `FLY_API_TOKEN` repo secret stands in for the interactive session.

## Deploy a machine and measure cold start

```sh
# After `make push`:
fly machine run registry.fly.io/rommel-workspaces:<sha> \
  --app rommel-workspaces \
  --region iad \
  -e ROMMEL_WID="cold-start-probe" \
  --autostop=stop

# Cold start = stopped → first /healthz response:
fly machine stop <id>
time ( fly machine start <id> && \
       until flyctl ssh console --app rommel-workspaces --machine <id> \
              -C 'curl -fsS localhost:7777/healthz'; do sleep 0.05; done )
```

Target per [`docs/techstack.md`](../docs/techstack.md): 250 ms – 1 s.
Record the measured number in the Phase-3 completion doc.

## Environment baked into the image

| Where | Var | Value |
| --- | --- | --- |
| `ENV` in Dockerfile | `ROMMEL_WORKSPACE_ROOT` | `/workspace` |
| File at `/etc/rommel/token.pubkey` | `ROMMEL_TOKEN_PUBKEY` (loaded by entrypoint) | PEM contents passed via `--build-arg ROMMEL_TOKEN_PUBKEY_FILE=…` |
| Injected per-machine | `ROMMEL_WID` | set by backend at `machine_create` |
| Injected per-machine | `ROMMEL_PORT` | defaults to `7777` in daemon |

## Gotchas

- **Build context = repo root.** `docker build .` from inside this directory
  will fail at `COPY proto`. Use `make build` or `scripts/build.sh`.
- **`.dockerignore` lives at the repo root**, not next to this Dockerfile.
  Docker only reads `<context-root>/.dockerignore`; a per-Dockerfile ignore
  requires BuildKit's `<dockerfile>.dockerignore` extension which we
  deliberately avoid for portability.
- **Pubkey rotation = image rebuild.** The PEM is baked into the image
  layer; rotating means rebuilding and re-pushing. This is intentional
  (tokens can never outlive the deploy that minted their verifier). If this
  becomes painful, the Fly-secrets variant is documented as the escape
  hatch in
  [`docs/executing/phase-3-workspace-image-plan.md`](../docs/executing/phase-3-workspace-image-plan.md)
  §0.2.
- **No public services.** `fly.toml` has no `[[services]]` block. A
  workspace is reachable from inside Fly's private network only (`.flycast`
  / `.internal` DNS). If you ever see `0.0.0.0` exposure for this app,
  that's a regression and the scope-gated EdDSA token has become the
  *last* line of defense rather than defense-in-depth.

## Relationship to `sandbox-daemon/`

`sandbox-daemon/` is the source of the Go binary baked here. Local-dev
iteration on the daemon stays in `sandbox-daemon/` (run from Go source via
`make run-local`); this directory exists to package and ship the binary as
a Fly Machine image. Phase 3 removed `sandbox-daemon/Dockerfile` — the
canonical image is now this one.
