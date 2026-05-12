# sandbox-daemon

Long-lived Go binary that runs inside every workspace VM and exposes the daemon primitives from [`docs/primitives.md`](../docs/primitives.md) §1 over a WebSocket. Browser ↔ daemon traffic is JSON-over-WS, framed by the `Envelope` schema in [`proto/schemas/envelope.json`](../proto/schemas/envelope.json).

## Scope today

Phase-2 scaffolding (see [`docs/executing/scaffolding-plan.md`](../docs/executing/scaffolding-plan.md) §2). Implemented:

- WebSocket upgrade at `/ws?token=<jwt>`.
- Session-token validation against an **EdDSA** public key (`ROMMEL_TOKEN_PUBKEY`, PEM).
- `ping → pong` envelope echo.
- Real `fs.read` (path-sandboxed to `ROMMEL_WORKSPACE_ROOT`).
- Real `workspace.info`.
- Every other `fs.*` / `pty.*` primitive returns an error envelope with `code: "not_implemented"` so the surface area is visible.

`GET /healthz` is unauthenticated and returns `200 ok`.

## Environment

| Var | Required | Description |
| --- | --- | --- |
| `ROMMEL_PORT` | no (default `7777`) | TCP port for the HTTP/WS server. |
| `ROMMEL_TOKEN_PUBKEY` | **yes** | Ed25519 public key in PEM (`-----BEGIN PUBLIC KEY-----…`). Baked into the workspace image at build time. |
| `ROMMEL_WORKSPACE_ROOT` | **yes** | Absolute path to the workspace root. `fs.*` paths are resolved relative to this and may not escape it. |
| `ROMMEL_WID` | **yes** | Workspace id this daemon belongs to. Tokens whose `wid` claim doesn't match are rejected. |

## Running locally

```sh
# 1) generate a throwaway Ed25519 keypair
openssl genpkey -algorithm ed25519 -out /tmp/rommel-dev.pem
openssl pkey -in /tmp/rommel-dev.pem -pubout -out /tmp/rommel-dev.pub

# 2) export config
export ROMMEL_TOKEN_PUBKEY="$(cat /tmp/rommel-dev.pub)"
export ROMMEL_WORKSPACE_ROOT="$PWD"
export ROMMEL_WID="dev-workspace"

# 3) run
make run-local
```

You can mint a matching token with any EdDSA JWT signer (the backend will do this in phase 4); for now the daemon's own tests show the contract in `internal/ws/server_test.go`.

## Wire format

Every message is wrapped in an `Envelope` (see `proto/schemas/envelope.json`):

```json
{
  "kind": "request" | "response" | "event" | "error",
  "type": "fs.read",
  "id": "uuid-correlation-id",
  "payload": { … },
  "error": { "code": "not_implemented", "message": "…" }
}
```

Generated types live in `proto/clients/go/gen` (package `protogen`). The daemon imports them via a `replace` directive in `go.mod`; once a top-level `go.work` lands, the replace can go.

## Targets

```sh
make bootstrap   # go mod download
make lint        # go vet (+ golangci-lint if installed)
make test        # go test ./...
make build       # static binary → dist/sandbox-daemon
make run-local   # go run ./cmd/daemon (needs env vars above)
```

## Layout

```
sandbox-daemon/
├── cmd/daemon/main.go        # entrypoint
└── internal/
    ├── auth/                 # EdDSA JWT verify, scope checks
    ├── config/               # env parsing
    ├── fs/                   # fs.read real, fs.{write,list,watch} stubbed
    ├── pty/                  # all pty.* stubbed for v1 scaffolding
    ├── workspace/            # workspace.info
    └── ws/                   # gorilla upgrade, envelope codec, dispatch
```

## Building the Docker image

Build context must be the **repo root** (the Dockerfile copies `proto/` and `sandbox-daemon/` from there):

```sh
docker build -f sandbox-daemon/Dockerfile -t rommel-sandbox-daemon .
```

The image is intended for the `workspace-image/` subtree (Phase 3) to consume.
