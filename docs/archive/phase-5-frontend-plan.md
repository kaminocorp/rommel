# Phase 5 — `frontend/` Implementation Plan

Companion to [`scaffolding-plan.md`](./scaffolding-plan.md) §5. Specializes that section into a step-by-step build order for the **`frontend/`** subtree: the browser IDE shell that consumes the backend's `POST /workspaces/:id/sessions` endpoint, opens a direct WebSocket to the workspace daemon, and renders an editor + terminal against the real wire protocol.

**Status going in:** Phases 0–4 are complete (see [`docs/changelog.md`](../changelog.md)).

- `proto/clients/ts/` ships `@rommel/proto` — generated TS unions for `Envelope`, `SessionTokenClaims`, `fs.*`, `pty.*`, `workspace.*`. Already a pnpm workspace package (root `pnpm-workspace.yaml`).
- `sandbox-daemon` answers `system.ping` and a real `fs.read` over `ws://…/ws?token=…`; the wire format is what the daemon's `internal/ws/server.go` validates frames against.
- `backend/api/sessions.py::create_session` returns `{daemon_url, token, expires_at}`. The first end-to-end browser-to-daemon round-trip is unblocked on the wire side; only the browser is missing.

**Definition of "done" for Phase 5:**

1. `pnpm install && pnpm --filter ./frontend dev` boots Next.js locally on `:3000`. The landing page renders.
2. Supabase auth works end-to-end: sign-in redirects, the session JWT is captured, `Authorization: Bearer <jwt>` is attached to every backend call; a signed-out user is bounced to `/sign-in`.
3. `GET /workspaces/[id]` renders the IDE shell (header, file-tree placeholder, **Monaco editor pane**, **xterm terminal pane**, connection-status pill).
4. **Integration gate:** with a local `sandbox-daemon` running and the backend pointed at it, the frontend calls `POST /workspaces/:id/sessions`, opens the returned WS URL, sends `system.ping`, and renders `ok: true` in the connection-status pill. **Same round-trip the Phase-4 integration gate already proved against a Python client, now driven from the browser.**
5. `vercel --prod` (or push-to-`main` via the Vercel GitHub app) deploys without errors; `https://rommel.vercel.app/` (or equivalent) serves the production build.
6. `.github/workflows/frontend.yml` wakes up and goes green on PR — already gated on `frontend/package.json` existing; ships `pnpm install --frozen-lockfile && pnpm --filter ./frontend lint && build`.

---

## 0. Decisions to settle before any code lands

The hot-path performance choices are already locked (Pattern-B WS directly to the daemon, Monaco for the editor, xterm.js for the terminal). The decisions below are mostly library/policy calls — each one is reversible per-PR, but cheaper to confirm now than to retrofit after the IDE shell has shipped.

### 0.1 Router: Next.js App Router (15.x), React 19

`techstack.md` locks Next.js + Tailwind on Vercel; the App Router is the current default. **Recommendation: Next.js 15 + React 19 (canary stable as of this writing) + App Router.**

- The IDE shell is mostly client components (`"use client"`) — Monaco, xterm, and the WS client all touch `window`. Two server components are useful: the auth-redirect root (`app/layout.tsx`) and the workspace-list landing page (which can RSC-fetch from the backend).
- Keeping App Router means future server actions / streaming SSR are available without a router migration. Pages Router would buy nothing here.

Trade-off considered: **Vite + React SPA, no Next.js.** Faster dev server, smaller dep tree, no SSR foot-guns with Monaco/xterm. Rejected because (a) `techstack.md` locks Next on Vercel and (b) Vercel's RSC + Vercel-managed deploys outweigh the SSR friction we deliberately avoid by `dynamic({ ssr: false })`-loading the editor + terminal.

### 0.2 Auth client: `@supabase/ssr` (server-side cookies), not the plain `@supabase/supabase-js` browser SDK

Supabase ships two SDKs that look similar but behave differently. **Recommendation: `@supabase/ssr` for the cookie-based session, plus the plain client *only* inside `"use client"` components that need to read the user.**

