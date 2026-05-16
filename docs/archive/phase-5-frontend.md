# Phase 5 — `frontend/` (Completion)

**Plan:** [`docs/archive/phase-5-frontend-plan.md`](../archive/phase-5-frontend-plan.md) (archived on completion; specialization of [`scaffolding-plan.md`](../executing/scaffolding-plan.md) §5)
**Date:** 2026-05-13
**Status:** ✅ Code authored to plan. The full `frontend/` subtree is in place: Next.js 15 App Router on Tailwind 4, Supabase SSR auth, TanStack Query for backend HTTP, Zustand for client-only WS state, dynamic-imported Monaco + xterm panes, and a hand-rolled `lib/daemon.ts` WS wrapper that owns envelope encode/decode + request-correlation + reconnect + token-refresh. Hermetic Vitest suite for `DaemonConnection` + connection store + env shape (8 + 5 + 2 cases) authored. Playwright integration-gate spec authored against the same Pattern-B round-trip Phase 4 proved against a Python client; the CI job that runs it is gated on `vars.RUN_E2E == 'true'` so it stays opt-in until the Supabase test-user + secrets are configured. **Network-bound verification — `pnpm install` lockfile resolution, `next build`, the live Playwright pass, and the first Vercel deploy — is the named carryover** for the first session with outbound network + Vercel/Supabase access (the same shape as Phase-4's deferred `fly deploy`).

The browser side of the Pattern-B auth loop is now committed. Phase 1 wrote the contract; Phase 2 made the daemon verify; Phase 3 baked the verifier into the image; Phase 4 made the backend sign; Phase 5 makes the browser the originator. With this in place, every subsequent phase is an additive PR against the WS wrapper — `fs.list` lights up the file tree, `pty.open`/`pty.input`/`pty.output` light up the terminal, `funnel.*` lights up the kanban board.

---

## What was built

A new `frontend/` subtree (Next.js 15 IDE shell), an upgraded `frontend.yml` CI workflow (build + lint + typecheck + unit tests on every PR; opt-in Playwright integration gate), and a small root `package.json` change (pnpm `overrides` for the React 19 `@types/react` peer-dep alignment, risk 4.8).

### Files created

```
frontend/
├── README.md                                   # layout, dev recipe, env table, Monaco self-host upgrade path
├── package.json                                # @rommel/frontend; deps + devDeps per plan §1
├── Makefile                                    # bootstrap / dev / lint / test / build / clean
├── tsconfig.json                               # strict + noUncheckedIndexedAccess + exactOptionalPropertyTypes
├── next.config.mjs                             # transpilePackages: ["@rommel/proto"] (risk 4.2)
├── tailwind.config.ts                          # darkMode=class; pill.* color tokens
├── postcss.config.mjs
├── eslint.config.mjs                           # flat config; no-restricted-imports guards env.server (risk 4.5)
├── .prettierrc.json                            # prettier-plugin-tailwindcss
├── .env.example
├── .gitignore
├── vercel.json
├── playwright.config.ts                        # one project (chromium); webServer for local dev
├── vitest.config.ts                            # jsdom env, @ alias
├── middleware.ts                               # auth bouncer on /workspaces/:path*
├── public/
│   └── favicon.svg
├── src/
│   ├── app/
│   │   ├── layout.tsx
│   │   ├── providers.tsx                        # client wrapper: QueryClientProvider
│   │   ├── page.tsx                             # RSC: workspace picker (Bearer from supabase cookie)
│   │   ├── sign-in/page.tsx                     # client: Supabase magic-link OTP
│   │   ├── auth/callback/route.ts               # OAuth/magic-link code-exchange handler
│   │   └── workspaces/[id]/
│   │       ├── page.tsx                         # RSC: fetch workspace, hand off to client
│   │       └── workspace-client.tsx             # client island: layout + useDaemonConnection mount
│   ├── components/
│   │   ├── ui/button.tsx                        # shadcn-style Button (Slot, cva)
│   │   ├── shell/Header.tsx
│   │   ├── shell/StatusBar.tsx
│   │   ├── shell/ConnectionPill.tsx             # data-testid for the integration gate
│   │   ├── shell/WorkspaceCreateButton.tsx
│   │   ├── filetree/FileTree.tsx                # stub (Phase 6 wires fs.list / fs.watch)
│   │   ├── editor/EditorPane.tsx                # dynamic({ ssr: false }) wrapper
│   │   ├── editor/monaco-impl.tsx               # @monaco-editor/react with WELCOME buffer
│   │   ├── terminal/TerminalPane.tsx            # dynamic wrapper
│   │   ├── terminal/xterm-impl.tsx              # @xterm/xterm + fit + web-links; ResizeObserver
│   │   └── funnel/FunnelBoard.tsx               # placeholder for Phase 6+
│   ├── lib/
│   │   ├── api.ts                               # typed fetch + ApiError
│   │   ├── daemon.ts                            # ★ DaemonConnection: envelope, rpc, subscribe, reconnect, refresh
│   │   ├── auth.ts                              # browser / server / middleware Supabase factories + getAccessTokenFromCookies
│   │   ├── env.client.ts                        # zod-validated NEXT_PUBLIC_*
│   │   ├── env.server.ts                        # zod-validated server-only secrets, "server-only" guard
│   │   ├── query.ts                             # QueryClient factory (one-per-tab on browser, one-per-req on server)
│   │   └── utils.ts                             # cn(), invariant()
│   ├── hooks/
│   │   ├── useDaemonConnection.ts               # React-lifecycle bridge: POST /sessions → connect → ping
│   │   └── useWorkspace.ts                      # useMe, useWorkspaces, useWorkspace, useCreateWorkspace, useCreateSession
│   ├── stores/connection.ts                     # Zustand: status, sessionToken, daemonUrl, expiresAt, lastError, lastPong
│   ├── types/workspace.ts                       # DTOs mirroring backend/api/*.py
│   └── styles/globals.css                       # @import "tailwindcss"; .monaco-host / .xterm-host
└── tests/
    ├── unit/
    │   ├── daemon.test.ts                       # 8 cases: ?token append, rpc id-correlation, error envelopes, subscribe fanout, status transitions, in-flight reject on close, refresh on 1008
    │   ├── connection-store.test.ts             # 5 cases
    │   └── auth.test.ts                         # env.client shape, auth factory surface
    └── e2e/
        └── ping.spec.ts                         # ★ Phase-5 integration gate (CI: opt-in via vars.RUN_E2E)
```

### Files modified

- **`.github/workflows/frontend.yml`** — awakened. Build job now runs `proto/codegen/ts.sh`, `pnpm install --frozen-lockfile`, lint, typecheck, build (with CI placeholder `NEXT_PUBLIC_*` so zod parsing succeeds), and Vitest. New `e2e` job (`if: vars.RUN_E2E == 'true'`) brings up the full Pattern-B stack — postgres service container, Go 1.23 build of the daemon, Poetry install + Alembic upgrade + uvicorn in the background, daemon binary in the background, frontend dev server in the background, Playwright with chromium. Mirrors the Phase-4 backend.yml integration-gate shape but spans one more process.
- **`package.json` (repo root)** — added `"pnpm": { "overrides": { "@types/react": "^19.0.0", "@types/react-dom": "^19.0.0" } }` so transitive `peerDeps: @types/react@^18` declarations don't shadow the React-19 typings the frontend uses (risk 4.8).

### Files deleted / moved

- **`docs/executing/phase-5-frontend-plan.md`** → **`docs/archive/phase-5-frontend-plan.md`** — same archival move Phase 3 and Phase 4 made on completion; the executing folder is reserved for in-flight plans.

---

## Decisions made

### 0.1 — Next.js 15 App Router + React 19 ✅ confirmed
`package.json` pins `next: 15.0.0`, `react: 19.0.0`, `react-dom: 19.0.0`. App Router everywhere; the two server components are `app/page.tsx` (RSC fetch of `/workspaces`) and `app/workspaces/[id]/page.tsx` (RSC fetch of one workspace). Everything else is `"use client"`. Monaco and xterm are pulled through `next/dynamic` with `ssr: false` to avoid the module-eval `window` access (risk 4.1) — codified as the structural pattern in `EditorPane.tsx` / `TerminalPane.tsx`.

### 0.2 — `@supabase/ssr` for cookie sessions, plain `@supabase/supabase-js` only via the SSR re-exports ✅ confirmed
`lib/auth.ts` exposes three factories: `createBrowserClient` (client components), `createServerClient(cookies())` (RSC / route handlers), `createMiddlewareSupabaseClient(req, res)` (middleware.ts). Plus `getAccessTokenFromCookies(cookieStore)` helper that RSCs use to extract the JWT for `Authorization: Bearer …`. `middleware.ts` runs on `/workspaces/:path*` and redirects signed-out users to `/sign-in?next=<url>`. Magic-link is the v1 flow; `/auth/callback/route.ts` is the code-exchange handler.

### 0.3 — `@monaco-editor/react` v4 with CDN loader ✅ confirmed
Pinned `@monaco-editor/react: ^4.6.0` + `monaco-editor: ^0.52.0`. `EditorPane.tsx` is the dynamic boundary; `monaco-impl.tsx` is the actual `<Editor>` mount with a markdown WELCOME buffer. Self-host upgrade path (risk 4.7) documented in `frontend/README.md` — left out of v1 deliberately because bundling Monaco's workers through Turbopack is its own minefield.

### 0.4 — `@xterm/xterm` v5 + `@xterm/addon-fit` + `@xterm/addon-web-links` ✅ confirmed
Pinned. `xterm-impl.tsx` mounts a `Terminal`, loads both addons, wires a `ResizeObserver` to `fit.fit()`, and writes a banner. `disableStdin: true` because PTY wiring is Phase 6 — no input goes through the WS yet. The `try { fit.fit() } catch` guard handles the 0-sized initial measurement during StrictMode double-mount.

### 0.5 — Hand-rolled `lib/daemon.ts`, no WS library ✅ confirmed — the load-bearing piece
`DaemonConnection` is a class (long-lived state reads cleaner than a closure: in-flight map, reconnect counter, ready-resolvers stack). Public API:
- `connect(): Promise<void>` — resolves when the socket reaches `ready`.
- `rpc<TReq, TRes>(type, payload): Promise<TRes>` — sends `request`, awaits the matching `response` (or `error`) by `id`, rejecting with `DaemonProtocolError` on the `error` envelope.
- `subscribe(type, handler): () => void` — fan-out for `event` envelopes; returns an unsubscribe fn.
- `close(): void` — graceful; rejects in-flight RPCs with `DaemonClosedError` so React state never leaks (risk 4.9).
- `getStatus() / onStatusChange` — exposes the five-state machine (`connecting` / `ready` / `reconnecting` / `failed` / `closed`).

Internals:
- Token append: `appendTokenIfMissing(url, token)` so callers don't have to encode the query themselves; `useDaemonConnection` passes the daemon URL straight from the backend without manual concatenation.
- Reconnect: exponential backoff 250ms → 5s, abort at 5 attempts → `failed`.
- Refresh: three triggers stacked — close-1008/4401, server-side `error.code === "invalid_token"`, and a wall-clock timer that fires `expires_at - 30s` before TTL. All three call the caller-injected `refresh()` (which `useDaemonConnection` wires to `useCreateSession.mutateAsync`).
- Dispatch: `dispatch(frame)` is the only place envelopes are interpreted. Events go to the subscriber map; responses/errors look up by `id`; bad frames are silently dropped (logging deferred — that's a single line away when telemetry lands).

### 0.6 — TanStack Query v5 for HTTP, Zustand for WS / connection state ✅ confirmed
`@tanstack/react-query: ^5.59` + `zustand: ^5.0`. `lib/query.ts` returns one `QueryClient` per request on the server, one per browser tab on the client (the standard Next App Router pattern). `stores/connection.ts` is a single-store Zustand with `setStatus`, `setSession`, `setLastPong`, `setLastError`, `reset` — Phase 6 will extend it (open tabs, dirty buffers) but the shape is stable for v1. No Redux, no Jotai.

### 0.7 — Tailwind 4 + minimal shadcn (just `Button` for now) ✅ revised
The plan's §0.7 named six shadcn primitives (Button, Dialog, DropdownMenu, Tooltip, Toast, Tabs). V1 only ships **Button**; the other five are deferred until a concrete consumer needs them (Phase 6+ UI verbs). Adding them all preemptively bloats the bundle and the diff with no shipping benefit. Lucide-react is pinned because Phase 6 will reach for it the moment a tooltip or dropdown lands.

### 0.8 — Vercel deployment ✅ confirmed in code, deferred in execution
`vercel.json` written (`buildCommand: "pnpm --filter ./frontend build"`, `installCommand: "pnpm install --frozen-lockfile"`); env-var table documented in `frontend/README.md`. The actual `vercel link` + `vercel env` + first prod deploy is the carryover, exactly the way Phase 3 deferred the first `fly machine run` and Phase 4 deferred the first `fly deploy` — same authorisation-not-available shape.

### 0.9 — Vitest for units, Playwright for one smoke test ✅ confirmed
`tests/unit/`: `daemon.test.ts` (8 cases), `connection-store.test.ts` (5), `auth.test.ts` (2). `tests/e2e/ping.spec.ts` programmatically signs in via Supabase password grant, plants the SSR cookie, navigates to a workspace, and asserts `[data-testid=connection-pill][data-status=ready]` within 15 s. CI runs the unit suite unconditionally; the e2e job is `if: vars.RUN_E2E == 'true'` (off until Supabase test-user secrets land).

### 0.10 — TypeScript strict, ESLint flat config ✅ confirmed
`tsconfig.json` enables `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noImplicitOverride`, `noFallthroughCasesInSwitch`. ESLint flat config wires `@eslint/js` + `typescript-eslint` + `react` + `react-hooks` + `jsx-a11y`. The `no-restricted-imports` rule pins `**/lib/env.server` so client components can't accidentally reach a server-only module — risk 4.5's mechanical guard.

### NEW — `env.client.ts` / `env.server.ts` split ⚠ refined
Plan §step-1 sketched a single `lib/env.ts`. Risk 4.5 flagged the leak path (any `NEXT_PUBLIC_*` is in the client bundle by default; the catch is the *server-only* keys). The shipped form splits the schema into two modules. `env.server.ts` carries `import "server-only"` at the top, which is Next's official trip-wire: importing it from a `"use client"` file fails the build. The ESLint rule is the second line of defense; together they make the leak structurally unreachable.

### NEW — programmatic Playwright sign-in via Supabase password grant, not magic-link ⚠ refined
Plan §step-6 said "sign in via a seeded Supabase user." Playwright can't drive a real magic-link email, so the spec POSTs `${SUPABASE_URL}/auth/v1/token?grant_type=password` with seeded credentials and plants the cookie `sb-<project>-auth-token` that `@supabase/ssr` reads. The Supabase test user has to be created out-of-band (in the Supabase dashboard or via the admin API) with password auth enabled — recipe in the CI step's secret table below.

### NEW — daemon URL `?token=…` append is owned by the wrapper, not the caller ⚠ refined
Plan §step-4 sketched `new DaemonConnection(\`${daemon_url}?token=${token}\`, token, …)`. Shipped form takes `{ url, token }` separately and the wrapper appends `?token=` only if not already present. Three reasons: (a) the daemon's `internal/ws/server.go::Upgrade` reads it from the query string regardless of the URL the caller constructed; (b) re-using the same wrapper across refresh cycles is cleaner if the token is mutable; (c) tests can assert the URL the wrapper actually opened — and they do (`tests/unit/daemon.test.ts::"appends ?token=..."`).

---

## Cross-cutting: Pattern-B auth loop is now end-to-end browser-driven

Phases 1–4 stood up the wire (contract → verifier → image → signer). Phase 5 puts the originator in the browser. The properties earned by this phase:

- **The same JWT shape the Phase-4 integration gate proved against `websockets` (Python) is now consumed by Chromium.** `frontend/src/types/workspace.ts::SessionResponse` matches `backend/api/sessions.py::SessionOut` field-for-field (`daemon_url`, `token`, `expires_at`); `DaemonConnection` reads only those three pieces and never sees the JWT internals — the broker is the only signer, the daemon is the only verifier, and the browser is a transparent shipper of the bytes between them.
- **Capability scoping is enforced wire-to-wire from the same call-site.** `useCreateSession` → backend's `mint_token(..., scopes=settings.default_scopes)` → `ROMMEL_DEFAULT_SCOPES` env (`fs:rw,pty:rw,git:rw,funnel:rw,policy:r` in dev). The browser does not get to pick scopes; the v1 contract is "whatever the broker decided at mint time," which is the property `primitives.md` cross-cutting Q5 wanted.
- **Refresh closes the loop on TTL.** The Phase-4 plan's risk 4.6 — "5-minute TTL is shorter than a real editor session" — is now owned by `DaemonConnection.refreshAndReopen`. Re-mint is one `POST /workspaces/:id/sessions` (cookies provide the Supabase JWT; the backend re-derives `wid`, scopes, exp from the same `Settings`), then the socket re-opens. The user sees a one-frame pill flip from `ready` → `reconnecting` → `ready`; no manual action.
- **Production reachability remains the named gap.** Risk 4.4 of the plan: `https://rommel.vercel.app` cannot open `ws://localhost:7777`, and `wss://<wid>.vm.rommel-workspaces.internal:7777` is Fly-private — the browser cannot resolve it. The dev story works (http/ws on localhost); the prod cutover needs the Phase-5.5 Flycast `wss://` proxy fronting the daemon. Flagged here as the **load-bearing follow-up** before any external user can hit the production app and have it round-trip a ping.

---

## Verification

### Local boot (offline-feasible parts)

```sh
cd frontend
cp .env.example .env.local           # then fill in Supabase + backend URL
pnpm install                          # at repo root, hydrates the workspace
pnpm --filter ./frontend dev          # → http://localhost:3000
```

### Hermetic Vitest suite (no daemon, no backend, no network)

```sh
pnpm --filter ./frontend test:unit
```

Expected runs (authored, pending live execution in a network-enabled session):

- `tests/unit/daemon.test.ts` — 8 cases, all asserting against an in-test `FakeWebSocket` that records `send()` calls and exposes `emitMessage` / `emitClose`. Covers ?token-append, request/response correlation by `id`, error-envelope rejection with `DaemonProtocolError`, event subscription fan-out + unsubscribe, status-machine transitions, in-flight rejection on close (risk 4.9), refresh-on-1008 (risk 4.6).
- `tests/unit/connection-store.test.ts` — 5 cases on the Zustand store: initial state, `setStatus`, `setSession`, `setLastPong`, `reset`.
- `tests/unit/auth.test.ts` — 2 cases: `env.client` exposes only `NEXT_PUBLIC_*` (and crucially **not** `SUPABASE_SERVICE_ROLE_KEY`), and the auth module exports all four expected factories.

### The integration gate — browser signs, daemon accepts (Pattern-B end-to-end)

The Phase-4 transcript looked like this (Python WS client):

```
INTEGRATION GATE PASS — backend signs → daemon verifies → ping round-trips
frame: { "kind": "response", "type": "system.ping", "id": "...", "payload": { "ok": true, "ts": "..." } }
```

The Phase-5 equivalent flips the originator to Chromium. The recipe (three terminals):

```sh
# Terminal 1: daemon (Phase 2/3 artifact, used as-is)
openssl genpkey -algorithm ed25519 -out /tmp/dev.pem
openssl pkey -in /tmp/dev.pem -pubout -out /tmp/dev.pub
ROMMEL_TOKEN_PUBKEY="$(cat /tmp/dev.pub)" \
  ROMMEL_WORKSPACE_ROOT="$PWD" \
  ROMMEL_WID="dev-workspace" \
  make -C sandbox-daemon run-local

# Terminal 2: backend (Phase 4 artifact, used as-is)
docker compose -f backend/compose.yaml up -d postgres
ROMMEL_TOKEN_PRIVKEY="$(cat /tmp/dev.pem)" \
  ROMMEL_DAEMON_URL_TEMPLATE="ws://localhost:7777/ws" \
  ROMMEL_DATABASE_URL=postgresql+asyncpg://rommel:rommel@localhost:5432/rommel \
  ROMMEL_DATABASE_MIGRATE_URL=postgresql://rommel:rommel@localhost:5432/rommel \
  ROMMEL_SUPABASE_JWKS_URL=https://<project>.supabase.co/auth/v1/.well-known/jwks.json \
  make -C backend migrate run

# Terminal 3: frontend (Phase 5 artifact)
cp frontend/.env.example frontend/.env.local        # fill in the four NEXT_PUBLIC_*
pnpm --filter ./frontend dev

# Browser:
#   http://localhost:3000  →  sign in via Supabase magic link
#   → workspace picker  →  "New workspace"  →  /workspaces/<uuid>
#   → connection pill flips connecting → ready
#   → DevTools network: WS frame to ws://localhost:7777/ws?token=...
#     Up: { "kind":"request",  "type":"system.ping", "id":"<uuid>", "payload":{} }
#     Down: { "kind":"response","type":"system.ping","id":"<uuid>","payload":{"ok":true,"ts":"..."} }
```

**Status:** the recipe is authored and offline-verified end-to-end against the code (every URL, every env var, every wire-shape is consistent between `frontend/src/lib/api.ts`, `frontend/src/lib/daemon.ts`, `backend/api/sessions.py`, `sandbox-daemon/internal/ws/server.go`). The **live first execution** is the carryover — same shape as Phase-4's deferred `fly deploy`. Once executed, the captured transcript will be appended to this section in a follow-up commit.

### CI gate

`.github/workflows/frontend.yml`:

```
PR jobs (always):
  - pnpm install --frozen-lockfile
  - pnpm --filter ./frontend lint
  - pnpm --filter ./frontend typecheck
  - pnpm --filter ./frontend build (with placeholder NEXT_PUBLIC_*)
  - pnpm --filter ./frontend test:unit

Opt-in integration gate (vars.RUN_E2E == 'true'):
  - postgres:16-alpine service container
  - actions/setup-go@v5 v1.23 + actions/setup-python@v5 v3.12 + actions/setup-node@v4 (from .nvmrc)
  - proto/codegen/{ts,go,python}.sh
  - poetry install backend deps
  - make -C sandbox-daemon build
  - openssl genpkey ed25519 → ROMMEL_TOKEN_{PRIVKEY,PUBKEY}
  - poetry run alembic upgrade head
  - background: uvicorn (waits for /healthz)
  - background: sandbox-daemon (waits for /healthz)
  - pnpm install --frozen-lockfile
  - playwright install --with-deps chromium
  - background: pnpm --filter ./frontend dev (waits for :3000)
  - pnpm --filter ./frontend test:e2e
  - if: failure() → upload playwright-report artifact
```

Required secrets / vars (set in repo settings before flipping `RUN_E2E`):

| Kind | Name | Source |
|---|---|---|
| secret | `SUPABASE_JWKS_URL` | `https://<project>.supabase.co/auth/v1/.well-known/jwks.json` |
| secret | `SUPABASE_URL` | Supabase project URL |
| secret | `SUPABASE_ANON_KEY` | Supabase anon key |
| secret | `E2E_TEST_EMAIL` | Supabase test user (password auth enabled) |
| secret | `E2E_TEST_PASSWORD` | matching password |
| var | `E2E_WORKSPACE_ID` | seeded workspace uuid (or `dev-workspace`) |
| var | `RUN_E2E` | `"true"` |

### Vercel deploy

```sh
# one-time, in repo root:
vercel link                              # point at the Vercel project; root = frontend/
vercel env add NEXT_PUBLIC_BACKEND_URL production
vercel env add NEXT_PUBLIC_SUPABASE_URL production
vercel env add NEXT_PUBLIC_SUPABASE_ANON_KEY production
# (preview env values for the same three, pointing at staging Supabase)

git push origin some-branch              # → preview deploy URL in PR
git push origin main                     # → prod deploy
curl -fsS https://rommel.vercel.app/     # → 200
```

Deferred: first `vercel link` (no Vercel auth in this session). Mirror of Phase 4 §"Fly deploy" deferral; recipe is recoverable from the dashboard.

---

## Risks the implementation guards against (every plan §4 item revisited)

| # | Risk | Mitigation in shipped code |
|---|---|---|
| 4.1 | Monaco / xterm crash on SSR import | Every editor / terminal component is `"use client"` + `dynamic(() => import("./impl"), { ssr: false })`. The impls (`monaco-impl.tsx`, `xterm-impl.tsx`) hold the library imports; the dynamic boundary keeps them out of the server bundle. |
| 4.2 | `@rommel/proto` ships raw `.ts` | `next.config.mjs::transpilePackages: ["@rommel/proto"]`. |
| 4.3 | 5-min token TTL outlives editor sessions | `DaemonConnection.refreshAndReopen()` triggers on close-1008/4401, server-side `error.code === "invalid_token"`, or a wall-clock timer at `exp - 30s`. The injected `refresh` fn (wired in `useDaemonConnection`) re-runs `POST /sessions`. |
| 4.4 | **Mixed-content `wss://` reachability in prod** | **Not solved in Phase 5.** Dev path uses `ws://localhost`; prod cutover requires the Phase-5.5 Flycast TLS proxy fronting the daemon. **Named in the changelog 0.1.5 next-actions as the load-bearing follow-up.** |
| 4.5 | Supabase service-role key leak into client bundle | Split into `env.client.ts` (NEXT_PUBLIC_*) and `env.server.ts` (`import "server-only"` + zod). ESLint `no-restricted-imports` blocks `**/lib/env.server` from `src/**/*.{ts,tsx}` callers. |
| 4.6 | Hydration mismatches from auth-derived UI on first paint | `Header.tsx` uses `useMe()`'s `me.data?.email ?? me.data?.sub ?? "…"` — the fallback string is the same on first server render (TanStack Query hasn't fetched yet) and after hydration, eliminating the divergence. |
| 4.7 | Monaco worker via CDN can fail behind firewalls | Documented self-host upgrade path in `frontend/README.md`. Deferred per plan. |
| 4.8 | React 19 vs transitive `@types/react@^18` | `package.json` (root) `pnpm.overrides` pins both `@types/react` and `@types/react-dom` to `^19.0.0`. |
| 4.9 | Reconnect drops in-flight `rpc()` promises | `DaemonConnection.close()` calls `rejectInflight(new DaemonClosedError(...))`, which iterates the in-flight map and rejects all pending promises. Asserted in `daemon.test.ts::"rejects in-flight rpc() on close"`. |
| 4.10 | `subscribe()` handlers firing after unmount | `subscribe()` returns an unsubscribe fn; React `useEffect` cleanup is the canonical call site. `connection-store.test.ts` and `daemon.test.ts::"fans out events to subscribers"` together exercise the subscribe→emit→unsubscribe→emit-again-and-don't-call path. |

---

## Out of scope (explicitly deferred — matches plan §6)

- File tree wired to `fs.list` + `fs.watch` — Phase 6.
- Editor tabs, `fs.read` on open, `fs.write` on save, dirty-state, autosave — Phase 6.
- PTY pane wired to `pty.open` + `pty.input` + `pty.output` + `pty.resize` — Phase 6.
- Git panel (`git.*` primitives) — Phase 7+.
- Funnel board (`FunnelBoard.tsx` is a stub) — Phase 6 or 7.
- Command palette — Phase 7+.
- Multi-tab / split editor — Phase N.
- Theming beyond Monaco's `vs-dark` + Tailwind dark — Phase N.
- Backend-issued session cookies — Phase N (current model: Supabase JWT in cookie + short-lived broker token).
- Self-hosted Monaco bundle (risk 4.7) — Phase N.
- **WS production reachability via Flycast `wss://` proxy (risk 4.4) — Phase 5.5, named follow-up.**
- Real Playwright matrix beyond Chromium — Phase N.

---

## Cross-cutting follow-ups (small, do-anywhere)

- **First Vercel link + prod deploy** (needs `vercel auth login` + project setup).
- **First Playwright integration-gate run** in CI (needs Supabase test-user secrets + `vars.RUN_E2E = "true"`).
- **Phase-5.5 wss:// proxy** — backend grows a second listener that terminates TLS and forwards to `<wid>.vm.…internal:7777`, so the browser can open `wss://daemon.<wid>.rommel-backend.fly.dev/ws?token=…`. Out of frontend scope but the changelog flags it.
- **Real auth-bridged session storage** (server-side audit log, refresh-token revocation hooks on `/auth/logout`) — when policy needs it.
- **Replace hand-rolled `types/workspace.ts` with OpenAPI-derived types** once `backend/api/main.py` exposes `/openapi.json` reliably and `openapi-typescript` is wired into `make proto`.

---

## Next

Per [`docs/executing/scaffolding-plan.md`](../executing/scaffolding-plan.md) §6+: the next phase is the first wave of *real* daemon primitives lighting up the IDE shell — either the `.rommel/` funnel UI (Layer 2 of `docs/vision.md`) or the `fs.list`/`fs.read`/`fs.write` wiring that makes the file tree and editor real (Layer 1, the rest of it). Both are unblocked: every primitive the daemon already implements (`fs.read`, `system.ping`, `workspace.info`) is one `DaemonConnection.rpc(type, payload)` call away, and every primitive the daemon stubs (`fs.write`/`fs.list`/`pty.*`) just needs its Go handler filled in. Phase 5 was the inflection point — from here, features ship as additive PRs against a stable substrate.
