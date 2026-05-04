# Tech Stack

Locked-in technical choices for the Rommel ADE. Companion to `vision.md`.

## Stack at a glance

| Layer | Pick | Where it runs |
|---|---|---|
| Frontend | Next.js (App Router) + React + Tailwind | Vercel |
| Editor | Monaco | Browser |
| Terminal | xterm.js | Browser |
| Backend (control plane) | FastAPI + Pydantic | Fly.io |
| Sandbox daemon | Go + `gorilla/websocket` | Inside each workspace VM |
| Workspace VMs | Fly Machines + persistent volumes | Fly.io |
| DB | Postgres via Supabase, accessed through a repository abstraction | Supabase |
| Auth | Supabase Auth (JWT), validated in backend, swappable seam | Supabase + backend |
| Migrations / RLS | Alembic | Supabase Postgres |
| Protocol | `proto/` at repo root, codegen to TS / Go / Pydantic | Build step |
| Repo layout | Monorepo, independent deploys per subdirectory | One repo |

## Three-tier runtime architecture

```
Browser
  │
  ├─→ Vercel (Next.js — UI shell, mostly static + RSC)
  │
  ├─→ Fly.io: control-plane backend (FastAPI)
  │     ├─ Auth, workspace lifecycle, agent rule policy, billing
  │     └─→ Fly Machines API  (spawn/stop/snapshot workspace VMs)
  │
  └─→ Fly Machine (workspace sandbox, one per user)
        └─ sandbox-daemon (Go) — files, PTY, git over WebSocket
```

**The "window vs runtime" split:**

- **Backend (FastAPI) = the window.** Handles requests *about* workspaces: who can do what, which workspace exists, mint a session token. CRUD-shaped, low-frequency. ~10 requests per session, not per second.
- **Daemon (Go) = the ADE runtime.** Handles what happens *inside* a workspace: every keystroke, every FS read, every shell command. Long-lived WebSocket connections, byte streaming. Hot path.

## Connection model — Pattern B (broker-and-direct)

Once a session is established, the browser connects **directly** to the sandbox daemon. The backend is only involved at session-establishment time:

1. Browser asks backend for a session: `POST /workspaces/{id}/sessions`
2. Backend validates auth + permissions, returns `{ daemon_url, signed_token }` (token short-lived, ~5min, daemon-validated)
3. Browser opens WebSocket to `daemon_url` with token
4. Daemon validates token, session is live
5. All terminal I/O, file ops, FS watches — browser ↔ daemon, backend is *not* in the path

**Why Pattern B**:

- Hot-path performance independent of backend language (FastAPI is fine for control plane because it never sees keystrokes).
- Backend downtime doesn't kill active sessions, only new logins.
- Backend scales horizontally trivially (stateless CRUD).
- Standard pattern: GitHub Codespaces, Gitpod, Coder all work this way.

## Why each choice

### Fly Machines for workspace sandboxes

- Real Linux microVMs (Firecracker), not WASM fakes — `fly deploy`, `apt install`, real toolchains all work.
- Persistent volumes per machine — workspace state survives restarts.
- One Fly app (`rommel-workspaces`) hosts N machines, one per user workspace. Standard Fly pattern, scales to thousands.
- Auto-stop on idle: dormant workspaces cost ~nothing, restart in <1s when reconnected.
- `~250ms-1s` cold spawn.

E2B (self-hosted fork) is a future option if snapshot/branch-and-resume becomes load-bearing for Hermes. See `roadmap.md`.

### Go for the sandbox daemon

The daemon's job is byte-pumping: WebSocket fanout, PTY management, FS watching, streaming git output. Go's concurrency model (goroutines per connection), `os/exec`, and `gorilla/websocket` are the right shape. Small static binary, easy to bake into the workspace VM image.

### FastAPI for the backend

Given Pattern B, the backend is CRUD + orchestration: validate JWTs, call Fly Machines API, store workspace metadata, evaluate agent policy rules. Python is well-suited:

- Pydantic gives typed schemas the frontend can consume via OpenAPI.
- Policy/rule logic is easier to express in Python than Go.
- Performance is a non-issue because the hot path doesn't touch it.

### Next.js + Monaco

Monaco has first-class React integration (`@monaco-editor/react`). Next.js on Vercel gives painless preview deploys, RSC for the UI shell, and a familiar deployment model. Tailwind for styling because the IDE shell is mostly layout work.

### Supabase, accessed agnostically

We get managed Postgres + Auth + dashboard from Supabase, but the backend speaks to it through a repository layer (`backend/repositories/supabase/`). No `supabase-js` calls leak into business logic. RLS policies are managed via Alembic migrations (code-as-infra, reviewable in PRs). Auth is a swappable seam — JWT validation is generic; today it's Supabase, tomorrow it could be Clerk/Auth.js/anything.

## Repo layout (monorepo)

```
rommel/
├── frontend/          # Next.js → Vercel (Vercel project root: frontend/)
├── backend/           # FastAPI → Fly.io (own fly.toml)
│   ├── api/           # HTTP routes
│   ├── services/      # Business logic (workspace lifecycle, fly orchestrator)
│   ├── repositories/  # Data access layer
│   │   └── supabase/  # The only place supabase-specific code lives
│   ├── policy/        # Agent rule definitions and enforcement helpers
│   └── alembic/       # Migrations + RLS
├── sandbox-daemon/    # Go binary, baked into workspace VM image
├── workspace-image/   # Dockerfile for the workspace VM (bakes daemon + tools)
├── proto/             # Schema source-of-truth, codegen to TS/Go/Pydantic
├── docs/              # vision.md, techstack.md, primitives.md, roadmap.md
├── .rommel/           # The project's own planning funnel (eat your own dogfood)
└── infra/             # IaC (Terraform/Pulumi) for Fly + Supabase if/when needed
```

Each subdirectory deploys independently:

- `frontend/` → Vercel (push-to-deploy, project root set to `frontend/`)
- `backend/` → Fly.io (`fly deploy` from `backend/`)
- `sandbox-daemon/` → built and embedded into `workspace-image/` Docker image, which is pushed to Fly's registry as the workspace image
- `proto/` → consumed at build time by all three; not deployed itself

## Open-core posture

The project is OSS-first. Self-hostable end-to-end on a user's own Fly + Supabase. If/when there's a hosted commercial version, it lives in the same repo behind config flags or in an `enterprise/` directory — **no fork**. Forking diverges over time and turns every fix into a port. Auth abstraction is the key seam: OSS default is single-user / BYO-OIDC; commercial impl plugs in multi-tenant flows on the same interface.

## Decisions still open

- Frontend ↔ daemon transport: raw WebSocket vs WebTransport. (WebSocket for v1; revisit if performance demands.)
- Protocol format: Protobuf vs JSON Schema. (Leaning Protobuf for the strict typing, but JSON Schema is fine if we want easier debugging.)
- Monorepo tooling: bare `pnpm workspaces` vs Turborepo. (Bare is enough for v1.)
