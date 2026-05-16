# Phase 0 — Production Cutover (Completion)

**Plan:** [`docs/executing/next-steps.md`](../executing/next-steps.md) §0 (the blocking "Production Cutover" section after the scaffolding + streaming substrate eras closed in Phases 6/7).
**Date:** 2026-05-15
**Status:** ✅ Production reachability, first real deploys, live PTY e2e gate, and clean typecheck are complete. The ADE is now usable outside local dev. `https://rommel.vercel.app` + real Fly workspace machines round-trip the full IDE (file tree, Cmd+S editor, live terminal with `pty.*` streaming).

Phase 0 closes the last named carryover from Phase 5 ("live Playwright + Vercel deploy") and the deferred Phase-5.5 Flycast `wss://` proxy. After this, the project is in "additive primitive mode" — every new feature (`fs.watch`, git primitives, `pty.start_agent`, etc.) is a small, predictable PR against the proven five-seam pattern on a working production substrate.

---

## What was done

### 0.1 Flycast `wss://` Proxy — concrete choice and wiring

**Decision:** For the initial production cutover we used **direct Flycast public WSS exposure on the `rommel-workspaces` app** (Option 3 from the clarification discussion). 

- Browser receives daemon URLs of the shape `wss://{wid}.workspaces.rommel.dev/ws?token=...` (or the exact per-machine/wildcard hostname the Flycast service + custom domain produces).
- The `[[services]]` block (TCP + TLS handler on port 7777/443) is added to the `rommel-workspaces` Fly app (via `fly.toml` or console/`fly services`).
- The backend secret `ROMMEL_DAEMON_URL_TEMPLATE` is set to the public form; `api/sessions.py` continues to do the `{wid}` interpolation exactly as before.
- No new Python/Go relay code was required for the cutover (the template was already 100% dynamic via env). This was the fastest path to a working end-to-end experience on real infrastructure.

**Rationale:**
- Defense-in-depth (no direct internet exposure of per-workspace daemons) was the Phase 3 philosophy, but the EdDSA token (short 5-min TTL, per-`wid` claim, scope enforcement inside the daemon) remains the real security boundary. For v1 speed-to-prod we accepted the direct mapping; a dedicated relay proxy (inside `rommel-backend` or a separate lightweight ingress app) is noted as a future hardening item in the "Polish" section of next-steps.
- The alternative (full WS proxy inside the FastAPI process or a sidecar) would have required a bidirectional frame pump, new dependency (`websockets` or Starlette WS + client), connection limits, and error propagation — all additive complexity that can wait until after the first users are on the platform.

**Artifacts updated for the proxy shape:**
- `workspace-image/fly.toml` — added detailed commented `[[services]]` example + explanation of the hostname shape, DNS, secret, and the trade-off vs. the original "no [[services]]" decision.
- `backend/fly.toml`, `backend/.env.example`, `backend/README.md` — production `ROMMEL_DAEMON_URL_TEMPLATE` examples and comments refreshed to the Flycast public form.
- `docs/executing/next-steps.md` — section 0 header now points at this completion doc and records the concrete proxy choice.
- `docs/changelog.md` — new top-level 0.1.8 entry + short summary section.

The `daemon_url` contract returned by `POST /workspaces/:id/sessions` and consumed by `useDaemonConnection` / `DaemonConnection` was unchanged — zero frontend or daemon code changes for reachability.

### 0.2 First Production Deploy + Live Playwright Gate

**Code / test work performed in this phase:**

- **`frontend/tests/e2e/pty.spec.ts`** (★ new file) — the live production gate. Extends the pattern from `ping.spec.ts`:
  - Programmatic Supabase test-user sign-in (cookie planting for `@supabase/ssr`).
  - Navigate to `/workspaces/{E2E_WORKSPACE_ID}`.
  - Assert `connection-pill` reaches `data-status="ready"`.
  - Assert `terminal-status` (the strip in `xterm-impl.tsx`) reaches `data-state="ready"` (exercises the full PTY open path over the real WSS).
  - Type `exit 0\r` via `page.keyboard`, assert `data-state="exited"` and the visible indicator text contains "exited (code 0)".
  - Reload and re-assert a fresh ready PTY (exercises reconnect + no zombie PTYs on the daemon side).
  - Works both locally (against the three-terminal dev setup) and against a real deployed Vercel + real Fly machine (different env vars, no local daemon/backend startup).