- The backend's auth seam expects `Authorization: Bearer <jwt>`. With `@supabase/ssr`, the session JWT is in an httpOnly cookie that the Next.js middleware can read on every request — no localStorage exposure to XSS, and the cookie is automatically refreshed by Supabase when it nears expiry.
- The frontend's HTTP client (`lib/api.ts`) reads the JWT from the cookie via a server action / route handler proxy *or* exposes it to client code via a session hook from `@supabase/ssr`'s `createBrowserClient`.
- `lib/auth.ts` exposes three factories (per the [Supabase SSR docs](https://supabase.com/docs/guides/auth/server-side/nextjs)):
  - `createBrowserClient()` for client components,
  - `createServerClient(cookies())` for server components / route handlers,
  - `createMiddlewareClient(req)` for `middleware.ts`.

Trade-off considered: **`@supabase/auth-helpers-nextjs`.** Already deprecated; replaced by `@supabase/ssr`. Reject.

Trade-off considered: **Clerk.** Better DX for the auth flows, but `techstack.md` locks Supabase and the backend already validates Supabase JWKS (`backend/services/auth.py`). Switching means rewriting the backend's auth seam — not Phase-5 scope.

### 0.3 Editor: `@monaco-editor/react` (loader-managed Monaco), dynamic-imported with `ssr: false`

**Recommendation: `@monaco-editor/react` v4.x.**

- Wraps `monaco-editor` with a React-friendly API and a CDN loader (default: jsDelivr). No webpack/SWC custom config required — which is the load-bearing reason; bundling Monaco directly through Next.js has bitten Codespaces/Gitpod-style projects more than once.
- Risk-side: depending on a CDN-fetched Monaco means the editor is unavailable if jsDelivr is firewalled. Mitigation (Phase-N): self-host the Monaco files under `public/monaco/` and configure `loader.config({ paths: { vs: "/monaco/vs" } })`. Out of scope for v1, but document the upgrade path in `frontend/README.md`.
- Every `EditorPane` component is `"use client"` AND imported via `dynamic(() => import("…"), { ssr: false })` — Monaco touches `window` on its very first module evaluation.

Trade-off considered: **raw `monaco-editor` + a hand-rolled React wrapper.** More control, but `@monaco-editor/react` is the standard for a reason; v1 doesn't need anything it can't do.

Trade-off considered: **CodeMirror 6.** Smaller bundle, friendlier to mobile. Rejected because every other IDE-shell in the reference set (`docs/refs/oss-refs.md` likely lists them) uses Monaco; consistency with how engineers expect "the IDE editor" to behave is worth the bundle weight.

### 0.4 Terminal: `@xterm/xterm` v5 + `@xterm/addon-fit` + `@xterm/addon-web-links`

**Recommendation: the official `@xterm/*` scoped packages (xterm v5 series).**

- Same constraints as Monaco: `dynamic({ ssr: false })`, `"use client"`.
- `addon-fit` resizes the terminal to its container; a `ResizeObserver` triggers `pty.resize` on the daemon.
- `addon-web-links` is a one-line UX win — terminal output URLs become clickable.
- Future Phase-N: `@xterm/addon-canvas` if the default DOM renderer chokes; `@xterm/addon-search` for `Ctrl+F`.

Trade-off considered: **Hyperterm-style WebGL renderer (`@xterm/addon-webgl`).** Faster for high-throughput output (`yarn build` logs, etc.). Rejected for v1 — the DOM renderer is plenty for `system.ping` round-trips; revisit when real PTY traffic lands.

### 0.5 WS client: hand-rolled wrapper around the native `WebSocket`, NOT a library

**Recommendation: a `lib/daemon.ts` wrapper that owns:**

- Connection lifecycle (open → authenticated → ready → closed/failed).
- Envelope encode/decode against `@rommel/proto`'s `Envelope` type.
- Request/response correlation via `id` (a per-conn counter or `crypto.randomUUID()`).
- Exponential-backoff reconnect (initial 250ms, cap 5s, abort after 5 attempts).
- Token-expiry refresh: on a 401 close code OR a server-side `error.code === "invalid_token"` envelope OR a wall-clock check (`expires_at - now < 30s`), re-call `POST /workspaces/:id/sessions` and re-open the WS, replaying in-flight subscribers.

The wrapper exports an `RPC<Req, Res>(type, payload) => Promise<Res>` and a `subscribe<Event>(type, handler) => unsubscribe` API — frontend code never touches the raw socket.

Trade-off considered: **`partysocket`** (reconnecting WS). Solves reconnect but doesn't help with envelope decoding / `id` correlation; we'd still own most of the wrapper. Rejected — one more dep, almost no leverage.

Trade-off considered: **`socket.io` / `nanostream` / similar.** Add framing on top of WS that the daemon doesn't speak. Reject — the daemon's wire format is JSON envelopes per `proto/schemas/envelope.json`; we don't get to redefine it.

### 0.6 Data layer: TanStack Query for HTTP, Zustand for client-only state, no Redux

**Recommendation:**

- **TanStack Query v5** for every call to the backend (`/workspaces`, `/auth/me`, `/workspaces/:id/sessions`). Gives us cache, retries, stale-while-revalidate, and `useQuery`/`useMutation` ergonomics. Pairs cleanly with Server Components by hydrating from `dehydrate(queryClient)` at the boundary.
- **Zustand** for the client-only state the IDE needs: open tabs, active editor, WS connection status. Stable, tiny, no provider tree.
- **Jotai / Recoil**: rejected. Atoms-by-default makes sense for fine-grained UI state; Zustand is closer to how the daemon connection actually behaves (one big object).
- **Redux Toolkit**: rejected. Overkill for v1 — the IDE shell doesn't have action-replay needs.

Trade-off considered: **SWR.** Sibling library, same authors as Next.js; fine choice. TanStack Query has the slightly nicer mutation API and the WS-event-driven `queryClient.setQueryData` pattern is well-documented — useful because `fs.watch` events will eventually invalidate file tree queries.

### 0.7 UI primitives: Tailwind + Shadcn/ui (Radix under the hood)

**Recommendation: Tailwind 4 + `shadcn-ui` for the half-dozen components we actually need (Button, Dialog, DropdownMenu, Tooltip, Toast, Tabs).**

- Shadcn pastes copy-owned source into `src/components/ui/` — no runtime dep, but you get accessible Radix primitives.
- Lucide-react for icons (peer dep of Shadcn anyway).
- **No** Material UI, Chakra, Mantine — those are component-system commitments; the IDE shell is mostly bespoke.

Trade-off considered: **Headless UI + hand-rolled Tailwind components.** A bit less starter code, but Shadcn's accessibility audit pays for itself even for the toast/dropdown/dialog primitives.

### 0.8 Deployment: Vercel, project root = `frontend/`, env via Vercel dashboard

- Vercel project pointed at `frontend/` as the root, `pnpm install` + `pnpm build` as the standard commands.
- Env vars set in the dashboard (or via `vercel env`):
  - `NEXT_PUBLIC_BACKEND_URL` — `https://rommel-backend.fly.dev`.
  - `NEXT_PUBLIC_SUPABASE_URL` + `NEXT_PUBLIC_SUPABASE_ANON_KEY` — public, fine to expose.
  - `SUPABASE_SERVICE_ROLE_KEY` — **never** in `NEXT_PUBLIC_*`. Server-only.
- Preview deploys hit a preview Supabase project + the prod backend URL (or a staging backend in a follow-up).

Trade-off considered: **Self-host on Fly alongside the backend.** Defeats the purpose of techstack-locked Vercel; loses preview deploys and the App Router's edge-streaming.

### 0.9 Testing: Vitest for units, Playwright for one smoke test

- **Vitest** for unit tests (`lib/daemon.ts` envelope encode/decode, reducer-style state). Vitest is the de-facto Next/React testing default; Jest's CommonJS legacy hurts in App Router.
- **Playwright** for *one* smoke test that boots the dev server, signs in via a seeded Supabase user, navigates to a workspace page, and asserts the connection-status pill turns green. CI runs against a daemon subprocess + the local backend.
- **React Testing Library** for component-level tests when a component has non-trivial behavior; not aggressively pursued in v1.

Trade-off considered: **Cypress instead of Playwright.** Playwright's CDP-level control is more reliable for WS-driven UI; reject Cypress.

Trade-off considered: **No browser tests in v1.** Possible, but the integration-gate value of "the IDE shell actually round-trips a ping" is worth the one Playwright spec.

### 0.10 TypeScript strict mode, ESLint flat config, Prettier via Tailwind plugin

- `tsconfig.json` extends `"strict": true`, `"noUncheckedIndexedAccess": true`, `"exactOptionalPropertyTypes": true`. Matches the strictness `@rommel/proto`'s generated unions expect.
- ESLint flat config (`eslint.config.mjs`) with `@eslint/js`, `eslint-plugin-react`, `eslint-plugin-react-hooks`, `eslint-plugin-jsx-a11y`, and `@typescript-eslint/eslint-plugin`. **No** `next lint` — Next 15 deprecates it in favor of flat config.
- Prettier with `prettier-plugin-tailwindcss` so class-name ordering is stable in PRs.

### 0.11 Out-of-scope decisions deliberately deferred to Phase 5+

The funnel board, real file tree, PTY pane wiring, command palette — all UI-layer features that depend on daemon primitives beyond `system.ping` and `fs.read`. Phase 5's scope is the *substrate* (auth, sessions, two empty panes, a working WS pipe). Per `scaffolding-plan.md` §5 "Done when": the round-trip works; everything else is additive.

---

## 1. Files to create

```
frontend/
├── README.md
├── package.json                       # @rommel/frontend; deps: next, react, react-dom,
│                                      #   @rommel/proto (workspace), @supabase/ssr,
│                                      #   @supabase/supabase-js, @tanstack/react-query,
│                                      #   zustand, @monaco-editor/react, @xterm/xterm,
│                                      #   @xterm/addon-fit, @xterm/addon-web-links,
│                                      #   tailwindcss, clsx, tailwind-merge, lucide-react,
│                                      #   @radix-ui/react-* (per shadcn), zod
│                                      # devDeps: typescript, eslint (flat config),
│                                      #   prettier, prettier-plugin-tailwindcss, vitest,
│                                      #   @vitejs/plugin-react, @testing-library/react,
│                                      #   @playwright/test, @types/*
├── tsconfig.json                      # strict mode + paths: "@/*" -> "src/*"
├── next.config.mjs                    # transpile @rommel/proto, experimental.serverActions if used
├── tailwind.config.ts                 # content globs, container, dark-mode = class
├── postcss.config.mjs                 # tailwindcss + autoprefixer
├── eslint.config.mjs                  # flat config; rules per §0.10
├── .prettierrc.json                   # tailwind plugin; trailingComma = "all"
├── .env.example                       # every NEXT_PUBLIC_* + server-only var, documented
├── .gitignore                         # .next/, .vercel/, node_modules/, .env.local
├── vercel.json                        # build & install commands; framework detected automatically
├── playwright.config.ts               # baseURL = http://localhost:3000; one project (chromium)
├── vitest.config.ts                   # jsdom env, alias @ -> src
├── middleware.ts                      # bouncer for unauthenticated /workspaces/* requests
├── public/
│   └── favicon.svg
├── src/
│   ├── app/
│   │   ├── layout.tsx                 # root layout, QueryClientProvider, ThemeProvider
│   │   ├── page.tsx                   # landing: workspace picker (RSC fetch on the server)
│   │   ├── sign-in/
│   │   │   └── page.tsx               # Supabase magic-link / OAuth login
│   │   ├── auth/
│   │   │   └── callback/route.ts      # OAuth code-exchange handler
│   │   └── workspaces/
│   │       └── [id]/
│   │           ├── page.tsx           # workspace shell (RSC: fetches workspace metadata)
│   │           └── workspace-client.tsx  # client island: editor + terminal + WS
│   ├── components/
│   │   ├── ui/                        # shadcn-paste land (button, dialog, dropdown, …)
│   │   ├── shell/
│   │   │   ├── Header.tsx             # workspace name, user menu, status pill
│   │   │   ├── StatusBar.tsx
│   │   │   └── ConnectionPill.tsx     # subscribes to daemon connection state
│   │   ├── filetree/
│   │   │   └── FileTree.tsx           # stub list, no fs.list wiring in v1
│   │   ├── editor/
│   │   │   └── EditorPane.tsx         # dynamic(() => import("./monaco-impl"), { ssr: false })
│   │   ├── terminal/
│   │   │   └── TerminalPane.tsx       # dynamic xterm, fit-addon, web-links addon
│   │   └── funnel/
│   │       └── FunnelBoard.tsx        # placeholder, .rommel/ board is Phase-6
│   ├── lib/
│   │   ├── api.ts                     # typed HTTP client (TanStack-Query-ready)
│   │   ├── daemon.ts                  # WebSocket wrapper (envelope, rpc, subscribe, reconnect)
│   │   ├── auth.ts                    # supabase client factories (browser/server/middleware)
│   │   ├── env.ts                     # zod-validated process.env
│   │   ├── query.ts                   # QueryClient factory (dehydrate/hydrate at RSC boundary)
│   │   └── utils.ts                   # cn(), invariant(), etc.
│   ├── hooks/
│   │   ├── useDaemonConnection.ts     # opens session, mounts daemon.ts, exposes status
│   │   └── useWorkspace.ts            # TanStack-Query wrapper for /workspaces/:id
│   ├── stores/
│   │   └── connection.ts              # Zustand store: WS state, current session, latest error
│   ├── types/
│   │   └── workspace.ts               # OpenAPI-derived or hand-rolled
│   └── styles/
│       └── globals.css                # tailwind base/components/utilities + IDE shell theme
└── tests/
    ├── unit/
    │   ├── daemon.test.ts             # envelope encode/decode, rpc correlation
    │   ├── connection-store.test.ts
    │   └── auth.test.ts
    └── e2e/
        └── ping.spec.ts               # ★ Playwright smoke: sign-in → /workspaces/dev → green pill
```

---

## 2. Step-by-step implementation

### Step 1 — Subtree skeleton + Tailwind + envar zod (PR-1)

`pnpm init` (or copy from the techstack's Next 15 starter), `pnpm install next@15 react@19 react-dom@19 tailwindcss@4 …`. Configure App Router defaults (`src/app/layout.tsx`, `src/app/page.tsx`). Tailwind 4's `@import "tailwindcss"` in `globals.css` replaces the v3 directives.

`lib/env.ts`:

```ts
import { z } from "zod";

const ServerEnv = z.object({
  NEXT_PUBLIC_BACKEND_URL: z.string().url(),
  NEXT_PUBLIC_SUPABASE_URL: z.string().url(),
  NEXT_PUBLIC_SUPABASE_ANON_KEY: z.string().min(1),
  SUPABASE_SERVICE_ROLE_KEY: z.string().min(1).optional(),  // only on server
});

export const env = ServerEnv.parse(process.env);
```

Boot smoke: `pnpm --filter ./frontend dev` → `http://localhost:3000` shows a placeholder home page.

CI gate: `frontend/package.json` exists → `.github/workflows/frontend.yml` wakes up and runs `pnpm install --frozen-lockfile`, `pnpm --filter ./frontend lint`, `pnpm --filter ./frontend build`.

### Step 2 — Supabase SSR auth + middleware (PR-2)

Install `@supabase/ssr` + `@supabase/supabase-js`. `lib/auth.ts` exports the three client factories. `middleware.ts` runs on `/workspaces/:path*` and `/api/:path*` — if the SSR client can't read a session, redirect to `/sign-in?next=<url>`.

`/sign-in/page.tsx` renders the Supabase magic-link form (one input, one button); `/auth/callback/route.ts` is the OAuth/magic-link code-exchange handler (`createServerClient` → `auth.exchangeCodeForSession(code)` → redirect).

Smoke: visit `/workspaces/anything` while signed out → bounced to `/sign-in`. Sign in via magic link → redirected back. `GET /auth/me` is reachable from the browser via a client-side fetch that includes the cookie-derived JWT in `Authorization`.

### Step 3 — HTTP client + workspace landing page (PR-3)

`lib/api.ts`:

```ts
export async function api<T>(
  path: string,
  init: RequestInit & { token?: string } = {},
): Promise<T> {
  const { token, headers, ...rest } = init;
  const res = await fetch(`${env.NEXT_PUBLIC_BACKEND_URL}${path}`, {
    ...rest,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...headers,
    },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<T>;
}
```

Wrap in TanStack-Query hooks (`useWorkspaces()`, `useWorkspace(id)`, `useCreateSession(id)`). The landing page (`src/app/page.tsx`) is a server component that calls `api("/workspaces", { token: <jwt from cookies()> })` and lists the user's workspaces. Each card links to `/workspaces/[id]`.

Empty-state UX: if the user has zero workspaces, render a "Create workspace" CTA that POSTs `/workspaces` with `{ name: "untitled" }`.

### Step 4 — WS client (`lib/daemon.ts`) + connection store (PR-4)

The load-bearing piece. The wrapper is a class because long-lived state (reconnection back-off, in-flight requests) reads cleaner that way:

```ts
import type { Envelope } from "@rommel/proto/envelope";

export class DaemonConnection {
  private socket: WebSocket | null = null;
  private inflight = new Map<string, { resolve: (v: unknown) => void; reject: (e: unknown) => void }>();
  private subscribers = new Map<string, Set<(payload: unknown) => void>>();
  private nextId = 1;

  constructor(
    private readonly url: string,
    private readonly token: string,
    private readonly onStatusChange: (s: ConnectionStatus) => void,
  ) {}

  async connect(): Promise<void> { /* exponential back-off, ws onopen sets status=ready */ }
  async rpc<TReq, TRes>(type: string, payload: TReq): Promise<TRes> { /* id correlation */ }
  subscribe<TEvent>(type: string, handler: (e: TEvent) => void): () => void { /* fan-out events */ }
  close(): void { /* graceful */ }
}
```

`stores/connection.ts` (Zustand) holds `{ status, sessionId, lastError }`. `hooks/useDaemonConnection.ts` is the integration point:

```ts
export function useDaemonConnection(workspaceId: string) {
  const { mutateAsync: createSession } = useCreateSession(workspaceId);
  const store = useConnectionStore();

  useEffect(() => {
    let conn: DaemonConnection | null = null;
    (async () => {
      const { daemon_url, token } = await createSession();
      conn = new DaemonConnection(`${daemon_url}?token=${token}`, token, store.setStatus);
      await conn.connect();
      const pong = await conn.rpc("system.ping", {});
      store.setLastPong(pong);
    })();
    return () => conn?.close();
  }, [workspaceId]);
}
```

Unit tests (`tests/unit/daemon.test.ts`): mock `WebSocket` with a tiny in-test echo, assert that `rpc("system.ping")` resolves with the matching response, that the request `id` round-trips, and that `subscribe("pty.output")` receives a server-pushed event.

### Step 5 — Workspace shell page: editor + terminal + status pill (PR-5)

`src/app/workspaces/[id]/page.tsx` is a thin server component that fetches workspace metadata and renders `<WorkspaceClient workspace={…} />`. The client component sets up the three panes in a CSS grid:

```
┌────────────────┬──────────────────────────┐
│  FileTree      │   EditorPane (Monaco)    │
│  (stub)        │                          │
│                ├──────────────────────────┤
│                │   TerminalPane (xterm)   │
└────────────────┴──────────────────────────┘
ConnectionPill: ●  connected · session 0a1b2c…
```

- `EditorPane.tsx`: `dynamic(() => import("./monaco-impl"), { ssr: false, loading: () => <Skeleton/> })`. The impl mounts `<MonacoEditor language="markdown" value="" />` — no file wiring in v1.
- `TerminalPane.tsx`: same dynamic pattern. Loads `@xterm/xterm` + `@xterm/addon-fit` + `@xterm/addon-web-links`. A `ResizeObserver` calls `fit.fit()` on container resize. No PTY wiring yet — the terminal renders a `Welcome to Rommel.` banner.
- `ConnectionPill.tsx`: subscribes to `useConnectionStore`, renders the four-state pill (`connecting | ready | reconnecting | failed`).

Both panes are inert in v1 — they're proving the bundling story works (Monaco + xterm both load, neither blows up SSR). Real wiring lands in Phase 6+.

### Step 6 — Playwright smoke + frontend.yml gate (PR-6)

`tests/e2e/ping.spec.ts`:

```ts
test("integration: sign-in → workspace page → connection-pill ready", async ({ page }) => {
  await page.goto("/");
  await signInAsTestUser(page);  // helper: programmatic Supabase password sign-in
  await page.goto("/workspaces/dev-workspace");
  await expect(page.locator("[data-testid=connection-pill]")).toHaveText(/ready/);
});
```

The Playwright config spins up a per-job daemon subprocess (mirroring the backend's pytest fixture in `backend/tests/conftest.py`) and a per-job backend (`make -C backend run` in the background). CI's `frontend.yml` gets:

- A `services: postgres` block (RLS again).
- A "Build sandbox-daemon" step (Go 1.23, `make -C sandbox-daemon build`).
- A "Run backend" step (`poetry run uvicorn` in the background; waits for `/healthz`).
- A "Run daemon" step (`./sandbox-daemon/dist/sandbox-daemon` in the background; waits for `/healthz`).
- A "Playwright install browsers" step (`pnpm exec playwright install --with-deps chromium`).
- The Playwright run itself.

This is the **Phase-5 integration gate**: the full Pattern-B loop (browser → backend `/sessions` → daemon WS) running in one CI job. If it passes, Phase 5 has functionally landed.

### Step 7 — Vercel project setup + first prod deploy (PR-7)

- Create the Vercel project pointing at `frontend/` as the root directory; framework auto-detects Next.js.
- Set env vars in the Vercel dashboard (the four `NEXT_PUBLIC_*` + the optional service role).
- `pnpm` is detected from `packageManager` in the root `package.json`.
- Push to `main` triggers deploy; PR commits trigger preview deploys.
- Smoke: `curl -fsS https://rommel.vercel.app/` (or wherever the project lands) returns 200. Sign in via a real Supabase project. Open a workspace page. Connection pill turns green.

PR-7's PR description includes the live URL as the gate.

---

## 3. Verification recipe

### 3.1 Local boot

```sh
cd frontend
pnpm install                                # at repo root, hydrates the workspace
pnpm --filter ./frontend dev                # → http://localhost:3000
```

Visit `http://localhost:3000` — landing page renders.

### 3.2 Auth flow

```sh
# In .env.local:
NEXT_PUBLIC_BACKEND_URL=http://localhost:8080
NEXT_PUBLIC_SUPABASE_URL=https://<project>.supabase.co
NEXT_PUBLIC_SUPABASE_ANON_KEY=<anon-key>
```

Visit `/workspaces/anything` while signed out → bounced to `/sign-in`. Magic-link sign-in → redirected back. Header shows the user email.

### 3.3 The integration gate — browser signs, daemon accepts

```sh
# Terminal 1: daemon
openssl genpkey -algorithm ed25519 -out /tmp/dev.pem
openssl pkey -in /tmp/dev.pem -pubout -out /tmp/dev.pub
ROMMEL_TOKEN_PUBKEY="$(cat /tmp/dev.pub)" \
  ROMMEL_WORKSPACE_ROOT="$PWD" \
  ROMMEL_WID="dev-workspace" \
  make -C sandbox-daemon run-local

# Terminal 2: backend
docker compose -f backend/compose.yaml up -d postgres
ROMMEL_TOKEN_PRIVKEY="$(cat /tmp/dev.pem)" \
  ROMMEL_DAEMON_URL_TEMPLATE="ws://localhost:7777/ws" \
  ROMMEL_DATABASE_URL=postgresql+asyncpg://rommel:rommel@localhost:5432/rommel \
  ROMMEL_DATABASE_MIGRATE_URL=postgresql://rommel:rommel@localhost:5432/rommel \
  ROMMEL_SUPABASE_JWKS_URL=https://<project>.supabase.co/auth/v1/.well-known/jwks.json \
  make -C backend migrate run

# Terminal 3: frontend
pnpm --filter ./frontend dev

# Browser:
#   1. Open http://localhost:3000
#   2. Sign in
#   3. Click "Create workspace" → it appears with id <uuid>
#   4. Open it
#   5. Connection pill turns green; the WS frame for system.ping resolved
```

DevTools network tab confirms the WS upgrade went directly to `ws://localhost:7777/ws?token=…` — Vercel/Next.js is not in the data path. This is the same property the Phase-4 integration gate proved with a Python WS client; now it's proven with a Chromium WS client.

### 3.4 Vercel deploy

```sh
git push origin some-branch                 # → preview deploy URL in PR
git push origin main                        # → prod deploy
curl -fsS https://rommel.vercel.app/        # → 200
```

### 3.5 CI gate

`.github/workflows/frontend.yml` runs:

```
- pnpm install --frozen-lockfile
- pnpm --filter ./frontend lint
- pnpm --filter ./frontend build
- pnpm --filter ./frontend test:unit       # Vitest
- (PR-6+) pnpm --filter ./frontend test:e2e  # Playwright + backend + daemon subprocesses
```

All steps green.

---

## 4. Risks and gotchas

### 4.1 SSR + `window` access — Monaco and xterm both crash on import during SSR

Both libraries reference `window` / `document` at module-evaluation time. Any component that statically imports them will break `next build` with `ReferenceError: window is not defined`.

**Fix:** every editor / terminal component is `"use client"` AND mounted via `dynamic(() => import("./impl"), { ssr: false })`. The impl modules contain the actual library imports; the dynamic boundary keeps them out of the server bundle.

Codified in `EditorPane.tsx` and `TerminalPane.tsx` as the structural pattern; any future client-only dep should follow it.

### 4.2 `next.config.mjs` must transpile `@rommel/proto`

The pnpm workspace publishes `@rommel/proto` as raw `.ts`/`.tsx` (see `proto/clients/ts/package.json`: `"main": "src/index.ts"`). Next's default build doesn't transpile node_modules.

**Fix:**

```js
// next.config.mjs
export default {
  transpilePackages: ["@rommel/proto"],
};
```

Without this, `next build` errors with "Module parse failed: Unexpected token". The TS proto client could ship pre-compiled JS, but that means a second build step in `make proto`; transpilation is the cheaper move.

### 4.3 Token TTL is 5 minutes; long-running editor sessions outlive it

The backend's `mint_token` defaults to 300 s; a user staring at the editor for 6 minutes will see the WS close with code 1008 (policy violation) when the daemon's exp check fires.

**Fix:** `lib/daemon.ts` handles the close-1008 OR a wall-clock check (`expires_at - now < 30s`) by calling `POST /workspaces/:id/sessions` again, re-opening the WS, and **replaying in-flight subscribers** (e.g. `fs.watch` subscriptions). This is the half of risk 4.6 from the Phase-4 plan that the frontend owns.

Document the convention in `frontend/README.md` and in a comment on `DaemonConnection.refresh()`.

### 4.4 Mixed-content: `https://rommel.vercel.app` can't open `ws://localhost:7777`

Production Vercel is served over HTTPS; opening a plain `ws://` from there gets blocked by the browser as mixed content. In production, the daemon is reached at `wss://<wid>.vm.rommel-workspaces.internal:7777/ws` — but `.internal` DNS is Fly-private; the browser can't resolve it.

This is **the** architectural cliff for Phase 5+. The plan to traverse it:

1. **Phase 5 v1:** in dev (`http://localhost:3000`), `ws://localhost:7777` works because both are http. The dev story is complete.
2. **Phase 5.5 (small follow-up PR):** the backend exposes a Flycast `wss://` proxy in front of the workspace daemon — i.e. the backend's fly app gets a *second* listener on a dedicated port that terminates TLS and forwards to `<wid>.vm.…internal:7777`. Browser opens `wss://daemon.<wid>.rommel-backend.fly.dev/ws?token=…`; the proxy is transparent. **Flagged here so the prod cutover doesn't surprise anyone — it's not Phase-5 scope to build.**
3. Alternatively, **Fly anycast machines with public IPs per workspace** (heavier ops). Defer.

Until the proxy lands, prod deploys can still hit the local-dev daemon over `ws://` — but only when the user is running the workspace locally, which is not the production story. The first PR-7 deploy will live with this gap; the completion doc should call it out as the load-bearing follow-up.

### 4.5 Supabase service-role key must never reach the browser

Easy to leak: any `NEXT_PUBLIC_*` env is baked into the client bundle. Naming `SUPABASE_SERVICE_ROLE_KEY` *without* a `NEXT_PUBLIC_` prefix keeps it server-only — but only if no code imports it from a `"use client"` file. `lib/env.ts`'s zod schema marks server-only keys as `.optional()` so the browser bundle doesn't crash if they're absent (rather than failing-fast on the server, which is the catch).

**Fix:** split `lib/env.ts` into `env.client.ts` (parses only `NEXT_PUBLIC_*`) and `env.server.ts` (parses everything else, guarded by a `"server-only"` import). ESLint rule `import/no-restricted-paths` prevents client code from importing `env.server.ts`.

### 4.6 Hydration mismatches from theme / auth-state on the first paint

If the auth-derived UI (header user menu) renders differently on server vs client, React 19 emits a hydration mismatch warning and bails to client-render. The fix is to gate user-derived UI behind a `<Suspense fallback={...}>` boundary and have the server render the fallback unconditionally.

### 4.7 Monaco worker loading via `@monaco-editor/react`'s CDN loader

`@monaco-editor/react` defaults to fetching Monaco from `https://cdn.jsdelivr.net/...` and spinning up its workers. Three failure modes:

- Corporate firewalls that block jsDelivr → editor is a permanent loading spinner.
- Vercel preview deploys hitting jsDelivr unauthenticated → rare 429s.
- CSP headers forbidding cross-origin workers → silent failure.

**Mitigation v1:** document the issue + the self-host upgrade path (`public/monaco/` + `loader.config({ paths: { vs: "/monaco/vs" } })`). **Don't** bake self-host into Phase 5 — bundling Monaco's workers through Next.js's webpack/Turbopack is its own minefield; doing it later as a one-PR change is the right ordering.

### 4.8 React 19 + `@types/react` 18 mismatch via transitive deps

`@monaco-editor/react` (and many other libs) often pin `peerDeps` to `@types/react@^18`. With React 19 installed, pnpm prints peer-dep warnings; some libs typecheck-fail on `JSX.Element` deprecation.

**Fix:** `pnpm.overrides` in the root `package.json`:

```json
"pnpm": { "overrides": { "@types/react": "^19", "@types/react-dom": "^19" } }
```

Verify with `pnpm dlx are-the-types-wrong @rommel/frontend` in CI before relying on it.

### 4.9 WebSocket reconnect can drop in-flight `rpc()` promises

If the daemon goes away mid-rpc, `inflight.get(id)` is stuck. The `DaemonConnection` must reject all in-flight promises on the `close` event with a typed `DaemonClosedError`. The hooks layer's `useEffect` cleanup propagates this through the React tree; TanStack-Query mutations land in `onError` and the UI shows a "Reconnecting…" toast.

### 4.10 `fs.watch` events arriving after `connection.close()`

If we add `fs.watch` in Phase 6, subscribers must be torn down on close — otherwise React state updates fire against unmounted components and the dev-mode StrictMode double-mount surfaces warnings. The connection wrapper's `subscribe()` returns an unsubscribe fn that handlers store in their `useEffect` cleanup; the close path also flushes the subscriber map. Codify this discipline in `lib/daemon.ts`'s tests.

---

## 5. Sequencing (suggested)

Per-PR carve-up — each independently revertable, each leaves CI green:

1. **PR-1 — Skeleton + Tailwind + zod env.** Next.js boots locally; CI's `frontend.yml` wakes up.
2. **PR-2 — Supabase SSR auth + middleware.** Sign-in works; protected routes redirect.
3. **PR-3 — HTTP client + landing page + workspace CRUD UI.** Workspaces list + create + open.
4. **PR-4 — `lib/daemon.ts` + connection store + unit tests.** Hermetic WS contract tests pass in Vitest with a mocked WebSocket. The wrapper API is locked.
5. **PR-5 — Workspace page: editor + terminal + connection pill.** Both panes mount via dynamic import; no SSR error; pill subscribes to the store.
6. **PR-6 — Playwright integration gate + CI extension.** The full Pattern-B loop runs in CI. **Phase 5 functionally complete.**
7. **PR-7 — Vercel project setup + first prod deploy.** Live URL in the PR description as the gate.

PR-6 is the "Phase 5 done" milestone (matches the function of PR-4 in the Phase-4 plan). PR-7 is the deployability proof.

A possible **PR-5.5** (or saved for Phase-5.5 follow-up) is the `wss://` proxy / public-daemon-URL story (risk 4.4) — but the cleanest framing is a separate phase since it changes the workspace-image's reachability model.

---

## 6. Out of scope (deferred to later phases)

- **File tree wired to `fs.list` + `fs.watch`.** Phase 6.
- **Real editor wiring: tabs, `fs.read` on open, `fs.write` on save, dirty-state, autosave.** Phase 6.
- **PTY pane wired to `pty.open` + `pty.input` + `pty.output`.** Phase 6.
- **Git panel (`git.*` primitives).** Phase 7+.
- **Funnel board (`FunnelBoard.tsx` is a stub; the `.rommel/` UI is Phase 6 or 7 depending on the funnel primitive design).**
- **Command palette.** Phase 7+.
- **Multi-tab / split editor.** Phase N.
- **Theming beyond Monaco defaults + Tailwind dark mode.** Phase N.
- **Real Vercel auth → backend session bridging** beyond passing the JWT as a Bearer header. (E.g. backend-side session storage, server-issued cookies.) Phase N.
- **Self-hosted Monaco bundle** (risk 4.7). Phase N.
- **WS production reachability via Flycast/`wss://` proxy** (risk 4.4). **Flagged as the load-bearing prod-cutover follow-up.**
- **Real Playwright matrix beyond Chromium.** Phase N.

---

## 7. Completion doc target

When Phase 5 lands, write `docs/completions/phase-5-frontend.md` mirroring the structure of `phase-4-backend.md`:

- **What was built** — file tree + summary.
- **Decisions made** — every 0.X above, marked confirmed/revised.
- **Verification** — copy of §3 with the actual integration-gate transcript (the Chromium WS round-trip showing the daemon accepting a token the *browser* triggered the backend to sign). Include a screenshot or a saved Playwright trace if practical.
- **Cross-cutting** — note that the full Pattern-B loop (Phase 1 contract → Phase 2 verifier → Phase 3 image → Phase 4 signer → Phase 5 browser client) is now operational end-to-end in dev. Production reachability (risk 4.4) is the named follow-up.
- **What's next** — depending on user priority: either the funnel board + planning UI (`.rommel/`), or wiring the existing daemon primitives (`fs.read`, `pty.open`) into real editor/terminal behavior. Scaffolding plan §6+ has the broad shape; the Phase-5 completion doc proposes a concrete next plan.

Update `docs/changelog.md` with the `0.1.5` entry pointing at the completion doc, and call out the Phase-5.5 follow-up (production WS reachability) as the load-bearing item on the next-actions list.

---

## Appendix A — Why Phase 5 is the inflection point for the project

Phases 0–4 built infrastructure: a contract, a verifier, an image, a signer. None of them produce a user-visible experience on their own; each one was a brick in a wall the user can't yet see.

Phase 5 is the first phase whose output is a *thing the user logs into and clicks around in*. Every subsequent phase adds verbs to that thing — `fs.list` makes the file tree real, `pty.open` makes the terminal real, `funnel.promote` makes the kanban real. The substrate that Phase 5 ships (one Vercel app, one auth seam, one WS wrapper, one Monaco shell, one xterm shell, one connection-status pill) is the canvas every future phase paints on.

That framing also fixes the temptation: the *only* additional UX in Phase 5 is the workspace picker, sign-in, and connection-status pill. Everything that looks like an IDE feature (open file, run command, render git status) is explicitly Phase 6+. Phase 5's gate is the round-trip — once it's green, the rate of feature delivery accelerates dramatically because the loop closes.

---

## Appendix B — Reference: the protocol surface this phase needs to speak

For the v1 round-trip:

- **Outbound** (browser → daemon, via `lib/daemon.ts`):
  - `{ "kind": "request", "type": "system.ping", "id": "<uuid>", "payload": {} }`
- **Inbound** (daemon → browser):
  - `{ "kind": "response", "type": "system.ping", "id": "<uuid>", "payload": { "ok": true, "ts": "<RFC3339Nano>" } }`
  - `{ "kind": "error", "type": "system.ping", "id": "<uuid>", "error": { "code": "...", "message": "..." } }`

Phase 5 doesn't need to send anything more than that. The wrapper API (`rpc`, `subscribe`) is general enough to handle every future primitive in `primitives.md` §1 — the v1 round-trip just proves the plumbing works.

This is intentional. The whole purpose of Phase 5 is to make the Phase-4-proven loop *user-driven* instead of test-driven. Every primitive after that is a small additive PR against the wrapper, not a re-architecture.
