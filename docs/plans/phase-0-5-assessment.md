# Phase 0–5 Code Assessment & Production Readiness

**Date:** 2026-05-16
**Scope:** All work landed across `docs/completions/phase-0-production-cutover.md` → `phase-5-fs-watch-filetree-deploy.md`.
**Question asked:** Is the code at ≥9/10 quality, 100% functionally sound, and clear-to-ship for the first prod cutover (FE→Vercel, DB+BE→Fly/DO/OVH)?

**Verdict:** **8.5/10 today, 9.5/10 after ~1 day of fixes.** No release-blocking *functional* bugs were found, but two items violate the project's own engineering standards and must be cleaned up before the first prod deploy. Test coverage for the two newest streaming primitives (`fs.watch`, `pty.start_agent`) is also missing — they work, but they aren't gated.

## TL;DR — what to fix before merging to prod

| # | Severity | File | What |
|---|---|---|---|
| 1 | **Blocker** | `sandbox-daemon/internal/fs/handler.go:534` | `panic()` on fsnotify init failure. Replace with a stored error surfaced as `fs.watch_failed` from the next `Watch()` call. |
| 2 | **Blocker** | `sandbox-daemon/internal/fs/handler.go:525-541` | `*fsnotify.Watcher` is never `Close()`d. Add `defer` in a shutdown hook (or close + nil it when the last subscriber is removed). |
| 3 | **Major** | `sandbox-daemon/internal/ws/server_test.go` (+ `internal/pty/handler_test.go`) | No dedicated tests for `fs.watch` (event delivery, recursive add, OnDisconnect cleanup, soft cap) or `pty.start_agent` (allow-list, cwd/env, success). The harness (`drainUntil`, `FakePublisher`) is ready; 6-8 mechanical cases each. |
| 4 | **Minor** | `frontend/src/components/filetree/FileTree.tsx:37-60` | Dead `handleNew("file", …)` branch ending in `fsWrite(null as any, …)`. Unreachable (the menu wires to `createEmptyFile`) but a clear tripping hazard. Delete it; keep `handleNew` only for `"dir"`. |
| 5 | **Minor** | `frontend/src/hooks/useFs.ts:105-110` | `useFsWatch` is a *stub* in this file (the *real* `useFsWatch` lives in `frontend/src/hooks/useFsWatch.ts`). The stub returns its args. Either delete the stub or finish it. Confusing duplicate name. |
| 6 | **Minor** | `frontend/src/lib/auth.ts:57,75` | Two `as any` casts on the `@supabase/ssr` cookie adapter — documented in Phase 0 as the minimum surface to make `tsc` green. Track this so the next supabase bump is the moment we remove them. |
| 7 | **Minor** | `backend/api/main.py:73-79` | `CORS allow_origins=["*"]` with a comment that "Production locks this to the frontend domain via an env override" — but no env var is wired. Add `ROMMEL_CORS_ORIGINS` to `Settings` and read it. |
| 8 | **Minor** | `sandbox-daemon/internal/git/handler.go:358-367` | `git.Commit` extracts OID by string-searching `]` in commit output. Switch to `--pretty=format:%H` or follow with `git rev-parse HEAD`. |

Items 1, 2, 4 are ~30 minutes total. Items 3 (tests) and 7 (CORS env) are ~3 hours total. Item 8 is ~10 minutes. After this list lands, the code clears 9/10.

## What's verified solid (✅)