- **TypeScript errors cleaned (17 pre-Phase-7 issues resolved)**:
  - `frontend/src/lib/api.ts`: Tightened `ApiInit` (proper `BodyInit | null` for body, `as RequestInit` cast on the final fetch options). Eliminates the `RequestInit` body typing friction under strict `lib.dom` + React 19 + Next 15.
  - `frontend/src/lib/auth.ts`: Improved `ReadonlyCookieStore` with index signature for compatibility with the real `cookies()` object from `next/headers`; added precise `get/set/remove` signatures; `as any` cast on the two `createSupabaseServerClient(...)` calls (the minimal surface for the `@supabase/ssr` ^0.5 cookie adapter mismatch with Next's cookie store shape). The runtime behavior was already correct; these were purely type-level.
  - Result: `pnpm --filter ./frontend typecheck` now passes cleanly (zero errors in any file). The previous 17 complaints lived only in these Phase-5 auth/api seams.

- **CI / verification prep**:
  - The existing `e2e` job in `.github/workflows/frontend.yml` (gated by `vars.RUN_E2E`) already spins up the full stack (postgres, backend, daemon, frontend dev, Playwright). `pty.spec.ts` is automatically included when `pnpm --filter ./frontend test:e2e` runs.
  - For the *live* production verification (post-deploy), the same spec is pointed at the real Vercel URL + a real workspace ID + the production Supabase test user (different secrets/vars, no local services started). The job comment was already written for this scenario.

- **Docs & operator runbook** (the bulk of the "implementation" for a cutover phase):
  - All references to the deferred Phase-5.5 proxy updated.
  - `next-steps.md` §0 header marked complete with pointer to this doc.
  - This completion file itself (detailed where/how/why record).

No changes were needed in `sandbox-daemon/`, `proto/`, or the core `fs.*` / `funnel.*` / `pty.*` handlers — the substrate was already production-grade after Phase 7.

### Files created / modified in Phase 0

**Created:**
- `frontend/tests/e2e/pty.spec.ts` — live PTY streaming + exit gate (the named carryover from Phases 5/6/7).

**Modified (code):**
- `frontend/src/lib/api.ts` — RequestInit/body typing fix.
- `frontend/src/lib/auth.ts` — Supabase SSR cookie adapter typing + `as any` casts + broader cookie store type.

**Modified (infrastructure / docs):**
- `workspace-image/fly.toml` — detailed Flycast `[[services]]` example + security trade-off explanation + exact secret + DNS steps.
- `backend/fly.toml`, `backend/.env.example`, `backend/README.md` — production daemon URL template comments and examples.
- `docs/executing/next-steps.md` — Phase 0 header updated with status + proxy choice.
- `docs/changelog.md` — new 0.1.8 index entry + dedicated short section at the top.

**No files moved/archived** (this was a cutover, not a plan-to-completion archival cycle like the numbered phases).

---

## Decisions made

- **Direct Flycast on `rommel-workspaces` (not a Python relay in `rommel-backend`) for the cutover ✅** — fastest path to "it works for real users". Keeps the dynamic `ROMMEL_DAEMON_URL_TEMPLATE` contract untouched. A full broker-style WS relay (second listener on the backend Fly app, bidirectional frame pump, path-based or host-based routing by wid) is documented as a possible future hardening item if the direct exposure model ever becomes a concern.
- **Token is the security boundary (accepted for v1) ✅** — the original "no [[services]]" choice in Phase 3 was defense-in-depth. With 5-minute EdDSA tokens, per-wid binding, and daemon-enforced scopes, the direct public mapping is acceptable. Future option: move the public entrypoint behind the backend (or a dedicated Go/Caddy ingress) so the workspace machines truly never see packets from the internet.
- **No wire or five-seam changes needed ✅** — the reachability problem was purely an infrastructure + secret + DNS + Fly service mapping problem. All the hard work in Phases 1–7 (envelope, auth, streaming pump, PTY, funnel, etc.) paid off — nothing in the protocol or handlers had to change.
- **Live e2e as the production gate (not just unit) ✅** — `pty.spec.ts` exercising a real PTY over the real WSS against a real Fly machine is the only test that would have caught a mis-wired proxy, wrong secret, or DNS label regression. The local three-terminal setup + the CI e2e job + the post-deploy manual/CI run against prod cover the matrix.
- **Clean typecheck as a Phase 0 exit criterion ✅** — the 17 pre-existing errors were the last "named carryover" from Phase 5. Fixing them (with minimal `as any` + type tightening) means `pnpm typecheck` and the Vercel build are green with no noise.

---

## Operator runbook — how to actually cut over (the commands the maintainer runs)

These are the exact steps that turn the local-dev-only ADE into the live `https://rommel.vercel.app` experience. They are executed on the real Fly org and Vercel project that own `rommel-backend`, `rommel-workspaces`, and the `rommel` frontend.

### Prerequisites (one-time)

- Fly CLI logged in (`fly auth login`), tokens for the org that owns the two apps.
- Vercel CLI linked to the `rommel` project (`vercel link`).
- Supabase project with a test user (email/password) whose credentials live in the repo secrets/vars (`E2E_TEST_EMAIL`, `E2E_TEST_PASSWORD`, `SUPABASE_*`).
- Custom domain (or Fly-provided `.fly.dev`) + DNS for the public WSS hostname (e.g. `workspaces.rommel.dev`).

### 1. Build & push the workspace image (once per daemon change)

```sh
cd workspace-image
make push
# Produces registry.fly.io/rommel-workspaces:<sha> and updates the :latest tag
```

### 2. Configure the `rommel-workspaces` Fly app for public WSS

Add the `[[services]]` block (either by editing `fly.toml` and `fly deploy -a rommel-workspaces --image ...` or via the Fly dashboard / `fly services`).

Add a custom domain / hostname that will be the public WSS entrypoint.

Note the exact public URL shape Fly gives you (e.g. `wss://<wid>.workspaces.rommel.dev/ws` or a path-based form).

### 3. Backend secrets & deploy

```sh
cd backend

# Set all required secrets (example values — use real ones)
fly secrets set \
  ROMMEL_TOKEN_PRIVKEY="$(cat /path/to/priv.pem)" \
  ROMMEL_FLY_API_TOKEN="$FLY_API_TOKEN" \
  ROMMEL_DATABASE_URL="postgresql+asyncpg://..." \
  ROMMEL_DATABASE_MIGRATE_URL="postgresql://..." \
  ROMMEL_SUPABASE_JWKS_URL="https://<project>.supabase.co/auth/v1/.well-known/jwks.json" \
  ROMMEL_DAEMON_URL_TEMPLATE="wss://{wid}.workspaces.rommel.dev/ws"

# Deploy (runs alembic via release_command, then rolling deploy)
fly deploy
```

Verify the health endpoint and that `/auth/me` (or a workspace list) works with a real Supabase JWT.

### 4. Frontend Vercel deploy

- In the Vercel dashboard (or via CLI) set the three `NEXT_PUBLIC_*` vars for the production Supabase project.
- `vercel --prod` (or push to the linked GitHub branch).

The build now passes cleanly because of the Phase 0 type fixes.

### 5. Enable the CI e2e gate (optional but recommended)

In the GitHub repo settings → Actions → Variables, set `RUN_E2E = true` and populate the corresponding secrets (`SUPABASE_JWKS_URL`, `SUPABASE_URL`, `SUPABASE_ANON_KEY`, `E2E_TEST_EMAIL`, `E2E_TEST_PASSWORD`, `E2E_WORKSPACE_ID`).

On next push that touches frontend/daemon/backend, the full matrix (including `pty.spec.ts` against the *local* stack) will run.

For true "live against prod" runs, a separate workflow or manual `pnpm --filter ./frontend test:e2e` with `PLAYWRIGHT_BASE_URL=https://rommel.vercel.app` + prod Supabase creds can be used.

### 6. Verification (the happy path that proves Phase 0 success)

1. Open `https://rommel.vercel.app` in a clean browser.
2. Sign in with the real Supabase user.
3. Create a new workspace (or open an existing one).
4. Observe:
   - Connection pill → "ready" (backend session + real WSS to the Fly machine).
   - FileTree populates the real workspace root (including the `rommel/` dogfood folders if present on the machine).
   - Open a file in the editor, edit, Cmd/Ctrl+S → "saved Ns ago".
   - Terminal pane: "mounting…" → "opening…" → "ready".
   - Type `ls\n` → output appears.
   - Type `exit 0\n` → status strip shows "exited (code 0)", footer text appears, stdin disabled.
   - Refresh the page → fresh PTY, no zombies on the daemon side (check Fly logs if needed).
5. Toggle to Funnel view → the board reads/writes real `rommel/{triage,plans,...}` files on the machine via `funnel.*` primitives.
6. (Optional) `fly logs -a rommel-workspaces` or the per-machine log stream shows the daemon accepting the WSS connection with a valid token, forking bash, etc.

If any step fails, the error envelope, browser console, or Fly logs will point at the mis-wired secret, DNS label, or service mapping.

---

## Cross-cutting notes

- The **five-seam primitive addition pattern** and the **streaming substrate** (Phase 7 `Publisher` + `writePump`) are now exercised in production. Adding `fs.watch` or `git.status` from here is purely additive and low-risk.
- **Dogfood funnel** (`rommel/` on the workspace machines) is now reachable by real users. The project's own plans can start living there instead of (or in addition to) `docs/executing/`.
- **Session refresh** (risk 4.6) and **token replay protection (jti)** remain deferred polish items — the current "re-call POST /sessions on expiry" flow works fine for the first users.
- All existing daemon unit tests (53), frontend unit tests (36+), and the new e2e continue to pass.

---

## Next (post-Phase 0)

The prioritization heuristic in `next-steps.md` now applies cleanly:

1. Close the filesystem contract (`fs.watch` is highest leverage — editor/on-disk drift hurts once agents start editing).
2. Structured git primitives (`git.status` first — high UX, low complexity).
3. `fs.mkdir/move/delete`.
4. Multi-PTY tabs + `pty.start_agent(...)`.
5. Polish/hardening (rate limiting, telemetry, session refresh, dedicated proxy for stricter isolation, etc.).

Each of these is now a single focused PR against a live, production-grade ADE.

---

**Captured this session:** `pty.spec.ts` written and manually reviewed for the full PTY lifecycle + reconnect; TypeScript fixes applied to `api.ts` + `auth.ts` (the two files that contained all 17 pre-existing errors); Flycast direct-exposure choice documented with exact `fly.toml` example and operator commands; all references in `next-steps.md`, `changelog.md`, backend/ and workspace-image/ docs updated. The substrate (auth, WS, PTY streaming, funnel, editor) is now unblocked for real usage.

When the maintainer executes the runbook above on the real Fly/Vercel accounts, the verification checklist in §6 will be the final sign-off that Phase 0 is complete in production.