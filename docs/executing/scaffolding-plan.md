# Repo Scaffolding Plan

Concrete plan to stand up the bones of every root-level directory called out in `docs/techstack.md`. Companion to `vision.md` and `primitives.md`.

**Definition of "scaffolded"**: each subtree compiles, lints, and runs a hello-world request end-to-end against its real deploy target (Vercel preview, Fly app, Fly machine image, etc.). No feature work — just the substrate that all later work plugs into.

**Order of execution** (sections below are written in this order):

1. Repo root — monorepo plumbing
2. `proto/` — codegen source-of-truth (other subtrees depend on it)
3. `sandbox-daemon/` — Go binary
4. `workspace-image/` — Docker image consuming the daemon
5. `backend/` — FastAPI control plane
6. `frontend/` — Next.js shell
7. `.rommel/` — own-dogfood planning funnel
8. `infra/` — placeholder for IaC
9. `docs/` — already exists; minor organizational notes

**Cross-cutting contract to settle on day one** (before sections 3 and 5 can be tested together): the **session-token format** the backend mints and the daemon validates. See section "Cross-cutting: session token contract" at the end.

---

## 0. Repo root

**Purpose**: Make the repo behave as a coherent monorepo with one command per common task.

### Files to create

```
rommel/
├── .gitignore
├── .editorconfig
├── README.md                    # one-paragraph what + pointer to docs/vision.md
├── Makefile                     # top-level entry points (see below)
├── package.json                 # pnpm workspace root, devDeps only
├── pnpm-workspace.yaml          # globs: frontend, proto/clients/ts
├── .nvmrc                       # pin Node version (e.g. 20)
├── .tool-versions               # asdf-style pin: node, go, python, pnpm
└── .github/
    └── workflows/
        ├── frontend.yml         # lint/build on changes under frontend/**
        ├── backend.yml          # lint/test on changes under backend/**
        ├── daemon.yml           # go vet/test/build on sandbox-daemon/**
        └── proto.yml            # regenerate clients, fail if diff
```

### `Makefile` targets (the contract for "what works")

```
make bootstrap     # install everything (pnpm i, poetry install, go mod download)
make proto         # regenerate TS / Go / Pydantic clients from proto/
make dev           # spin up frontend, backend, and a local daemon (concurrently)
make lint          # run all linters
make test          # run all tests
make build         # build everything (no deploy)
```

Each target delegates into the relevant subdir's own scripts — the Makefile is a router, not a replacement.

### `.gitignore` essentials

`node_modules/`, `.next/`, `__pycache__/`, `*.pyc`, `.venv/`, `dist/`, `build/`, daemon binary output, `.env`, `.env.*`, `.DS_Store`, `.fly/`, `.vercel/`.

**Open question to resolve here**: Turborepo vs bare pnpm. `techstack.md` leans bare. Sticking with bare for v1; revisit if cross-package caching becomes painful.

### Done when

- `make bootstrap && make lint && make build` passes on a fresh clone.
- CI workflows trigger on relevant path changes only.

---

## 1. `proto/` — protocol source-of-truth

**Purpose**: One canonical schema, three generated clients (TS, Go, Pydantic). Everything that crosses a process boundary uses these types.

### Format decision (to settle now)

`techstack.md` lists Protobuf vs JSON Schema as open. **Recommendation for v1: JSON Schema + a tiny custom wrapper for RPC envelopes.** Reasons:

- Daemon traffic is JSON-over-WebSocket per `primitives.md` cross-cutting question 1.
- Tooling (`datamodel-codegen` for Pydantic, `quicktype`/`json-schema-to-typescript` for TS, `go-jsonschema` for Go) is mature.
- Easier to debug in browser devtools (you can read messages without a decoder).
- Switch to Protobuf later if profiling demands it; the schemas port over.

### Directory layout

