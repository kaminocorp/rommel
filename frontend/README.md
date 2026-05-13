# `frontend/`

Rommel ADE — browser IDE shell. Next.js 15 + React 19 + Tailwind 4 on Vercel.

This is Layer 1 of the project (per [`docs/vision.md`](../docs/vision.md)) — the
substrate that future phases (file tree, PTY wiring, funnel UI) add verbs to.

## Layout

```
frontend/
├── middleware.ts                  # bouncer for /workspaces/* — redirects signed-out users
├── src/
│   ├── app/                       # App Router pages + route handlers
│   │   ├── layout.tsx             # root layout; QueryClientProvider
│   │   ├── page.tsx               # RSC: workspace picker
│   │   ├── sign-in/page.tsx       # Supabase magic-link form
│   │   ├── auth/callback/route.ts # OAuth code-exchange
│   │   └── workspaces/[id]/       # workspace shell (RSC + client island)
│   ├── components/
│   │   ├── shell/                 # Header, StatusBar, ConnectionPill, WorkspaceCreateButton
│   │   ├── filetree/              # FileTree (stub in v1)
│   │   ├── editor/                # EditorPane + monaco-impl
│   │   ├── terminal/              # TerminalPane + xterm-impl
│   │   ├── funnel/                # FunnelBoard (Phase 6+)
│   │   └── ui/                    # shadcn-style primitives
│   ├── lib/
│   │   ├── api.ts                 # typed HTTP client (TanStack-Query-friendly)
│   │   ├── daemon.ts              # WS wrapper: envelope, rpc, subscribe, reconnect, refresh
│   │   ├── auth.ts                # supabase-ssr factories
│   │   ├── env.client.ts          # zod-validated NEXT_PUBLIC_*
│   │   ├── env.server.ts          # zod-validated server-only secrets
│   │   ├── query.ts               # QueryClient factory
│   │   └── utils.ts               # cn(), invariant()
│   ├── hooks/
│   │   ├── useDaemonConnection.ts # bridges React lifecycle → DaemonConnection
│   │   └── useWorkspace.ts        # TanStack-Query wrappers for /workspaces, /sessions, /auth/me
│   ├── stores/connection.ts       # Zustand: WS state, current session, latest pong
│   ├── types/workspace.ts         # hand-rolled DTOs mirroring backend/api/*.py
│   └── styles/globals.css         # Tailwind v4 base + IDE shell shell theme
└── tests/
    ├── unit/                      # Vitest: daemon, connection store, env shape
    └── e2e/                       # Playwright: ★ phase-5 integration gate
```

## Local dev

```sh
# from repo root
pnpm install
cp frontend/.env.example frontend/.env.local
# edit .env.local with your Supabase project + the local backend URL

pnpm --filter ./frontend dev
# → http://localhost:3000
```

The full Pattern-B round-trip needs all three processes running. See the
integration-gate recipe in [`docs/completions/phase-5-frontend.md`](../docs/completions/phase-5-frontend.md).

## Env

| Var | Where | Why |
|---|---|---|
| `NEXT_PUBLIC_BACKEND_URL` | client + server | Base URL for the FastAPI control plane. |
| `NEXT_PUBLIC_SUPABASE_URL` | client + server | Supabase project URL. |
| `NEXT_PUBLIC_SUPABASE_ANON_KEY` | client + server | Supabase anon key (safe to ship). |
| `SUPABASE_SERVICE_ROLE_KEY` | server only | Optional; never prefix with `NEXT_PUBLIC_`. |

The split lives in `src/lib/env.client.ts` (public) and `src/lib/env.server.ts`
(server-only, guarded by `import "server-only"` and an ESLint
`no-restricted-imports` rule — risk 4.5 of the Phase-5 plan).

## Testing

```sh
pnpm --filter ./frontend test:unit       # Vitest
pnpm --filter ./frontend test:e2e        # Playwright (needs daemon + backend running)
```

The Playwright spec (`tests/e2e/ping.spec.ts`) is the **Phase-5 integration
gate** — it asserts the same round-trip Phase 4 proved with a Python WS
client, now driven from Chromium.

## Production

Vercel project pointed at `frontend/` as root. Push to `main` triggers
deploy; PRs trigger preview deploys.

**Known follow-up (risk 4.4):** prod uses `https://`, but the workspace
daemon is only reachable on `wss://<wid>.vm.rommel-workspaces.internal:7777`
which the browser can't resolve. The dev story works (`http` + `ws`); the
prod cutover needs a Flycast `wss://` proxy fronting the daemon. Flagged as
Phase-5.5.

## Monaco self-host upgrade path (risk 4.7)

`@monaco-editor/react` defaults to fetching Monaco from jsDelivr. If that's
blocked (corporate firewall, CSP), self-host:

1. Copy `node_modules/monaco-editor/min/vs/**` to `public/monaco/vs/`.
2. Add `loader.config({ paths: { vs: "/monaco/vs" } })` in `monaco-impl.tsx`
   before rendering `<Editor />`.

Deferred to Phase-N — the bundling story through Turbopack is its own
minefield.