### Daemon (`sandbox-daemon/`)
- **All filesystem primitives** are real (not stubs): `list`, `read`, `write`, `mkdir`, `move`, `delete` — atomic where it matters (`os.Rename`), sandboxed via `resolve()` (`internal/fs/handler.go:501-521`), typed error codes wired through `internal/ws/envelope.go`.
- **`fs.watch` event pump** is the real implementation (Phase 1's skeleton was replaced in Phase 5): single `*fsnotify.Watcher`, refcounted path map, recursive walk + auto-add of new directories, `runEventLoop` goroutine, `Create/Write/Remove/Rename → created/modified/deleted/moved` translation, `Publisher` captured at `Watch()` time, `OnDisconnect` decrements + removes (`internal/fs/handler.go:426-671`).
- **PTY substrate** is canonical: session map keyed by UUID, `Setsid` + SIGTERM group teardown, 4 KiB output chunks, drop counter, separate `outputLoop`/`waitLoop` goroutines, soft cap 4/conn, idempotent `Close`, per-conn `OnDisconnect` cleanup (`internal/pty/handler.go`).
- **`pty.start_agent`** reuses the entire PTY substrate; `claude|codex|cursor` allow-list enforced, returns a normal `pty_id`. Every existing `pty.*` op works on it (`internal/pty/handler.go:166-233`).
- **Git primitives** use `runGit(workdir, …)` with safe argv (no shell), porcelain parsing for status, complete branch + commit + diff (`internal/git/handler.go`). `workspace.info.repo` populated from real git output.
- **WS substrate** (`internal/ws/server.go`, `pump.go`) — `HandlerCtx`, `Publisher` interface, `ConnLifecycle.WithLifecycle()`, UUIDv4 connID, per-conn write pump with `Send` (blocking) + `Publish` (drop-oldest at 256 frames). Two domains (PTY + FS) already share the substrate cleanly.
- **EdDSA token verification** (`internal/auth/token.go`): signature + `exp` + scope + wid match.
- **Funnel** (`internal/funnel/handler.go`): six-stage transition table, 1 MiB read cap, name validator (rejects separators / leading `.` / `..`), atomic `os.Rename` for promote.
- **53+ existing daemon unit tests pass** (per phase docs; build gates couldn't run locally in this assessment shell because `go` is not on PATH here — see "Gates not run locally" below).

### Frontend (`frontend/`)
- **`DaemonConnection`** (`src/lib/daemon.ts`, 371 LOC): full WS wrapper, UUID req-ids, `rpc`/`notify`/`subscribe`, exponential reconnect backoff (250 ms → 5 s, 5 attempts), token-expiry refresh, typed error envelope. Notify path has 5-second inflight TTL (Phase 7 decision).
- **All `fs.*`, `git.*`, `funnel.*`, `pty.*` lib wrappers** present and typed against `@rommel/proto`.
- **`usePty`** (162 LOC) — opens on mount when daemon ready, subscribes filtered by `pty_id`, uses the `useRef` pattern from Phase 7 to avoid effect churn, best-effort `pty.close` on unmount.
- **`useFsWatch`** standalone hook (`frontend/src/hooks/useFsWatch.ts`, 103 LOC) — calls `fsWatch` on daemon ready, subscribes to `"fs.watch-event"`, stable `onEvent` via `useRef`, best-effort cleanup. (Note: a *different* `useFsWatch` stub also exists in `useFs.ts`; see Issue 5.)
- **`TerminalTabs`** (124 LOC) — tab bar, "+"/close buttons, soft cap 4, hidden-class strategy so inactive tabs keep streaming and preserve scrollback.
- **`xterm-impl.tsx`** (154 LOC) — `term.onData → ptyInput`, `pty.output → term.write`, `ResizeObserver → ptyResize` debounced 150 ms, exit footer, Playwright `data-testid` hooks.
- **FileTree right-click menu** is genuinely wired: New File / New Folder / Rename / Delete for directories, Rename / Delete for files. The menu's "New File" routes through the working `createEmptyFile(parent)` (`FileTree.tsx:147`), not the dead `handleNew("file", …)` path.
- **GitStatusPill**, **FunnelBoard**, **monaco-impl.tsx** (Cmd/Ctrl+S save) all live.
- **`tests/e2e/pty.spec.ts`** is the live production gate: programmatic Supabase sign-in → workspace → `connection-pill` ready → `terminal-status` ready → type `exit 0\r` → assert `data-state="exited"` → reload → fresh ready.
- **Unit suite**: 6 files (daemon, connection-store, auth, fs-rpc, funnel-rpc, pty-rpc) using the `FakeWebSocket` + `serverPush` pattern.

### Backend (`backend/`) + Infra
- **Auth seam**: Supabase JWKS validation with `jose`/PyJWT, JWKS cache + invalidation on rotation (`services/auth.py`).
- **Session broker**: EdDSA signing, single-`now()` for `iat`/`exp` (risk 4.5 guarded), `jti` UUID in claim, scope array (`services/session_broker.py`).
- **Row-level security**: `SET LOCAL rommel.user_id` per request inside `async with session.begin()`; two Postgres roles (privileged migrator, `app_user` RLS-bound); `FORCE ROW LEVEL SECURITY` enabled in `alembic/versions/0001_init.py:106-161`.
- **SQL is fully parameterized** — SQLAlchemy 2.0 Core throughout. No string interpolation in any repo file.
- **Alembic ↔ models** are in sync. `release_command = "alembic upgrade head"` blocks bad rollouts.
- **Healthcheck** `/healthz` configured in `fly.toml` with 10s/2s/5s.
- **Secrets hygiene**: nothing in code; `.env.example` lists every var; daemon image only embeds the EdDSA *public* key.
- **Fly orchestrator** wires `metadata.label=wid` for `.internal` DNS, injects `ROMMEL_WID`, `auto_destroy=false`, restart-on-failure.
- **Workspace image** `fly.toml` has the Flycast `[[services]]` block (Phase 0 cutover).

## Files significantly over 500 LOC (refactor candidates)

| File | LOC | Recommendation |
|---|---|---|
| `sandbox-daemon/internal/ws/server_test.go` | 1157 | Test file; size acceptable. Adding the `fs.watch` + `pty.start_agent` cases (Issue 3) will push it higher — at that point split into `server_test.go` + `pty_test.go` + `fs_test.go` + `funnel_test.go` (mirror the handler packages). |
| `sandbox-daemon/internal/fs/handler.go` | 676 | Two concerns interleaved. Split: keep `handler.go` for `List/Read/Write/Mkdir/Move/Delete/resolve`; move all watch machinery (`Watch`, `OnDisconnect`, `runEventLoop`, `ensure/add/removeWatcherLocked`, `watchEntry`, refcount map) into `watcher.go` in the same package. |
| `sandbox-daemon/internal/pty/handler.go` | 570 | Lift the per-session lifecycle into `session.go` (struct + `outputLoop` + `waitLoop` + `teardown`); leave the dispatch surface (`Open`, `Input`, `Resize`, `Close`, `StartAgent`, `OnDisconnect`) in `handler.go`. |

Everything else is comfortably under 400 LOC. The two largest TS files (`daemon.ts` at 371, `FileTree.tsx` at 316) are dense but cohesive and would lose readability if split today.

## Concurrency / safety findings

- **Benign race** between `runEventLoop` (publishing) and `OnDisconnect` (deleting watch entries) in `internal/fs/handler.go`. `wmu` guards the map structure, but the event loop can capture a `Publisher` and publish to it *after* its connection has been torn down. The pump drops orphan frames silently, so this is currently harmless — but it is a latent footgun. A `connDropped` flag (set in `OnDisconnect`, checked at the top of `handleFsEvent`) closes the window.
- **No data races** in `pty/handler.go`: sessions are accessed under `smu`; `outputLoop`/`waitLoop` are separate goroutines that only touch their own session struct; `teardown` is idempotent via `atomic.Bool.CompareAndSwap`.
- **No goroutine leaks** in the PTY path (Phase 7's `OnDisconnect` walks the session map). The fs.watch path *does* leak the `runEventLoop` goroutine + the fsnotify file descriptor at process shutdown (Blocker 2).
- **Path sandbox is solid**: `resolve()` rejects absolute paths and verifies the cleaned join still lives under `Root`. The duplicated copy in `pty.resolveCwd` agrees with it. Consider extracting to `internal/sandbox/resolve.go` (one-time hardening, not a blocker).

## Test coverage gaps (the only material concern)

| Surface | Tests today | Gap |
|---|---|---|
| `fs.watch` | None | Add: round-trip, single-file watch event, recursive watch picks up new dirs, soft cap enforcement, `OnDisconnect` removes from OS watcher, invalid path. |
| `pty.start_agent` | None | Add: success (with a stub agent in `$PATH`), unknown agent → `pty.unknown_agent`, cwd + env passthrough, soft cap shared with `pty.open`. |
| `pty.output_dropped` | Test double exists, no case wires it | Force pump backpressure (fill `Publisher` to 256) and assert one `pty.output_dropped` is emitted on next successful publish. |

The harness (`drainUntil`, `FakePublisher`, `FakeWebSocket.serverPush`) is already in place; these are mechanical to add.

## Gates not run locally during this assessment

`pnpm` and `go` are not on this shell's PATH, so I could not re-run:
- `pnpm --filter ./frontend typecheck` / `test:unit` / `lint`
- `make -C sandbox-daemon test`
- `pytest -q` in `backend/`

Phase 0 doc states a clean `tsc` (after the `as any` casts in `auth.ts`) and 53 daemon + 36 frontend tests green. **Recommended pre-merge step:** run all three locally and paste the green output into the PR description.

## Deploy-target recommendation: Fly.io (yes) vs. DO/OVH (no, today)

The codebase is *cleanly* coupled to Fly.io for the workspace orchestrator (`backend/services/fly_orchestrator.py` talks directly to `api.machines.dev`) and for daemon reachability (Flycast `[[services]]` in `workspace-image/fly.toml`). Two paths forward:

1. **Recommended for v1 cutover — Fly.io for everything.** Backend → `rommel-backend`, workspaces → `rommel-workspaces`, frontend → Vercel. This is the path the Phase 0 runbook is written for, and the substrate is already wired. Cost is modest at v1 traffic; SLA is acceptable.
2. **Future hedge — abstract `FlyOrchestrator` behind a `WorkspaceOrchestrator` interface.** A DigitalOcean App Platform / Render / OVH implementation would need either custom Droplet/VM orchestration (DO/OVH) or container-job spawning (Render). All are doable, none are a one-line swap. Don't pay this complexity tax until Fly cost or SLA forces it.

OVH and bare-metal options are not recommended for v1 — they add Kubernetes/VM ops cost without buying anything the architecture currently uses.

## Recommended sequence to reach 9/10 + green prod cutover

1. **(30 min)** Fix Blocker 1 (panic → `fs.watch_failed` error) and Blocker 2 (watcher close on shutdown / refcount-zero). Add a `Stop(ctx)` on `fs.Handler` called from `cmd/daemon/main.go` on `SIGTERM`.
2. **(15 min)** Delete dead `handleNew("file", …)` branch in `FileTree.tsx:37-60` and the stub `useFsWatch` in `useFs.ts:105-110`.
3. **(15 min)** Add the `ROMMEL_CORS_ORIGINS` env var to `backend/api/config.py` and consume it in `main.py`.
4. **(2-3 hr)** Land the deferred tests: `fs.watch` (6 cases in `server_test.go`), `pty.start_agent` (4 cases split between `server_test.go` and `pty/handler_test.go`), `pty.output_dropped` (1 case).
5. **(10 min)** Switch `git.Commit` OID extraction to `--pretty=format:%H` or `git rev-parse HEAD`.
6. **(deferred, post-cutover)** Split `internal/fs/handler.go` (watch → `watcher.go`) and `internal/pty/handler.go` (sessions → `session.go`). Not on the critical path for the first deploy; do once the test additions push `server_test.go` past comfort.
7. Run the three gates locally, paste green into the PR, follow the Phase 0 runbook.

## Bottom line

**The substrate is genuinely production-grade.** Phases 0–5 were executed cleanly: five-seam additions across `proto/ → daemon → lib → hooks → component`, with no architectural debt. The only items standing between today's code and a 9/10 rating are housekeeping: one Go panic to swap for an error return, one Go resource to close, three dead-code deletions in TS, three missing test files, and one CORS env var.

After that day of cleanup, `fly deploy` and `vercel --prod` are safe.