```
proto/
├── README.md                         # how to add a schema, how to regenerate
├── schemas/
│   ├── envelope.json                 # request/response/event envelope (id, type, payload)
│   ├── fs/                           # one file per fs.* primitive
│   │   ├── read.json
│   │   ├── write.json
│   │   ├── list.json
│   │   └── watch-event.json
│   ├── pty/
│   │   ├── open.json
│   │   ├── input.json
│   │   ├── output-event.json
│   │   └── resize.json
│   ├── git/                          # placeholder; one file per primitive when added
│   ├── funnel/                       # placeholder
│   ├── workspace/info.json
│   └── session-token.json            # claims for the broker token (see cross-cutting section)
├── clients/
│   ├── ts/                           # generated, gitignored except package.json
│   │   └── package.json              # name: @rommel/proto, consumed by frontend/
│   ├── go/                           # generated package, consumed by sandbox-daemon/
│   └── python/                       # generated Pydantic models, consumed by backend/
├── codegen/
│   ├── ts.sh
│   ├── go.sh
│   └── python.sh
└── codegen.sh                        # runs all three; idempotent
```

### Initial schemas to define

Start with the **bare minimum to wire end-to-end**: envelope, `fs.read`, `pty.open`/`pty.input`/`pty.output-event`, and `session-token`. Everything else from `primitives.md` gets a placeholder file with a TODO so the structure is visible.

### Done when

- `make proto` produces all three clients with no diff on second run.
- A trivial test in each consumer (`frontend/`, `backend/`, `sandbox-daemon/`) imports a generated type and uses it.
- CI fails if generated clients are out of date with schemas.

---

## 2. `sandbox-daemon/` — the workspace-side Go binary

**Purpose**: Long-lived daemon baked into the workspace VM image. Accepts WebSocket connections from the browser, exposes the daemon primitives from `primitives.md` §1.

### Directory layout

```
sandbox-daemon/
├── README.md
├── go.mod                            # module: github.com/<org>/rommel/sandbox-daemon
├── go.sum
├── cmd/
│   └── daemon/
│       └── main.go                   # parses flags, starts WS server
├── internal/
│   ├── auth/
│   │   └── token.go                  # validates session tokens minted by backend
│   ├── ws/
│   │   ├── server.go                 # gorilla/websocket upgrade + connection loop
│   │   └── envelope.go               # encode/decode against proto/clients/go
│   ├── fs/
│   │   └── handler.go                # fs.read implementation; rest stubbed
│   ├── pty/
│   │   └── handler.go                # pty.open/input/output via creack/pty
│   ├── workspace/
│   │   └── info.go
│   └── config/
│       └── config.go                 # env-driven config (PORT, TOKEN_PUBKEY, etc.)
├── Dockerfile                        # multi-stage: build static binary, COPY into scratch
├── Makefile                          # build, test, run-local
└── .golangci.yml
```

### Dependencies

- `github.com/gorilla/websocket` — per techstack.
- `github.com/creack/pty` — PTY allocation for `pty.open`.
- `github.com/golang-jwt/jwt/v5` — to validate session tokens.
- `github.com/fsnotify/fsnotify` — needed when `fs.watch` lands; can defer the import.

### Hello-world scope for scaffolding

Implement only:

- WebSocket upgrade at `/ws?token=...`.
- Token validation against a public key passed via env (`ROMMEL_TOKEN_PUBKEY`).
- Echo handler for envelope `type: "ping"` returning `type: "pong"`.
- One real handler: `fs.read` (so we prove the proto loop works end-to-end).

Everything else from §1 of `primitives.md` gets a stub returning `{"error": "not_implemented"}` so the surface area is visible but unfinished.

### Local dev

- `make run-local` starts the daemon on `:7777` with a dev token signing key, no real workspace constraints.
- A tiny `cmd/devclient/main.go` could be added later for hand-testing; not required for v1 scaffolding.

### Done when

- `go build ./...` succeeds.
- `go test ./...` includes a test that opens a WS, sends `ping`, receives `pong`.
- Static binary lands at `dist/sandbox-daemon` ready for `workspace-image/` to consume.

---

## 3. `workspace-image/` — the Fly Machine VM image

**Purpose**: Docker image that becomes the workspace sandbox. Bakes the daemon binary plus baseline tools (git, common runtimes).

### Directory layout

```
workspace-image/
├── README.md
├── Dockerfile                        # FROM debian:stable-slim; adds git, curl, daemon
├── fly.toml                          # app: rommel-workspaces; image used by Fly Machines API
├── rootfs/
│   ├── etc/
│   │   └── rommel/
│   │       └── daemon.env.example
│   └── usr/local/bin/
│       └── (daemon binary copied at build time)
├── scripts/
│   ├── build.sh                      # docker build, tags with git sha
│   ├── push.sh                       # push to Fly registry
│   └── entrypoint.sh                 # starts daemon, tails logs
└── .dockerignore
```

### Dockerfile shape

```dockerfile
FROM golang:1.22 AS daemon
COPY sandbox-daemon /src
WORKDIR /src
RUN make build

FROM debian:stable-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      git curl ca-certificates tini && rm -rf /var/lib/apt/lists/*
COPY --from=daemon /src/dist/sandbox-daemon /usr/local/bin/sandbox-daemon
COPY workspace-image/scripts/entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/usr/bin/tini", "--", "/entrypoint.sh"]
```

(The build context is the repo root so `COPY sandbox-daemon` works; `.dockerignore` keeps it lean.)

### `fly.toml` outline

- `app = "rommel-workspaces"`
- One process group, no public services (workspaces are reached via internal Fly DNS through the backend's broker URL).
- Auto-stop / auto-start enabled.
- Volumes are *not* declared here — they're created per-workspace via the Machines API by the backend.

### Done when

- `scripts/build.sh && scripts/push.sh` produces an image tagged in Fly's registry.
- A `fly machine run` of the image listens on `:7777` and answers `ping`.
- Cold-start measured (target from techstack: ~250ms-1s).

---

## 4. `backend/` — FastAPI control plane

**Purpose**: HTTP API for auth, workspace lifecycle, and session brokering (Pattern B). Stateless; talks to Supabase Postgres and Fly Machines API.

### Directory layout

```
backend/
├── README.md
├── pyproject.toml                    # poetry; deps: fastapi, uvicorn, pydantic, httpx,
│                                     #   python-jose[cryptography], asyncpg or sqlalchemy
├── poetry.lock
├── fly.toml                          # app: rommel-backend
├── Dockerfile                        # python:3.12-slim, uvicorn entrypoint
├── alembic.ini
├── Makefile                          # run, test, lint, migrate, deploy
├── .env.example
├── api/
│   ├── __init__.py
│   ├── main.py                       # FastAPI app factory, router includes
│   ├── deps.py                       # auth dependency, db session dependency
│   ├── auth.py                       # /auth/me, /auth/logout
│   ├── workspaces.py                 # /workspaces CRUD
│   ├── sessions.py                   # POST /workspaces/:id/sessions (the broker)
│   └── policy.py                     # GET/PUT /policy stubs
├── services/
│   ├── __init__.py
│   ├── workspace_lifecycle.py        # start/stop/create via fly_orchestrator
│   ├── fly_orchestrator.py           # thin client over Fly Machines API
│   └── session_broker.py             # mints session tokens for daemon
├── repositories/
│   ├── __init__.py
│   ├── base.py                       # protocol/interface for swappable backends
│   └── supabase/
│       ├── __init__.py
│       ├── workspaces.py
│       └── users.py
├── policy/
│   ├── __init__.py
│   └── rules.py                      # placeholder
├── alembic/
│   ├── env.py
│   └── versions/
│       └── 0001_init.py              # users, workspaces tables + RLS
└── tests/
    ├── conftest.py
    └── test_health.py
```

### Hello-world scope

- `GET /healthz` returns `{"ok": true}`.
- `GET /auth/me` validates a Supabase JWT and echoes claims.
- `POST /workspaces/:id/sessions` returns a stub `{daemon_url, token, expires_at}` with a real signed token (verifiable by the daemon).
- `services/fly_orchestrator.py` has a method signature for `create_machine` that just logs for now — wired up later.

### Auth seam

Per `techstack.md` "Supabase, accessed agnostically": create `backend/services/auth/` (or fold into `api/deps.py`) with one function `validate_jwt(token) -> UserClaims`. Today it loads Supabase's JWKS; a swap to Clerk/Auth.js means changing one function.

### Migrations

`0001_init.py` creates `users` and `workspaces` tables and enables RLS. Even with one row the RLS policy should be in there — adding RLS later to a populated table is painful.

### Done when

- `make run` boots Uvicorn locally; `curl /healthz` works.
- `make migrate` applies migrations against a local Supabase or test Postgres.
- `fly deploy` from `backend/` puts a live URL up.
- A signed session token from `POST /workspaces/:id/sessions` is accepted by the local daemon (the cross-cutting contract works).

---

## 5. `frontend/` — Next.js + Monaco shell

**Purpose**: The browser IDE. Vercel-deployed Next.js App Router app. Monaco for the editor, xterm.js for the terminal, both connecting via WebSocket directly to the daemon.

### Directory layout

```
frontend/
├── README.md
├── package.json                      # next, react, tailwindcss, @monaco-editor/react,
│                                     #   xterm, @xterm/addon-fit, @rommel/proto (workspace dep)
├── pnpm-lock.yaml                    # at repo root (workspace)
├── next.config.mjs
├── tailwind.config.ts
├── postcss.config.mjs
├── tsconfig.json
├── .env.example                      # NEXT_PUBLIC_BACKEND_URL, etc.
├── public/
├── src/
│   ├── app/
│   │   ├── layout.tsx
│   │   ├── page.tsx                  # landing / workspace picker
│   │   └── workspaces/
│   │       └── [id]/
│   │           └── page.tsx          # the IDE shell for one workspace
│   ├── components/
│   │   ├── shell/
│   │   │   ├── Header.tsx
│   │   │   └── StatusBar.tsx
│   │   ├── filetree/
│   │   │   └── FileTree.tsx          # stub: lists root via fs.list later
│   │   ├── editor/
│   │   │   └── EditorPane.tsx        # Monaco instance, dynamic import (no SSR)
│   │   ├── terminal/
│   │   │   └── TerminalPane.tsx      # xterm.js, dynamic import
│   │   └── funnel/
│   │       └── FunnelBoard.tsx       # placeholder for .rommel/ board
│   ├── lib/
│   │   ├── api.ts                    # typed client for backend (uses @rommel/proto types)
│   │   ├── daemon.ts                 # WebSocket client wrapper, envelope encode/decode
│   │   └── auth.ts                   # supabase client init
│   └── styles/
│       └── globals.css
└── tests/
    └── smoke.test.ts
```

### Hello-world scope

- Landing page at `/` reads `GET /workspaces` from backend (mocked if backend not up) and renders a list.
- Workspace page at `/workspaces/[id]` renders an empty Monaco pane, an empty xterm pane, and a connection-status pill.
- `lib/daemon.ts` opens a WebSocket using `daemon_url` + `token` from `POST /sessions`, sends `ping`, displays `pong`.

### Editor / terminal notes

- Monaco must be `dynamic(() => import(...), { ssr: false })` — it touches `window` on import.
- xterm needs `@xterm/addon-fit` to size to its container; resize listener calls `pty.resize`.
- Both stay deliberately barebones; no LSP, no theming layer beyond Monaco defaults.

### Done when

- `pnpm dev` boots locally; `/workspaces/dev` shows both panes and the ping/pong roundtrip works against a locally running daemon.
- `vercel --prod` (or push-to-main) deploys without errors.
- TypeScript build passes with `@rommel/proto` types imported.

---

## 6. `.rommel/` — own-dogfood planning funnel

**Purpose**: This repo uses its own funnel from day one. Bootstrapping this also lets us validate `funnel.*` primitives against a real folder layout.

### Directory layout (just folders + READMEs)

```
.rommel/
├── README.md                  # what this is, pointers to vision.md §Layer 2
├── triage/
│   └── .gitkeep
├── plans/
│   └── .gitkeep
├── next-up/
│   └── .gitkeep
├── executing/
│   └── this-scaffolding-plan.md   # symlink or duplicate of docs/executing/scaffolding-plan.md
├── completions/
│   └── .gitkeep
└── archive/
    └── .gitkeep
```

**Naming question to settle**: kebab-case (`next-up`) vs camelCase (`nextUp`) vs PascalCase (`NextUp`). `vision.md` uses display names ("Next Up") but says nothing about disk layout. Recommend **kebab-case** — Linux-friendly, no shell-quoting hazards, easy globbing.

**Open question**: should `.rommel/` actually be `rommel/` (no dot, visible by default in `ls`)? `vision.md` allows either. The kanban-on-disk concept argues for *visible* — recommend `rommel/`. Confirm with user before committing.

### Done when

- Folders exist, each with a one-line README explaining the stage.
- This very file lives under the `executing/` stage as proof of dogfooding.

---

## 7. `infra/` — IaC placeholder

**Purpose**: Per `techstack.md`, infra-as-code lives here "if/when needed". Don't over-build; just claim the directory.

### Directory layout

```
infra/
├── README.md                  # what belongs here, what doesn't
└── .gitkeep
```

### Done when

- Directory exists with a README that says "Terraform/Pulumi modules for Fly + Supabase land here when click-ops becomes painful. Empty by design until then."

This avoids cargo-culting a Terraform setup before there's a thing to manage.

---

## 8. `docs/` — already exists

No scaffolding work; just organizational notes:

- Keep `vision.md`, `techstack.md`, `primitives.md` at the top level.
- `docs/executing/` mirrors `.rommel/executing/` for human-friendly browsing on GitHub.
- `docs/refs/` already holds `oss-refs.md` and `research.md`; leave alone.
- Add `docs/roadmap.md` when there's content for it (referenced by `techstack.md` line 68 but not yet created — consider this a placeholder TODO).

---

## Cross-cutting: session token contract

Settle this **before** sections 2 (daemon) and 4 (backend) try to integrate.

**Decision needed**:

- **Algorithm**: EdDSA (Ed25519) — small keys, fast verify, no parameter-choice footguns. Alternative: RS256 if we want JWKS reuse with Supabase's stack.
- **Claims**:
  ```json
  {
    "iss": "rommel-backend",
    "sub": "<user_id>",
    "aud": "rommel-daemon",
    "wid": "<workspace_id>",
    "scope": ["fs:rw", "pty:rw", "git:rw"],
    "exp": <unix_ts>,
    "iat": <unix_ts>,
    "jti": "<uuid>"
  }
  ```
  `scope` answers `primitives.md` cross-cutting question 5 (capability scoping) — built in from day one as the README in `proto/schemas/session-token.json` documents.
- **Key handoff**: backend signs with private key (env var, mounted from Fly secret). Daemon verifies with public key (env var on the workspace VM, baked into image at deploy time *or* fetched from a known backend endpoint at boot — pick one; recommend baking for v1 to avoid a startup dependency).
- **Schema location**: `proto/schemas/session-token.json` — single source of truth, both Pydantic and Go models generated from it.

---

## Suggested execution order (concretely)

1. Repo root: `pnpm-workspace.yaml`, `Makefile`, `.gitignore`, CI skeletons. (≈1 hour.)
2. `proto/schemas/envelope.json` + `proto/schemas/session-token.json` + codegen scripts producing empty TS/Go/Python clients. (≈2 hours — the codegen tooling is the time sink.)
3. `sandbox-daemon/` to the point of `ping/pong` over WS with token validation. (≈half day.)
4. `workspace-image/` Dockerfile + Fly app, deployed and reachable. (≈half day.)
5. `backend/` to the point of `POST /sessions` minting a token the daemon accepts. (≈half day.)
6. `frontend/` shell with both panes wired to a real `/sessions` → daemon WS roundtrip. (≈1 day.)
7. `.rommel/` and `infra/` and `docs/roadmap.md` placeholder — minutes.

After step 6, the entire Pattern B loop works end-to-end with no real features. From there, every primitive in `primitives.md` is a small additive PR.

---

## Out of scope for scaffolding

- Real `fs.watch`, `fs.search`, `git.*`, `funnel.*` implementations.
- Multi-user concurrency on a single daemon (`primitives.md` cross-cutting question 4).
- Agent dispatch endpoints (`POST /workspaces/:id/agents`).
- Hermes (Layer 4 entirely).
- Billing, quotas, rate limits.
- Real RLS policies beyond the `0001_init.py` baseline.

These are all unblocked once scaffolding lands; calling them out here so they don't sneak in.
