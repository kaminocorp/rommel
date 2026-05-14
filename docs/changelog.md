# Changelog

All notable changes to this project, latest on top. Each entry links to the corresponding completion doc under [`docs/completions/`](./completions/).

## Index

- [**0.1.7** — 2026-05-14](#017--2026-05-14) — First streaming primitives: `pty.*` lit wire-to-wire (`pty.open`/`input`/`resize`/`close` + server-pushed `pty.output`/`pty.exit`/`pty.output_dropped`). New per-connection write pump + `Publisher` / `HandlerCtx` / `ConnLifecycle` seams. Terminal pane no longer inert. Streaming-substrate era closes.
- [**0.1.6** — 2026-05-13](#016--2026-05-13) — `rommel/` planning funnel dogfooded; first real daemon primitives wired wire-to-wire: `fs.list` / `fs.write` / `funnel.list` / `funnel.read` / `funnel.promote`. Real FileTree, Cmd+S-saving EditorPane, six-column FunnelBoard. Scaffolding era closes.
- [**0.1.5** — 2026-05-13](#015--2026-05-13) — `frontend/`: Next.js 15 + React 19 IDE shell — Supabase SSR auth, TanStack Query, Zustand store, `lib/daemon.ts` WS wrapper, dynamic-imported Monaco + xterm panes, Vitest + Playwright suites. Pattern-B browser-side originator committed; live Playwright + Vercel deploy are the named carryover.
- [**0.1.4** — 2026-05-13](#014--2026-05-13) — `backend/`: FastAPI control plane — Supabase auth seam, EdDSA session-token broker, workspace CRUD, Fly orchestrator stub. Integration gate green: backend signs → daemon verifies → ping round-trips.
- [**0.1.3** — 2026-05-13](#013--2026-05-13) — `workspace-image/`: Fly Machine VM image — baked daemon binary, EdDSA pubkey, `git`/`curl`/`tini`; canonical Dockerfile.
- [**0.1.2** — 2026-05-12](#012--2026-05-12) — `sandbox-daemon/`: Go WS server with EdDSA token validation, `system.ping`, and real `fs.read`.
- [**0.1.1** — 2026-05-04](#011--2026-05-04) — `proto/` source-of-truth + codegen for TS/Go/Pydantic; session token contract committed.
- [**0.1.0** — 2026-05-04](#010--2026-05-04) — Repo root scaffolding: monorepo plumbing, defensive CI, no subtree code yet.

---

## 0.1.7 — 2026-05-14

**Phase 7 — `pty.*` primitives + streaming substrate.** Completion doc: [`docs/completions/phase-7-pty.md`](./completions/phase-7-pty.md). Plan (archived on completion): [`docs/archive/phase-7-pty-plan.md`](./archive/phase-7-pty-plan.md). Specialization of [`scaffolding-plan.md`](./executing/scaffolding-plan.md) §2, promoted to the top of the 0.1.6 "Next" list.

Status: ✅ Code authored to plan end-to-end. The first **streaming primitive batch** is wired wire-to-wire: `pty.open` / `pty.input` / `pty.resize` / `pty.close` as request/response RPCs, `pty.output` / `pty.exit` / `pty.output_dropped` as server-pushed events. The load-bearing structural change is `internal/ws/pump.go` — a per-connection write goroutine that owns the gorilla/websocket write side, with `Send` (blocking, response-grade) + `Publish` (best-effort, drop-oldest at 256-frame depth) APIs that any future streaming primitive (`fs.watch`, `git.log --follow`) reuses. The terminal pane is no longer inert: `term.onData` → `pty.input`, `pty.output` → `term.write`, `ResizeObserver` → `pty.resize` (150 ms debounced), `pty.exit` → greyed-out footer. **All 53 daemon tests + 36 frontend unit tests pass green** (47 + 27 from Phase 6 plus 6 new WS-level PTY cases, 7 new PTY handler-level cases, 9 new frontend `pty-rpc` cases). Carryover, same shape as Phase 6: live Playwright extension (`pty.spec.ts`) for the new terminal flow + first Vercel deploy of the upgraded shell.

### Added

- **`proto/schemas/pty/`** — three new schemas:
  - `close.json` (`PtyCloseRequest{pty_id}` + empty `PtyCloseResponse`; idempotent on the daemon — closing an unknown / already-closed pty_id returns success).
  - `exit-event.json` (`PtyExitEvent{pty_id, exit_code, signal?}`; `exit_code: -1` when signal-terminated; daemon flushes pending pty.output before emit).
  - `output-dropped-event.json` (`PtyOutputDroppedEvent{pty_id, dropped_count}` — emitted on the next successful publish after one or more drops).
- **`sandbox-daemon/internal/ws/pump.go`** — ★ new file. `writePump` owns the per-connection write side. `Send(f)` is blocking submit (responses / errors that must reach the client). `Publish(f)` is best-effort; if the 256-frame buffer is full, drops the oldest enqueued frame and retries once. `dropFn` callback lets the conn-level cleanup count drops for an operator-visible log line. `connPublisher` adapts the pump to the new `ws.Publisher` interface.
- **`sandbox-daemon/internal/pty/handler.go`** — ★ real implementation (~330 LOC). Daemon-global session map keyed by `pty_id` (UUIDv4), each tagged with the owning `connID`. `Open` spawns `$SHELL || /bin/bash || /bin/sh` under `Setsid: true` so the whole process group can be SIGTERMed at once. `outputLoop` reads 4 KiB chunks → base64-encodes → publishes `pty.output`; on Publish-drop bumps `droppedSinceFlush` and emits `pty.output_dropped` on the next successful publish. `waitLoop` blocks on `cmd.Wait()`, classifies the exit (`-1` + signal name for signaled, else exit code), unregisters, publishes `pty.exit`. `teardown()` SIGTERMs the process group then SIGKILLs after a 200 ms grace; idempotent via `atomic.Bool.CompareAndSwap`. `OnDisconnect(connID)` walks the map and tears down every session for that connection.
- **`sandbox-daemon/internal/pty/handler_test.go`** — ★ new file, 7 cases: `TestHandler_SoftCap`, `TestHandler_OpenInvalidSize`, `TestHandler_BadCwd`, `TestHandler_InputAndExit`, `TestHandler_OutputContainsEcho`, `TestHandler_CloseIsIdempotent`, `TestHandler_OnDisconnectSparesOtherConns`. Uses a `fakePublisher` test double that captures emitted events and lets tests assert payload contents + ordering.
- **`frontend/src/lib/pty.ts`** — ★ typed wrappers (~80 LOC): `ptyOpen` (returns `PtyOpenResponse`), `ptyInput` (fire-and-forget via the new `conn.notify()` path; auto-base64-encodes Uint8Array or string), `ptyResize`, `ptyClose`. Plus `bytesToBase64` / `base64ToBytes` browser-native helpers that chunk through `String.fromCharCode(...slice)` to avoid the "Maximum call stack size exceeded" trap on large buffers.
- **`frontend/src/hooks/usePty.ts`** — ★ React adapter. Opens on mount once the daemon is `ready`, captures the `pty_id`, subscribes to `pty.output` / `pty.exit` / `pty.output_dropped` filtered by `pty_id`, exposes `send(data)` / `resize(cols, rows)` plus `status` / `exitCode` / `signal` / `droppedCount` / `error` React state. Best-effort `ptyClose` on unmount; daemon's `OnDisconnect` is the safety net.
- **`frontend/tests/unit/pty-rpc.test.ts`** — ★ 9 cases: base64 round-trip (small + 100K), `ptyOpen` / `ptyInput` / `ptyResize` / `ptyClose` wrappers, `pty.output` / `pty.exit` / `pty.output_dropped` event delivery through the upgraded `FakeWebSocket.serverPush()` helper, plus a `notify` fire-and-forget case.
- **`docs/completions/phase-7-pty.md`** — this phase's completion doc.

### Modified

- **`proto/schemas/pty/resize.json`** — was a `_todo` stub; now defines `PtyResizeRequest{pty_id, cols, rows}` (same 1–1000 bounds as `pty.open`) and an empty `PtyResizeResponse`. Daemon calls `pty.Setsize()` which wraps `TIOCSWINSZ`.
- **`sandbox-daemon/go.mod` / `go.sum`** — added `github.com/creack/pty v1.1.24`. The dependency was deferred from Phase 2; lands here because Phase 7 is the first phase that needs it.
- **`sandbox-daemon/internal/ws/envelope.go`** — five new stable error codes (`pty.not_found`, `pty.spawn_failed`, `pty.write_failed`, `pty.invalid_size`, `pty.limit_reached`); internal `eventKind` constant aliasing `protogen.EnvelopeKindEvent` so the pump and publishers stay in agreement.
- **`sandbox-daemon/internal/ws/server.go`** — the load-bearing migration. New `Publisher` interface (`Publish(eventType, payload) bool`), new `HandlerCtx{Ctx, Claims, Publisher, ConnID}` struct, new `HandlerFunc` signature `(HandlerCtx, json.RawMessage) → (json.RawMessage, *EnvelopeError)`, new `ConnLifecycle{OnDisconnect(connID)}` interface registered via `Server.WithLifecycle()`. `runConn` mints a UUIDv4 `connID`, starts a write pump, builds a `connPublisher` against it, threads both into the `HandlerCtx` it constructs for every dispatch. All outbound frames (responses, errors, events) funnel through `pump.Send` / `pump.Publish`.
- **`sandbox-daemon/internal/fs/handler.go`**, **`sandbox-daemon/internal/funnel/handler.go`** — mechanical migration of every handler method to the new `(ws.HandlerCtx, json.RawMessage)` signature. No behavioural change; `context` import dropped (`Ctx` lives on `HandlerCtx` now; none of these handlers consult it yet).
- **`sandbox-daemon/cmd/daemon/main.go`** — wired the four real PTY routes into the dispatch map (replaces the Phase-2 `NotImplemented` stubs), constructs `ptyh := ptyx.New(cfg.WorkspaceRoot)` once at startup, registers it as the connection lifecycle via `wsx.NewServer(...).WithLifecycle(ptyh)`. The inline `workspace.info` closure and `pingHandler` are updated to the new signature.
- **`sandbox-daemon/internal/ws/server_test.go`** — harness now constructs the PTY handler, registers `WithLifecycle(ptyh)`, broadens default token scopes to include `pty:rw`, exposes `h.pty` on the struct so the disconnect-cleanup test can poll `OpenSessionCount()`. `roundTrip()` now skips event-kind frames between request → response (events are unsolicited, no `id` to correlate). New `drainUntil(timeout, predicate)` and `sendFrame()` helpers. 8 new PTY cases: `TestPty_OpenAndExit`, `TestPty_InputProducesOutput`, `TestPty_ResizeRoundTrip`, `TestPty_ResizeInvalidSize`, `TestPty_CloseIsIdempotent`, `TestPty_InputUnknownPtyId`, `TestPty_InsufficientScope`, `TestPty_OnDisconnectCleansUp`.
- **`frontend/src/lib/daemon.ts`** — added `notify<TReq>(type, payload): void`. Sends a request frame with a UUID id, registers an inflight slot whose `resolve` is a no-op and whose `reject` console-warns on the error envelope, then auto-deletes the slot after `NOTIFY_INFLIGHT_TTL_MS = 5_000`. Used by `pty.input` so every keystroke doesn't mint an unawaited Promise.
- **`frontend/src/components/terminal/xterm-impl.tsx`** — replaced the inert welcome banner with a real PTY-wired pane. Mounts xterm + fit + web-links, measures initial cols/rows, then calls `usePty({ cols, rows, env: { TERM: "xterm-256color", COLORTERM: "truecolor" } })`. Wires `term.onData → pty.send` and `pty.onOutput → term.write`; `ResizeObserver → fit.fit() → pty.resize` (150 ms trailing debounce). On `pty.exit` writes a dimmed `[process exited (code N)]` footer and flips `disableStdin = true`. Status strip carries `data-testid="terminal-status"` + `data-state={pty.status}` for the Playwright integration gate.

### Removed / Moved

- **`docs/executing/phase-7-pty-plan.md`** → **`docs/archive/phase-7-pty-plan.md`** — same archival move Phases 3 / 4 / 5 / 6 made on completion. The `executing/` folder is reserved for in-flight plans.
- **`rommel/executing/phase-7-pty-plan.md`** → **`rommel/archive/phase-7-pty-plan.md`** — the dogfooded copy follows the same rule.
- New: **`rommel/completions/phase-7-pty-completion.md`** — a short pointer card sitting in `completions/` until the phase ships, at which point a real `funnel.promote` moves it to `archive/`. Meta-dogfooding the Phase-6 primitive.

### Decisions

- **`HandlerCtx` over a bare context pair ✅** — `(ctx, claims, payload)` → `(HandlerCtx{Ctx, Claims, Publisher, ConnID}, payload)`. One breaking change to every handler, mechanical to migrate. The `Publisher` field is what made streaming primitives possible without sprinkling socket refs through handler code.
- **`ConnLifecycle` interface registered via `WithLifecycle` ⚠ refined** — alternative was a Done channel on `HandlerCtx`. Interface-registration wins: per-handler concerns (PTY tracks per-conn sessions; future `fs.watch` will track per-conn watchers) stay on the handler. `runConn`'s deferred cleanup calls `OnDisconnect(connID)` synchronously.
- **Per-connection write pump with drop-oldest at 256 frames ✅** — `Send` blocks (responses must reach the client); `Publish` drops oldest then retries (events are best-effort). At ~4 KiB/event the buffer is ~1 MiB of in-flight tolerance — survives FE GC pauses, can't OOM the daemon on a runaway PTY.
- **Bash via `pickShell()` with `$SHELL → /bin/bash → /bin/sh` fallback, no `--login` ✅** — avoids `/etc/profile` latency on first keystroke. `~/.bashrc` still runs for interactive shells.
- **`pty.input` is fire-and-forget on the wire AND in the FE API ✅** — daemon returns `(nil, nil)` on success; `conn.notify()` on the FE sends with an id (so errors can match), registers a transient inflight slot, TTLs the slot after 5 s. Every keystroke is one tiny frame; no awaited Promise.
- **5 ms output flush timer dropped ⚠ refined** — plan §0.4 sketched a 4 KiB + 5 ms coalescing pair. Implementation does only the 4 KiB buffer. Kernel already coalesces small writes; the timer was premature optimisation.
- **Per-PTY drop counter in the handler, not the pump ⚠ refined** — pump's `dropFn` only counts drops for an operator log line. The PTY handler maintains its own `droppedSinceFlush` for the per-session `pty.output_dropped` events.
- **`outputLoop` and `waitLoop` are separate goroutines ⚠ refined** — splits the EOF-vs-exit concerns. Output loop publishes until the master fd closes; wait loop publishes `pty.exit` from `cmd.Wait()`. Sidesteps the "shell forks a child that holds the slave open" race.
- **Soft cap of 4 PTYs per connection ✅** — defense against runaway agents. v1 UI shows 1; soft cap leaves headroom for the eventual multi-PTY tabs without changing the wire contract.
- **`xterm-impl.tsx` reads `pty` through a `useRef` ⚠ refined** — plan §3.3 wrote `useEffect(..., [pty.status, pty.send])`. The `usePty` return object identity churns per render, which would re-subscribe `term.onData` unnecessarily. The ref pattern decouples effect lifetime from `pty` identity.

### Cross-cutting: the streaming substrate is now in place

Phase 6 closed out the request/response substrate. Phase 7 adds streaming alongside it:

- **One additive seam per streaming primitive.** Adding `fs.watch` or `git.log --follow` from here is the same five steps as a request/response primitive — schema, codegen, Go handler, dispatch entry, TS wrapper + hook — *plus* publishing via `hc.Publisher.Publish(eventType, payload)` from inside the handler. The write pump, `Publisher` interface, disconnect cleanup hook, drop-oldest backpressure, and FE-side `subscribe()` correlation are all in place.
- **The handler-side ergonomics stay the same.** `HandlerCtx` looks bigger than `(ctx, claims)` but every handler still ignores the fields it doesn't need. `fs.read` doesn't care about `Publisher` or `ConnID`; `pty.open` ignores `Ctx` (the PTY's lifetime is longer than the request's).
- **The test patterns scale.** `server_test.go`'s `roundTrip` already speaks the wire; the new `drainUntil(predicate)` helper extends it to event streams without rewriting the harness. The frontend's `FakeWebSocket.serverPush(type, payload)` does the same on the FE side. Every future streaming primitive lands ~5 cases against the same harness.

### Verification

```sh
# Codegen: 4 new pty schemas + finalised resize regenerate clean, no drift.
make proto
git diff --exit-code proto/clients proto/schemas/pty

# Daemon unit suite — 53 cases (47 from Phase 6 + 6 new WS-level PTY + 7 new handler-level PTY).
make -C sandbox-daemon test
# expected: internal/config, internal/pty, internal/ws all PASS

# Frontend unit suite — 36 cases (27 from Phase 6 + 9 new pty-rpc).
pnpm --filter ./frontend test:unit
# expected: 6 files, 36 tests — auth(2), connection-store(8), daemon(8),
#           fs-rpc(4), funnel-rpc(5), pty-rpc(9) — all green

pnpm --filter ./frontend lint
# expected: clean exit

# Three-terminal end-to-end:
#   T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
#   T2: docker compose -f backend/compose.yaml up -d postgres && make -C backend migrate run
#   T3: pnpm --filter ./frontend dev
# Browser:
#   - Sign in → open the dev workspace
#   - Terminal pane: "mounting…" → "opening…" → "ready"
#   - Type `ls\n` → output renders inline
#   - Drag terminal divider → contents reflow; one debounced pty.resize per gesture
#   - Type `exit 0\n` → pane greys out with "[process exited (code 0)]"
#   - Refresh page → fresh prompt; daemon logs no zombie PTYs
```

Captured this session: daemon `go test ./...` all green at 53 tests / ~4.4 s wall (incl. real bash spawns inside `t.TempDir()` workspaces), frontend `pnpm test:unit` green at 6 files / 36 tests / ~770 ms, `pnpm lint` clean exit. `pnpm typecheck` reports 17 errors — all in pre-Phase-7 files (the Phase 5 named carryover: `@supabase/ssr` cookie typing, `RequestInit` body typing); zero in any Phase-7-touched file.

### Next

The streaming substrate is now done. The 0.1.6 "Next" candidates remain in the same order, with PTY pulled off the top:

1. **`fs.watch`** — first natural follow-up; the obvious next streaming primitive. Closes the editor / on-disk drift gap Phase 6 §9.3 flagged. Lands as one additive PR against the now-proven five-seam pattern plus `Publisher`.
2. **`git.*` structured primitives** — `git.status`, `git.diff`, `git.commit`, `git.branch.*`. Shell out internally; return parsed structured data over the existing request/response path.
3. **`fs.mkdir` / `fs.move` / `fs.delete`** — closes the v1 file-tree story.
4. **Multi-PTY tabs (UI-only)** — schema and daemon soft-cap already support 4; FE shows 1. Pure UI work.
5. **`pty.start_agent(claude|codex)`** — Vision Layer 3's hook. Small additive primitive: open a PTY, immediately exec the agent CLI.

Carryover follow-ups (network-bound): live Playwright `tests/e2e/pty.spec.ts` extending Phase 5's `ping.spec.ts`, first Vercel deploy of the upgraded shell, the deferred Phase-5.5 Flycast `wss://` proxy for production reachability.

---

## 0.1.6 — 2026-05-13

**Phase 6 — `rommel/` planning funnel + first real daemon primitives.** Completion doc: [`docs/completions/phase-6-funnel.md`](./completions/phase-6-funnel.md). Plan (archived on completion): [`docs/archive/phase-6-funnel-plan.md`](./archive/phase-6-funnel-plan.md). Specialization of [`scaffolding-plan.md`](./executing/scaffolding-plan.md) §6, broadened per the 0.1.5 "Next" pointer.

Status: ✅ Code authored to plan end-to-end. The §6 dogfooded folders are in place at `rommel/{triage,plans,next-up,executing,completions,archive}/`. The first **real daemon primitives** are wired wire-to-wire: `fs.list`, `fs.write`, `funnel.list`, `funnel.read`, `funnel.promote` — each with a typed `@rommel/proto` schema, a Go handler, a daemon-level test, a typed TS wrapper in `frontend/src/lib/`, a TanStack-Query hook, and a real component consuming it. **19 new daemon tests + 16 new frontend unit tests pass green** alongside the prior suites (47 daemon-side total, 27 frontend-side total). The "scaffolding era" closes here: every future primitive is one additive PR against five seams (`proto/schemas/<verb>.json` → `cmd/daemon/main.go` dispatch → `internal/<domain>/handler.go` → `frontend/src/lib/<domain>.ts` → `frontend/src/hooks/<useDomain>.ts`). Carryover, same shape as Phase 5: live Playwright extension for the new flows + first Vercel deploy of the upgraded shell.

### Added

- **`rommel/`** dogfood folder tree (visible folder per user confirmation; `vision.md` allowed either, the kanban-on-disk concept argues for visible):
  - `rommel/README.md` (overview + stage table + link to vision.md §Layer 2).
  - One `README.md` per stage under `triage/` `plans/` `next-up/` `executing/` `completions/` `archive/`.
  - `rommel/executing/phase-6-funnel-plan.md` — dogfooded copy of the plan (duplicated, not symlinked; survives Windows clones).
- **`proto/schemas/funnel/`** — new domain:
  - `list.json` (`FunnelListRequest/Response` + `FunnelStage` + `FunnelEntry`; six-stage enum on the schema).
  - `read.json` (`FunnelReadRequest/Response`; 1 MiB body cap codified in description; daemon enforces).
  - `promote.json` (`FunnelPromoteRequest/Response`; stage enum on both `from` and `to`).
- **`sandbox-daemon/internal/funnel/handler.go`** — ★ new package. `Handler{Root}` rooted at `<WorkspaceRoot>/rommel` (convention, no env var). `List` returns empty for a missing rommel/ (the funnel is opt-in). `Read` caps at 1 MiB. `Promote` validates against the transition table via `isValidTransition(from, to)` then `os.Rename` for atomic POSIX moves. Name validator rejects path separators, leading dots, and `..`.
- **`frontend/src/lib/fs.ts`** — typed wrappers: `fsList(conn, path)`, `fsRead(conn, path, encoding?)`, `fsWrite(conn, path, contents, encoding?)`. ~30 LOC; the wire contract is owned by `@rommel/proto`, the transport by `DaemonConnection` — this file just bolts them together.
- **`frontend/src/lib/funnel.ts`** — same shape: `funnelList`, `funnelRead`, `funnelPromote`. Plus `FUNNEL_STAGES` constant, `FUNNEL_STAGE_LABEL` display map, and `validNextStages(from)` — the FE mirror of `isValidTransition` so the promote dropdown shows only valid targets. Daemon enforces server-side regardless; the FE filter is UX, not security.
- **`frontend/src/hooks/useFs.ts`** — `useFsList(path, {enabled})`, `useFsRead(path)`, `useFsWrite()`. The mutation invalidates `["fs", "read", path]` + the `["fs", "list"]` prefix on success.
- **`frontend/src/hooks/useFunnel.ts`** — `useFunnelList(stage)`, `useFunnelRead(stage, name)`, `useFunnelPromote()`. Promote invalidates both source and destination list keys so the board snaps to the new layout.
- **`frontend/tests/unit/fs-rpc.test.ts`** — 4 cases: list / read default-utf8 / write+error-envelope / base64 passthrough.
- **`frontend/tests/unit/funnel-rpc.test.ts`** — 5 cases: the `FUNNEL_STAGES` canonical ordering / the `validNextStages` table mirror / list+read+promote RPC round-trips against the FakeWebSocket.
- **`docs/completions/phase-6-funnel.md`** — this phase's completion doc.

### Modified

- **`proto/schemas/fs/list.json`** — was a `_todo` stub; now defines `FsListRequest` / `FsListResponse` / `FsListEntry` with `kind ∈ {file, dir, symlink}`, byte `size`, RFC-3339 `mtime`, and a top-level `oneOf` that codegen unwraps into three exported types.
- **`proto/schemas/fs/write.json`** — was a `_todo` stub; now defines `FsWriteRequest` (path + contents + encoding ∈ {utf-8, base64} + reserved `mode: "overwrite"` enum) and `FsWriteResponse` (path + size + mtime).
- **`sandbox-daemon/internal/ws/envelope.go`** — five new stable error codes: `funnel.invalid_stage`, `funnel.invalid_name`, `funnel.invalid_transition`, `funnel.not_found`, `funnel.io`. Same constants travel on the wire; the FE switches on them.
- **`sandbox-daemon/internal/fs/handler.go`** — extended with `List()` and `Write()` methods. `resolve()` got an early-return for `clean == rootClean` so `fs.list(".")` (the workspace root) is accepted; previously the `filepath.Rel` round-trip returned `.` which the old guard didn't accept. `Write()` returns `fs.not_found` if the parent dir is missing (no `fs.mkdir` yet) so the FE can show a useful message.
- **`sandbox-daemon/cmd/daemon/main.go`** — wired real handlers into the dispatch map: `fs.list` / `fs.write` replace their `NotImplemented` stubs; new `funnel.list` / `.read` / `.promote` routes bound to `funnel:r` / `funnel:rw` (already in the session-token enum since Phase 1). The funnel handler is constructed against `filepath.Join(cfg.WorkspaceRoot, "rommel")`.
- **`sandbox-daemon/internal/ws/server_test.go`** — added `funnelx` to the harness, broadened default token scopes to include `funnel:rw`, replaced `TestFsWrite_StubReturnsNotImplemented` with 8 real fs.write/fs.list tests, and added 10 funnel tests. Reused the existing `roundTrip` / `mintToken` helpers — no harness rework. New count: 31 tests, all green.
- **`frontend/src/stores/connection.ts`** — added `daemon: DaemonConnection | null` (shared socket ref) and `selectedFile: string | null` (workspace-scoped editor state). `reset()` clears both.
- **`frontend/src/hooks/useDaemonConnection.ts`** — after `connect()` resolves, `store.setDaemon(conn)`; cleared in unmount cleanup. No other lifecycle change.
- **`frontend/src/components/filetree/FileTree.tsx`** — stub replaced with a real recursive `Node` component. Top-level mounts `useFsList(".")`; each subtree mounts its own `useFsList(path, {enabled: open})` query. Clicking a file pokes `store.selectFile(path)`; the EditorPane picks it up.
- **`frontend/src/components/editor/monaco-impl.tsx`** — went from inert welcome buffer to a real editor: subscribes to `selectedFile`, runs `useFsRead`, fills the Monaco buffer on resolve, tracks `dirty`, binds `Cmd/Ctrl+S` via `editor.addCommand(KeyMod.CtrlCmd | KeyCode.KeyS, …)`, and runs `useFsWrite` on save. Title bar shows path + `loading…` / `● modified` / `saving…` / `saved Ns ago`. Language inference covers ts/tsx/js/json/md/py/go/yaml/sh/css/html/rs/toml; rest fall back to plaintext.
- **`frontend/src/components/funnel/FunnelBoard.tsx`** — stub replaced with a six-column kanban. Each `StageColumn` runs `useFunnelList(stage)`; each `Card` shows the entry name + a "Promote ▸" dropdown filtered by `validNextStages(stage)`. Clicking a card name pokes `selectFile("rommel/<stage>/<name>")` so the entry opens in the editor.
- **`frontend/src/components/shell/Header.tsx`** — accepts an optional `children` prop so callers can inject the IDE/Funnel toggle into the header.
- **`frontend/src/app/workspaces/[id]/workspace-client.tsx`** — added an IDE / Funnel view toggle (rendered into `<Header>`). When `view === "funnel"`, the editor+terminal grid is replaced by `<FunnelBoard />`; FileTree stays visible in both modes.
- **`frontend/src/hooks/useFs.ts`** — `useFsList(path, opts?: { enabled?: boolean })` so FileTree's collapsed-subtree case reads naturally.
- **`frontend/tests/unit/connection-store.test.ts`** — extended with three new cases covering `setDaemon`, `selectFile`, and `reset()` clearing the Phase-6 fields.

### Removed / Moved

- **`docs/executing/phase-6-funnel-plan.md`** → **`docs/archive/phase-6-funnel-plan.md`** — same archival move Phases 3 / 4 / 5 made on completion. The `executing/` folder is reserved for in-flight plans.

### Decisions

- **`rommel/` (visible), not `.rommel/` ✅** — confirmed with user. Kanban-on-disk is user-facing content. The daemon hard-codes the funnel root as `<WorkspaceRoot>/rommel` — convention, not configuration.
- **Kebab-case stage folder names ✅** — `triage`, `plans`, `next-up`, `executing`, `completions`, `archive`. Display names ("Next Up") get formatted in `FUNNEL_STAGE_LABEL`; on disk it's `next-up`.
- **Transition table: linear forward + archive-from-anywhere ✅** — encoded once on each side and identical by construction: `sandbox-daemon/internal/funnel/handler.go::isValidTransition` and `frontend/src/lib/funnel.ts::validNextStages`. Daemon enforces server-side; FE filter is UX only.
- **Daemon connection sharing via the Zustand store ⚠ refined** — alternative was a React context. Store extension wins: minimal diff, no provider plumbing, class-instance ref equality holds because `setDaemon` fires exactly once per mount. Hooks gate on `status === "ready"` so RPCs don't fire against a half-built socket.
- **Editor: one-file-at-a-time, Cmd+S save, no dirty-confirm ✅** — no tabs in v1. Clicking another file replaces the buffer outright. Save is an explicit act. Multi-file tabs + "discard unsaved" modal are follow-ups; they don't change the wire contract.
- **`fs.list` returns kind ∈ {file, dir, symlink}, no recursion ✅** — one `fs.list` per opened directory; tree state lives in React. Hidden files included by default — they matter in an IDE.
- **`fs.write` is overwrite-only; `mode` field reserved ✅** — v1 sends full file contents. The `mode: "overwrite"` enum is reserved so `mode: "create"` / `mode: "append"` can land without breaking the wire. `fs.patch` deferred.
- **Funnel card content read is on-demand, not eager ✅** — `FunnelBoard` shows names only; the body loads via `funnel.read` when the user opens the card. Eager fetch would have been 6 × N RPCs per board mount.
- **NEW — `fs.list(".")` root-case fix ⚠ refined** — the original `fs.Handler.resolve()` rejected the workspace root itself because `filepath.Rel(root, root) == "."` failed the previous guard. Phase 6 adds an early-return `if clean == rootClean { return clean }`. Escape-detection logic unchanged for every other path.
- **NEW — Backend already authorised funnel scopes ✅** — `ROMMEL_DEFAULT_SCOPES` (Phase 4 §0) already contained `funnel:rw`; tokens minted today carry it without any backend change. No backend touched this phase.

### Cross-cutting: the scaffolding era closes here

Phases 1–5 stood the substrate up. Phase 6 is the first phase that *uses* it rather than building it. Properties earned:

- **One additive seam per new primitive.** Adding `fs.stat` or `git.status` from here is five small steps: schema, codegen, Go handler, dispatch entry, TS wrapper + hook. The Pattern-B auth loop, the WS transport, the envelope encode/decode, the request-correlation, reconnect-and-refresh — none of those need to be touched.
- **Tests scale linearly with primitives.** The `server_test.go` harness already mints tokens, dials WS, round-trips envelopes, and asserts error codes; every new primitive adds 4–8 test cases against that same harness. The FE's `FakeWebSocket` pattern in `tests/unit/*-rpc.test.ts` is the same shape — same ergonomics on both sides of the wire.
- **The funnel is now self-hosting.** The repo's own Phase plans live under `rommel/executing/` while in flight and `rommel/archive/` on completion. Promoting a plan is a real `funnel.promote` call against the dogfooded directory.

### Verification

```sh
# Daemon unit suite — 47 cases, hermetic, no network:
make -C sandbox-daemon test
# expected: internal/config and internal/ws PASS; new TestFs{List,Write}_* and TestFunnel* cases all green

# Frontend unit suite — 27 cases, jsdom + FakeWebSocket, no network:
pnpm --filter ./frontend test:unit
# expected: 5 files, 27 tests — connection-store(8), daemon(8), fs-rpc(4), funnel-rpc(5), auth(2)

# Frontend lint:
pnpm --filter ./frontend lint
# expected: clean exit

# Three-terminal end-to-end:
#   T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
#   T2: docker compose -f backend/compose.yaml up -d postgres && make -C backend migrate run
#   T3: pnpm --filter ./frontend dev
# Browser:
#   - Sign in → open the dev workspace
#   - File tree mounts the repo root → drill into rommel/executing/phase-6-funnel-plan.md
#   - Editor opens it; edit; Cmd+S → "saved 0s ago"
#   - Toggle Funnel → six columns render
#   - Promote ▸ → Completions on the plan card; board snaps to the new layout
```

Captured this session: daemon `go test ./...` all green (cached + fresh runs), frontend `pnpm test:unit` green at 5 files / 27 tests / 489 ms, `pnpm lint` clean. TypeScript `typecheck` reports zero errors in any Phase-6-touched file; the remaining 19 `tsc` complaints sit in Phase-5 files and are part of the pre-existing 0.1.5 "named carryover" (Supabase cookie typing, RequestInit body typing).

### Next

The substrate is done. Every entry in [`docs/primitives.md`](./primitives.md) is now an additive PR against five seams. Candidates ordered by leverage:

1. **`pty.open` / `pty.input` / `pty.output`** — lights up the terminal pane. xterm UI already in place from Phase 5; only daemon-side PTY + WS event-stream wiring is new. Prerequisite for "run agents in the terminal" (Vision Layer 3).
2. **`fs.watch`** — solves the editor / on-disk drift gap called out in this phase's plan §9.3.
3. **`git.*` structured primitives** — `git.status`, `git.diff`, `git.commit`, `git.branch.*`. Shell out internally; return parsed structured data.
4. **`fs.mkdir` / `fs.move` / `fs.delete`** — fill in the rest of the fs domain; closes the v1 file-tree story.

Carryover follow-ups (small, network-bound): live Playwright spec for the new flows (`tests/e2e/funnel.spec.ts`), first Vercel deploy of the upgraded shell, Phase-5 typecheck cleanup bundled with that deploy.

---

## 0.1.5 — 2026-05-13

**Phase 5 — `frontend/` Next.js IDE shell.** Completion doc: [`docs/completions/phase-5-frontend.md`](./completions/phase-5-frontend.md). Plan (archived on completion): [`docs/archive/phase-5-frontend-plan.md`](./archive/phase-5-frontend-plan.md).

Status: ✅ Code authored to plan end-to-end. New `frontend/` subtree on Next.js 15 + React 19 + Tailwind 4 + App Router. The `lib/daemon.ts` WS wrapper owns the wire (envelope encode/decode against `@rommel/proto`, request/response correlation by `id`, 5-attempt exponential-backoff reconnect, three-way token refresh: close-1008 / invalid_token / wall-clock at `exp - 30s`). The browser side of the Pattern-B auth loop is now committed: `useCreateSession()` calls `POST /workspaces/:id/sessions` on the backend, `DaemonConnection` opens `ws(s)://…/ws?token=…` directly to the daemon, `system.ping` round-trips, the `ConnectionPill` flips to `ready`. Hermetic Vitest suite (8 + 5 + 2 cases) authored against a fake-WebSocket test double; Playwright integration-gate spec authored that mirrors the Phase-4 Python round-trip from Chromium. The full pipeline runs in CI under an opt-in `vars.RUN_E2E == 'true'` job that brings up postgres + backend + daemon + frontend in one shot. **Live first execution — `pnpm install` lockfile resolution, `next build`, the live Playwright pass, first `vercel link` + prod deploy — is the named carryover for a network-enabled session, exactly the shape Phase 4 deferred its `fly deploy`.**

### Added

- **`frontend/`** subtree:
  - `package.json` — `@rommel/frontend`; pinned `next@15.0.0`, `react@19.0.0`, `react-dom@19.0.0`, `tailwindcss@^4.0.0-beta.3`, `@supabase/ssr@^0.5.2`, `@supabase/supabase-js@^2.45.4`, `@tanstack/react-query@^5.59.0`, `zustand@^5.0.0`, `@monaco-editor/react@^4.6.0` + `monaco-editor@^0.52.0`, `@xterm/xterm@^5.5.0` + `@xterm/addon-fit@^0.10.0` + `@xterm/addon-web-links@^0.11.0`, `zod`, `clsx`, `tailwind-merge`, `class-variance-authority`, `@radix-ui/react-slot`, `lucide-react`, `server-only`. Dev: typescript, eslint (flat), `typescript-eslint`, react / react-hooks / jsx-a11y plugins, prettier + `prettier-plugin-tailwindcss`, vitest + jsdom + `@vitejs/plugin-react`, `@testing-library/react`, `@playwright/test`. Node `>=20`, pnpm 9.
  - `tsconfig.json` — strict + `noUncheckedIndexedAccess` + `exactOptionalPropertyTypes`; path alias `@/* → src/*`.
  - `next.config.mjs` — `transpilePackages: ["@rommel/proto"]` (risk 4.2; the TS proto client ships raw `.ts`).
  - `tailwind.config.ts` — content globs, `darkMode: "class"`, `pill.*` color tokens for the four-state connection indicator.
  - `eslint.config.mjs` — flat config; `no-restricted-imports` blocks `**/lib/env.server` from `src/**` (risk 4.5 mechanical guard).
  - `middleware.ts` — runs on `/workspaces/:path*`; signed-out users get redirected to `/sign-in?next=<url>` before any RSC fetch hits the backend.
  - `src/app/`:
    - `layout.tsx` (root + `<Providers>` client wrapper for `QueryClientProvider`).
    - `page.tsx` — RSC workspace picker; pulls Bearer from the supabase-ssr cookie via `getAccessTokenFromCookies(cookies())`.
    - `sign-in/page.tsx` — Supabase magic-link OTP form.
    - `auth/callback/route.ts` — OAuth code-exchange handler (`exchangeCodeForSession` → cookie session → redirect to `next`).
    - `workspaces/[id]/page.tsx` (RSC: fetches workspace metadata, hands off to client) + `workspace-client.tsx` (client island: grid layout + `useDaemonConnection` mount).
  - `src/components/`:
    - `shell/Header.tsx`, `StatusBar.tsx`, `ConnectionPill.tsx` (`data-testid` + `data-status` for the integration gate), `WorkspaceCreateButton.tsx`.
    - `ui/button.tsx` (shadcn-style; Slot + cva). Other shadcn primitives deferred until a real consumer needs them (Phase 6+).
    - `filetree/FileTree.tsx` (stub), `funnel/FunnelBoard.tsx` (stub).
    - `editor/EditorPane.tsx` + `monaco-impl.tsx` — `dynamic({ ssr: false })` boundary, `vs-dark` markdown welcome buffer.
    - `terminal/TerminalPane.tsx` + `xterm-impl.tsx` — dynamic xterm + fit + web-links addons; `ResizeObserver` calls `fit.fit()` on container resize.
  - `src/lib/`:
    - `daemon.ts` — ★ `DaemonConnection` class: `connect()`, `rpc(type, payload)`, `subscribe(type, handler) → unsubscribe`, `close()`; five-state machine (`connecting`/`ready`/`reconnecting`/`failed`/`closed`); exponential backoff 250 ms→5 s with 5-attempt ceiling; `refreshAndReopen()` triggered by close-1008/4401, `error.code === "invalid_token"`, or wall-clock `exp - 30 s`; `appendTokenIfMissing()` so the daemon URL `?token=…` append is wrapper-owned not caller-owned.
    - `auth.ts` — three Supabase factories (`createBrowserClient`, `createServerClient(cookies())`, `createMiddlewareSupabaseClient(req,res)`) + `getAccessTokenFromCookies()` helper for RSCs.
    - `api.ts` — typed `fetch` wrapper; throws `ApiError(status, body)` on non-2xx so TanStack Query's `retry` can switch on `/^API 401/` and bail rather than retry through middleware-bounces.
    - `env.client.ts` (zod over `NEXT_PUBLIC_*`) + `env.server.ts` (`import "server-only"` + zod) — split per risk 4.5.
    - `query.ts` (one `QueryClient` per request on server, one per tab on browser), `utils.ts` (`cn()`, `invariant()`).
  - `src/hooks/`:
    - `useDaemonConnection(workspaceId)` — bridges React lifecycle to `DaemonConnection`; `POST /sessions` → store the response → `connect()` → `rpc("system.ping", {})` → store the pong; tears down on unmount.
    - `useWorkspace.ts` — TanStack Query wrappers: `useMe`, `useWorkspaces`, `useWorkspace(id)`, `useCreateWorkspace`, `useCreateSession(workspaceId)`. Each reads the Supabase access token via `createBrowserClient().auth.getSession()` and attaches it as Bearer.
  - `src/stores/connection.ts` — Zustand store: `status`, `sessionToken`, `daemonUrl`, `expiresAt`, `lastError`, `lastPong`, plus the corresponding setters and a `reset()`.
  - `src/types/workspace.ts` — hand-rolled DTOs mirroring `backend/api/{workspaces,sessions,auth}.py`. Replace with OpenAPI-derived once `/openapi.json` is wired through `make proto`.
  - `src/styles/globals.css` — `@import "tailwindcss"`; `.monaco-host` / `.xterm-host` rules that keep the panes flush in their grid cells.
  - `tests/unit/daemon.test.ts` (8 cases against a `FakeWebSocket` test double), `connection-store.test.ts` (5), `auth.test.ts` (2 — env shape, factory surface).
  - `tests/e2e/ping.spec.ts` — ★ Phase-5 integration gate: Supabase password-grant programmatic sign-in (no magic-link email needed), navigate to `/workspaces/<id>`, assert `[data-testid=connection-pill][data-status=ready]` within 15 s. CI runs it; local dev runs it via `playwright.config.ts::webServer`.
  - `playwright.config.ts`, `vitest.config.ts`, `postcss.config.mjs`, `.prettierrc.json`, `.env.example`, `.gitignore`, `vercel.json`, `Makefile` (delegates to pnpm so root `run_if_exists` picks it up), `README.md` (layout, env table, Monaco self-host upgrade path for risk 4.7), `public/favicon.svg`.

### Modified

- **`.github/workflows/frontend.yml`** — awakened. Build job now runs `proto/codegen/ts.sh`, `pnpm install --frozen-lockfile`, lint, typecheck, `next build` (with CI placeholder `NEXT_PUBLIC_*` so `lib/env.client.ts`'s zod parse succeeds), and Vitest. New `e2e` job (gated on `vars.RUN_E2E == 'true'`) spins up the full Pattern-B stack — postgres service container, Go 1.23 build of the daemon, Poetry install + Alembic upgrade + uvicorn in background, daemon binary in background, frontend dev server in background, Playwright chromium. Mirrors the Phase-4 backend.yml integration-gate shape but spans one more process. Required CI secrets/vars are tabulated in the completion doc.
- **`package.json` (repo root)** — added `"pnpm": { "overrides": { "@types/react": "^19.0.0", "@types/react-dom": "^19.0.0" } }` so transitive `peerDeps: @types/react@^18` declarations don't shadow the React-19 typings the frontend uses (risk 4.8). Touched only the bottom of the file; no other root state changed.

### Removed / Moved

- **`docs/executing/phase-5-frontend-plan.md`** → **`docs/archive/phase-5-frontend-plan.md`** — same archival move Phase 3 and Phase 4 made on completion; `docs/executing/` is for in-flight plans only.

### Decisions

- **Next.js 15 + React 19 + App Router + Tailwind 4 ✅ confirmed.** App Router everywhere; two server components (`page.tsx` for `/`, `page.tsx` for `/workspaces/[id]`) RSC-fetch from the backend; everything else is `"use client"`. Monaco + xterm always come through `dynamic({ ssr: false })` — risk 4.1 codified as structural pattern in `EditorPane.tsx` / `TerminalPane.tsx`.
- **`@supabase/ssr` (httpOnly-cookie sessions) over plain `@supabase/supabase-js` ✅ confirmed.** Three factories in `lib/auth.ts` for the three render contexts (browser / server / middleware); `getAccessTokenFromCookies(cookieStore)` is the helper RSCs use to attach `Authorization: Bearer` to backend calls without leaking the JWT to client code.
- **`@monaco-editor/react` v4 with the CDN loader ✅ confirmed.** Self-host upgrade path (risk 4.7) documented in `frontend/README.md` and deferred — bundling Monaco's workers through Turbopack is its own minefield.
- **`@xterm/*` v5 with fit + web-links addons ✅ confirmed.** WebGL/canvas renderers and `addon-search` are Phase-N additions.
- **Hand-rolled `lib/daemon.ts`, no WS library ✅ confirmed — the load-bearing piece.** `partysocket`/`socket.io`/`nanostream` all rejected: either no leverage over `id`-correlation, or wire framing the daemon doesn't speak. The wrapper is framework-agnostic (no React, no TanStack) so it's testable in pure Vitest with a fake WebSocket; the React adapter is `useDaemonConnection`.
- **TanStack Query for HTTP + Zustand for client-only state ✅ confirmed.** No Redux, no Jotai. The query client's `retry` function bails on `/^API 401/` so middleware-bounced calls don't loop.
- **NEW — Minimal shadcn surface (just `Button`) ⚠ refined.** Plan §0.7 named six components; v1 ships only Button (the only one used by `WorkspaceCreateButton` and `sign-in/page.tsx`). The rest land when a real consumer needs them (Phase 6+).
- **NEW — Split `env.client.ts` / `env.server.ts` ⚠ refined.** Plan §step-1 sketched a single `lib/env.ts`. Risk 4.5's catch — server-only secrets only fail at runtime when imported on the server — is closed structurally: `env.server.ts` has `import "server-only"` (Next's compile-time trip-wire) and the ESLint `no-restricted-imports` rule (lint-time guard). Together they make the leak path unreachable.
- **NEW — Programmatic Playwright sign-in via Supabase password-grant ⚠ refined.** Plan §step-6 said "magic-link a seeded user." Playwright can't drive an inbox; the spec POSTs to Supabase's password-grant endpoint, plants the `sb-<project>-auth-token` cookie that `@supabase/ssr` reads, and proceeds. CI secrets table covers the seeded-user setup.
- **NEW — `?token=…` URL append owned by the wrapper, not the caller ⚠ refined.** `DaemonConnection({ url, token })` + `appendTokenIfMissing()` so callers pass the daemon URL straight from the backend and refresh cycles can rewrite `token` without rewriting the URL.

### Cross-cutting: Pattern-B auth loop is now end-to-end browser-driven

- The same EdDSA JWT shape the Phase-4 integration gate proved against Python's `websockets` is now consumed by Chromium. `types/workspace.ts::SessionResponse` matches `backend/api/sessions.py::SessionOut` field-for-field (`daemon_url`, `token`, `expires_at`); `DaemonConnection` reads only those three.
- Capability scoping is enforced from the same call-site: `useCreateSession` → backend's `mint_token(..., scopes=settings.default_scopes)` → `ROMMEL_DEFAULT_SCOPES` env. The browser does not get to pick scopes; the v1 contract is "whatever the broker decided at mint time."
- Refresh closes the TTL loop. The Phase-4 plan risk 4.6 — "5-minute TTL is shorter than a real editor session" — is now owned by `DaemonConnection.refreshAndReopen`. One `POST /workspaces/:id/sessions` + a transparent socket re-open; the UI sees `ready → reconnecting → ready` for one frame.
- **Production reachability remains the named gap (risk 4.4).** `https://rommel.vercel.app` cannot open `ws://localhost:7777`, and `wss://<wid>.vm.rommel-workspaces.internal:7777` is Fly-private — the browser cannot resolve it. The dev story works; the prod cutover needs the Phase-5.5 Flycast `wss://` proxy.

### Verification

```sh
# Hermetic unit suite (no daemon, no backend, no network):
cd frontend
pnpm install
pnpm test:unit
# expected: daemon.test.ts (8), connection-store.test.ts (5), auth.test.ts (2) — all green

# Local boot:
pnpm dev    # http://localhost:3000

# Three-terminal integration gate (recipe in docs/completions/phase-5-frontend.md §Verification):
#   T1: make -C sandbox-daemon run-local  (ed25519 pubkey baked in env)
#   T2: docker compose up -d postgres && make -C backend migrate run
#   T3: pnpm --filter ./frontend dev
# Browser: sign in → create workspace → open it → ConnectionPill flips to "ready"
#          DevTools shows WS frame to ws://localhost:7777/ws?token=... and the pong response.
```

The **live first execution** of `pnpm install` (which writes the lockfile), `next build`, and the Playwright spec is the carryover — same shape as Phase 3's deferred `fly machine run` and Phase 4's deferred `fly deploy`. Each one is a single network-enabled session away.

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §6+: the next phase opens the first wave of *real* daemon primitives lighting up the IDE shell — either the `.rommel/` funnel UI (Layer 2 of `docs/vision.md`) or the `fs.list`/`fs.read`/`fs.write` wiring that makes the file tree and editor real. Both are unblocked: every primitive the daemon implements is one `DaemonConnection.rpc(type, payload)` call away.

Carryover follow-ups (small, do-anywhere): first `vercel link` + prod deploy; flip `vars.RUN_E2E` once Supabase test-user secrets land; **Phase-5.5 Flycast `wss://` proxy** for production WS reachability (risk 4.4 — the load-bearing prod-cutover item); replace `types/workspace.ts` with OpenAPI-derived types once `/openapi.json` is published via `make proto`.

---



**Phase 4 — `backend/` FastAPI control plane.** Completion doc: [`docs/completions/phase-4-backend.md`](./completions/phase-4-backend.md). Plan: [`docs/executing/phase-4-backend-plan.md`](./executing/phase-4-backend-plan.md).

Status: ✅ Integration gate green locally. The Python `services.session_broker.mint_token()` produces an EdDSA JWT that the actual `sandbox-daemon` binary (built off Phase-2 source) accepts on `/ws?token=…` and round-trips `system.ping` against; a wrong-`wid` token is rejected at the WS upgrade with HTTP 401. The full Pattern-B auth loop — signer → verifier — is operational end-to-end. Fly-side `fly deploy` from `backend/` is deferred to first cloud deploy (no `fly auth login` in this session; recipe is in `backend/README.md`).

### Added

- **`backend/`** subtree:
  - `pyproject.toml` — Poetry; FastAPI, uvicorn, pydantic, pydantic-settings, SQLAlchemy 2.0 (`+asyncio`), asyncpg, psycopg 3 (sync, for Alembic only), Alembic, `PyJWT[crypto]`, cryptography, httpx, structlog, cachetools. Dev: pytest, pytest-asyncio, Ruff, websockets. Python `^3.12`.
  - `api/` — `main.py` (app factory + lifespan), `config.py` (`Settings` with `env_prefix=ROMMEL_`, `@lru_cache get_settings()`, `alembic_url` property that strips `+asyncpg`), `deps.py` (`get_db` / `get_db_for_user` with `SET LOCAL rommel.user_id` in a `session.begin()` block / `get_current_user`), `health.py` (GET /healthz), `auth.py` (GET /auth/me, POST /auth/logout), `workspaces.py` (POST/GET/DELETE CRUD), `sessions.py` (POST /workspaces/:id/sessions; refresh stub returns 501), `policy.py` (GET /policy — empty bundle stub).
  - `services/` — `auth.py` (Supabase JWKS RS256 validator + `UserClaims`; `TTLCache` for JWKS with one-shot retry on `kid` miss to handle key rotation), `session_broker.py` (`mint_token()`; iat/exp derived from a single `datetime.now(UTC)` per risk 4.5), `workspace_lifecycle.py`, `fly_orchestrator.py` (httpx client over Fly Machines API; empty-token "dev stub" mode returns deterministic `stub-<hex>` machine ids; `metadata.label = wid` so `.internal` DNS resolves).
  - `repositories/` — `base.py` (Protocols + dataclasses), `postgres/engine.py` (per-URL-cached async engine + session_factory), `postgres/users.py` (upsert via `INSERT … ON CONFLICT DO UPDATE SET supabase_sub = EXCLUDED.supabase_sub … RETURNING *` so RETURNING fires on conflict), `postgres/workspaces.py` (CRUD).
  - `models/tables.py` — SQLAlchemy 2.0 Core metadata for `users` + `workspaces`, with a stable naming convention so Alembic autogen doesn't drift.
  - `alembic.ini` (sqlalchemy.url left blank), `alembic/env.py` (reads `Settings.alembic_url`; uses sync driver per risk 4.1), `alembic/versions/0001_init.py` — tables, `app_user` Postgres role (idempotent `DO $$ … IF NOT EXISTS …`), grants, `ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY` (defense-in-depth so even the owner is RLS-bound), and four policies (`users_self_*`, `workspaces_owner_*`) keyed off `current_setting('rommel.user_id', true)`.
  - `policy/rules.py` — `current_bundle()` returns `{"version": 0, "rules": []}` for v1.
  - `tests/` — `conftest.py` (session-scoped Ed25519 keypair, `test_settings` monkeypatched env, FastAPI `client` with `get_current_user` / `get_db_for_user` overridden, `daemon_subprocess` fixture that builds `sandbox-daemon/dist/sandbox-daemon` on first use and spawns it on a free port, `require_postgres` skip-gate), `test_health.py`, `test_auth.py` (hermetic JWKS happy-path + expired + 401-without-bearer), `test_sessions.py` (★ integration gate + wrong-wid rejection + claim-shape vs `session-token.json` schema + single-`now()` invariant + Ed25519 PEM smoke), `test_workspaces.py` (orchestrator stub mode + policy endpoint).
  - `Makefile` — `bootstrap` / `run` / `lint` / `test` / `build` (no-op; image handles packaging) / `migrate` / `migrate-new` / `deploy`.
  - `Dockerfile` — `python:3.12-slim` + `curl` + Poetry; layered dep install; `uvicorn api.main:app --host 0.0.0.0 --port 8080`. Build context is the subtree, not repo root (no proto codegen needed at backend build time — the Python client is published as a wheel; reused via direct import in v1).
  - `fly.toml` — `app = "rommel-backend"`, `internal_port = 8080`, `http_service.checks` against `/healthz`, `[deploy] release_command = "alembic upgrade head"` (one transient machine, blocks rollout — risk 4.6/§0.7 of the plan: never autogen on boot).
  - `compose.yaml` — `postgres:16-alpine` with `pg_isready` healthcheck.
  - `.env.example` — every `ROMMEL_*` env documented.
  - `README.md` — layout, dev recipe, deploy recipe, full risk-mitigation table.

### Modified

- **`.github/workflows/backend.yml`** — woke up. Adds a `postgres:16-alpine` service container (RLS won't run on SQLite), installs Go 1.23, builds the daemon binary for the integration gate, installs Poetry, runs `alembic upgrade head` + `ruff check` + `pytest`. Path-filters extended to include `sandbox-daemon/**` (because the integration gate depends on the daemon binary).
- **`.github/workflows/daemon.yml`** — `actions/setup-go@v5` `go-version`: `"1.22"` → `"1.23"`. This is the follow-up the Phase-3 completion doc flagged (`proto/codegen/go.sh` invokes `go-jsonschema@v0.18.0`, which requires Go ≥ 1.23). Comment added in-file pointing at `phase-3-workspace-image.md` for context.
- **`.github/workflows/proto.yml`** — same setup-go bump for the same reason.
- **Top-level `Makefile`** — added `migrate` target (delegates to `backend/`); listed it in `help`. The existing `run_if_exists` helper keeps `build`/`lint`/`test` working unchanged.

### Removed

None.

### Decisions

- **SQLAlchemy 2.0 Core + asyncpg + Alembic.** Plan 0.1 confirmed as-is. Repositories use `select() / insert() / delete()` expressions; no ORM session, no string SQL. Async engine cached per-URL in `repositories/postgres/engine.py`.
- **PyJWT (`^2.9`) with the `[crypto]` extra, not python-jose.** Plan 0.2 confirmed. PyJWT 2.x is actively maintained, audit-friendly (one module vs python-jose's `jwt/jws/jwk/jwe` quartet), and `algorithm="EdDSA"` produces a header compatible with the daemon's golang-jwt `WithValidMethods([]string{"EdDSA"})`.
- **PEM env-var for the signing key (`ROMMEL_TOKEN_PRIVKEY`), not a mounted file.** Plan 0.3 confirmed. Symmetric with the daemon's `ROMMEL_TOKEN_PUBKEY`, so rotation is one mental model on both sides.
- **`{daemon_url, token, expires_at}` response with a template-driven URL.** Plan 0.4 confirmed. `ROMMEL_DAEMON_URL_TEMPLATE` interpolates `{wid}`; prod uses `wss://{wid}.vm.rommel-workspaces.internal:7777/ws`, dev uses `ws://localhost:7777/ws`. Business logic doesn't change between environments.
- **pydantic-settings + Ruff.** Plan 0.5 confirmed. One `Settings` class, one cached factory; Ruff replaces black/flake8/isort.
- **Ephemeral Postgres for tests, NOT SQLite/Supabase-shadow.** Plan 0.6 confirmed. SQLite can't run RLS at all; the first migration enables it. Tests skip cleanly if Postgres isn't reachable so the non-DB unit suite runs anywhere.
- **`[deploy] release_command = "alembic upgrade head"`, never on FastAPI boot.** Plan 0.7 confirmed. App boot does no migration work. The release machine is transient and blocks rollout on non-zero exit. The startup-time `alembic heads == DB version` check is deferred (risk vs blast-radius: complicates dev-without-Postgres path more than it buys in v1).
- **NEW — RLS hardening: `FORCE ROW LEVEL SECURITY` + a dedicated `app_user` role.** Risk 4.2 is the trapdoor Postgres opens by default (table owners are RLS-exempt). The migration installs `app_user` with minimum grants, and *every* table also gets `FORCE ROW LEVEL SECURITY` so even if a misconfigured client connects as the schema owner, policies still fire. Defense in depth, cheap to maintain.
- **NEW — UsersRepo upsert via `INSERT … ON CONFLICT DO UPDATE SET <col>=EXCLUDED.<col>`.** `DO NOTHING` swallows `RETURNING` for the conflict path; the no-op `DO UPDATE` is the cleanest way to make Postgres return the existing row. No `email` overwrite — keeps a user-edited value safe.

### Cross-cutting: Pattern-B auth loop is now end-to-end operational

Phase 1 committed the contract. Phase 2 made the daemon verify it. Phase 3 baked the verifying pubkey into the image. Phase 4 ships the signer. The properties earned by this phase:

- **Wire compatibility is proven.** The integration-gate transcript captures a real broker→daemon round-trip: the daemon ingests a PyJWT-emitted `EdDSA` JWT and `system.ping` returns the matching response frame. The wrong-`wid` negative case rejects with HTTP 401 at the WS upgrade, as `sandbox-daemon/internal/auth/token.go::Verify` should.
- **Rotation is tightly coupled to image SHA.** Backend `ROMMEL_TOKEN_PRIVKEY` is a Fly secret; the matching pubkey is baked into the image at `/etc/rommel/token.pubkey`. Rotating either half requires re-deploying that half — they cannot drift apart without breaking the next session creation, which is the property Phase 3 Decision 0.2 was designed for.
- **Capability scoping is enforced wire-to-wire.** The default scope vocabulary (`ROMMEL_DEFAULT_SCOPES=fs:rw,pty:rw,git:rw,funnel:rw,policy:r`) is what the broker stamps onto fresh tokens; the daemon's per-route `RequiredScope` table is the matching enforcement point. Both halves cite the same enum (`proto/schemas/session-token.json`).

### Verification

```sh
# Hermetic Python smoke (signer + claim-shape; runs without Postgres or daemon):
cd backend
poetry install
poetry run pytest -q tests/test_health.py tests/test_auth.py tests/test_workspaces.py \
                    tests/test_sessions.py::test_broker_claim_shape_matches_proto_schema \
                    tests/test_sessions.py::test_broker_signature_verifies_with_public_key \
                    tests/test_sessions.py::test_broker_uses_single_now_for_iat_and_exp

# Full integration gate (needs Go toolchain + Postgres):
docker compose up -d postgres
poetry run alembic upgrade head
poetry run pytest -q tests/test_sessions.py::test_broker_signs_token_daemon_accepts \
                    tests/test_sessions.py::test_daemon_rejects_token_with_wrong_wid
```

The integration-gate transcript captured live in this session (full output in [`docs/completions/phase-4-backend.md`](./completions/phase-4-backend.md) §Verification):

```
daemon up on :53605 (wid=smoke-2375143b)
INTEGRATION GATE PASS — backend signs → daemon verifies → ping round-trips
frame: { "kind": "response", "type": "system.ping", "id": "...", "payload": { "ok": true, "ts": "..." } }
wrong-wid: rejected: InvalidStatus: server rejected WebSocket connection: HTTP 401
```

Deferred: first `fly deploy` of `rommel-backend` (needs `fly auth login` + `fly apps create`; recipe in `backend/README.md`).

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §5: **`frontend/`** — the browser IDE. Newly unblocked: real `POST /workspaces/:id/sessions` to call, fixed `{daemon_url, token, expires_at}` response shape, the daemon's `/ws?token=…` upgrade reachable from the browser with the token the backend just minted. Phase-5 is the last scaffolding phase before the Layer-2 funnel UI work begins.

Carryover follow-ups (small, do-anywhere): first `fly deploy` of the backend; implement `POST /sessions/:id/refresh` once frontend sessions need to outlive the 5-minute TTL; wire workspace `status` transitions through the orchestrator's start/stop callbacks.

---

## 0.1.3 — 2026-05-13

**Phase 3 — `workspace-image/` Fly Machine image.** Completion doc: [`docs/completions/phase-3-workspace-image.md`](./completions/phase-3-workspace-image.md). Plan: [`docs/executing/phase-3-workspace-image-plan.md`](./executing/phase-3-workspace-image-plan.md).

Status: ✅ Local image build, smoke test, and signal-handling all green. `make -C workspace-image build` produces `rommel-workspaces:<git-sha>` from repo-root context in ~25 s warm (~110 s cold); compressed registry size **66 MiB**. The image boots under `tini`, the entrypoint loads the EdDSA pubkey from `/etc/rommel/token.pubkey` into `ROMMEL_TOKEN_PUBKEY`, fails fast on missing `ROMMEL_WID`, and forwards SIGTERM to the daemon's graceful shutdown (sub-second drain). Fly-side `fly machine run` cold-start measurement is the one verification deferred — needs `fly auth login`, recipe baked into `workspace-image/README.md`.

### Added

- **`workspace-image/`** subtree:
  - `Dockerfile` — multi-stage: `golang:1.23` builder regenerates the proto Go client and compiles a static `-trimpath -ldflags="-s -w"` daemon binary; runtime stage is `debian:stable-slim` + `apt(ca-certificates curl git tini)` + daemon binary + baked `rootfs/`. Build context is the repo root.
  - `fly.toml` — `app = "rommel-workspaces"`, `internal_port = 7777`, **no `[[services]]`** (internal Flycast/`.internal` only), **no volumes** (the backend attaches one per workspace via the Machines API). `[[restart]] policy = "on-failure"`.
  - `Makefile` — `build` / `push` / `run-local` / `clean`. Same `IMAGE=… TAG=…` env override pattern as the daemon's Makefile.
  - `.gitignore` — local-only pubkey overrides (`*.pubkey.local`, `*.pem.local`).
  - `rootfs/etc/rommel/daemon.env.example` — documents every `ROMMEL_*` env the daemon reads.
  - `rootfs/etc/rommel/token.pubkey.example` — real Ed25519 PEM committed for dev builds; the matching private key was generated in `/tmp/`, used only to derive the pubkey, then deleted in the same `openssl` step, so the dev verifier is intentionally unrecoverable.
  - `scripts/build.sh` — `cd $(git rev-parse --show-toplevel)` then `docker build -f workspace-image/Dockerfile ... .` with `--build-arg ROMMEL_TOKEN_PUBKEY_FILE`. `TAG_LATEST=true` opt-in for `:latest`.
  - `scripts/push.sh` — `flyctl auth whoami` gate, `flyctl auth docker` credential install, then `docker tag` + `docker push` to `registry.fly.io/rommel-workspaces:<tag>`.
  - `scripts/entrypoint.sh` — `set -euo pipefail` bash; loads the PEM into `ROMMEL_TOKEN_PUBKEY` (the daemon parses PEM contents, not a file path); fails fast on missing `ROMMEL_WID`; `exec`'s the daemon under tini.
  - `README.md` — full build / smoke / push / cold-start recipe + gotchas (build-context, `.dockerignore` location, pubkey rotation, no public services).
- **`.dockerignore`** at the repo root — new file written for `workspace-image/`'s build context. Sweeps out `.git/`, `.github/`, `.claude/`, `.rommel/`, `docs/`, `frontend/`, `backend/`, `infra/`, all `node_modules/`, `.next/`, `.venv/`, generated proto clients, env files. Documented as the canonical ignore for any future Dockerfile built from repo root.
- **`.github/workflows/workspace-image.yml`** — path-filtered on `workspace-image/**`, `sandbox-daemon/**`, `proto/**`, `.dockerignore`, and the workflow itself. Gates on `workspace-image/Dockerfile` existing (same skip-when-absent pattern as `daemon.yml`/`frontend.yml`/`backend.yml`/`proto.yml`). PR runs `scripts/build.sh` with `TAG_LATEST=false`; `push` to `main` additionally runs `superfly/flyctl-actions/setup-flyctl` + `scripts/push.sh` with `FLY_API_TOKEN` from secrets and `TAG_LATEST=true`.

### Modified

- **Top-level `Makefile`** — added `workspace-image` to the `build` and `clean` target lists via the existing `run_if_exists` helper. `lint`/`test` deliberately untouched (the image has neither — CI builds it instead).
- **`sandbox-daemon/README.md`** — replaced the "Building the Docker image" section with a pointer to `workspace-image/`. Inner-loop dev (`make run-local` on Go source) is unchanged.

### Removed

- **`sandbox-daemon/Dockerfile`** — per Decision 0.1 of the Phase-3 plan. The workspace-image Dockerfile is now the only Dockerfile in the repo. Keeping a near-duplicate in `sandbox-daemon/` would have diverged the moment one was updated without the other; the daemon's local-dev path doesn't need Docker.

### Decisions

- **Single Dockerfile, in `workspace-image/`.** The daemon's binary is built from source inside `workspace-image/Dockerfile`'s build stage. No second Dockerfile, no cross-Dockerfile `FROM` plumbing.
- **EdDSA pubkey baked as a file via `ARG ROMMEL_TOKEN_PUBKEY_FILE`.** PEM lives at `/etc/rommel/token.pubkey`; entrypoint exports its contents into `ROMMEL_TOKEN_PUBKEY` before `exec`'ing the daemon. Rotation requires a rebuild — intentional, so tokens can never outlive the deploy that minted their verifier.
- **No `[[services]]` in `fly.toml`.** Workspaces are reachable only via `.flycast` / `.internal` DNS on port 7777. If `0.0.0.0` exposure ever shows up here, the EdDSA scope-gate becomes the *last* line of defense rather than defense-in-depth.
- **`ROMMEL_WORKSPACE_ROOT=/workspace` as Dockerfile `ENV` + `WORKDIR /workspace`.** Pairs cleanly with Fly volumes (attached over the same path per workspace by the backend) and lets bare `docker run` work without a volume mount.
- **Repo-root `.dockerignore`.** Docker only reads `<context-root>/.dockerignore`; per-Dockerfile ignores would require BuildKit-only extensions we don't want. Future Dockerfiles built from repo-root context should extend it, not shadow it.
- **Tag by git SHA; `:latest` on main only.** PR builds never tag `:latest`; `TAG_LATEST=true` is an opt-in flag the CI sets only on `push` to `main`.
- **Builder bumped to `golang:1.23`.** The Phase-3 plan and the deleted `sandbox-daemon/Dockerfile` both used `golang:1.22`. Upstream `github.com/atombender/go-jsonschema@v0.18.0` (invoked by `proto/codegen/go.sh`) raised its toolchain floor to 1.23; the build failed at the codegen step until we bumped the builder. The runtime stage is unchanged; the daemon's `go.mod` declares `go 1.22` as a minimum, which a 1.23 toolchain honours. **Follow-up:** `daemon.yml` and `proto.yml` pin `setup-go@v5` `go-version: "1.22"` and will hit the same wall in CI — bump in the next PR.

### Cross-cutting: production token-pubkey baking path is live

Phase 1 settled the contract; Phase 2 made the daemon verify against it; Phase 3 closes the loop on **how the verifier reaches the daemon in production**. PEM is baked into the image layer at build time, written to `/etc/rommel/token.pubkey`, and loaded by the entrypoint. Backend signing key (Phase 4) and daemon verifying key are now provably tied to a deployed image SHA — the property we wanted from Decision 0.2.

### Verification

```sh
make -C workspace-image build                # → rommel-workspaces:<short-sha>, ~25s warm
docker image inspect rommel-workspaces:<sha> --format '{{.Size}}'   # → 69,355,305 bytes (66 MiB)

# happy-path smoke
docker run -d --rm -p 7777:7777 -e ROMMEL_WID="dev-workspace" rommel-workspaces:<sha>
curl -fsS http://localhost:7777/healthz      # → "ok" on first poll (<200ms after container start)
# daemon log line: "daemon: listening on :7777 (wid=dev-workspace, root=/workspace)"

# signal-forwarding smoke
time docker stop -t 10 <cid>                 # → 0m0.133s  (tini → daemon graceful shutdown)

# fail-fast smoke
docker run --rm rommel-workspaces:<sha>      # → "entrypoint: ROMMEL_WID is required ..." exit 1
```

Deferred: `fly machine run` cold-start measurement (needs `fly auth login`; recipe in `workspace-image/README.md` §"Deploy a machine and measure cold start").

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §4: **`backend/`** — FastAPI control plane. Newly unblocked by Phase 3: `POST /workspaces/:id/sessions` has a real verifier to mint tokens for; `services/fly_orchestrator.py`'s `create_machine` has a real image ref (`registry.fly.io/rommel-workspaces:<sha>`); the Pattern B loop (browser → backend `/sessions` → daemon WS) is now wire-realistic on the daemon side.

---

## 0.1.2 — 2026-05-12

**Phase 2 — `sandbox-daemon/` Go binary.** Completion doc: [`docs/completions/phase-2-sandbox-daemon.md`](./completions/phase-2-sandbox-daemon.md). Plan: [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §2.

Status: ✅ Single Go binary (`sandbox-daemon`) that upgrades `/ws?token=…` to a WebSocket, validates EdDSA-signed broker tokens against `protogen.SessionTokenClaims`, round-trips `system.ping`, and implements `fs.read` with a workspace-root path sandbox. Every other primitive from `primitives.md` §1 returns a `not_implemented` error envelope so the surface area is visible. 13 WS-level tests + 3 config tests, all green; `go vet` clean; static binary builds via `make build`.

### Added

- **`sandbox-daemon/`** Go module (module path `github.com/rommel-ade/rommel/sandbox-daemon`, Go 1.22):
  - `cmd/daemon/main.go` — config load, route table, `http.Server` with `/healthz` (unauthenticated) and `/ws`, graceful shutdown on SIGINT/SIGTERM.
  - `internal/config/` — env parsing (`ROMMEL_PORT`, `ROMMEL_WORKSPACE_ROOT`, `ROMMEL_WID`, `ROMMEL_TOKEN_PUBKEY` as PEM-encoded Ed25519). Fails fast with **all** errors listed (not first-fail), so an under-configured deploy gets one diagnostic, not three.
  - `internal/auth/` — `Verify(token, pub, expectedWID)` enforces `alg=EdDSA` allow-list, `iss=rommel-backend`, `aud=rommel-daemon`, `exp > now`, `wid` match; runs claims through `protogen.SessionTokenClaims.UnmarshalJSON` for required-field + scope-enum validation; ships a `HasAnyScope` helper for the dispatcher.
  - `internal/ws/` — local `Frame` wire type (with `json.RawMessage` payload) wrapping `protogen.Envelope`; gorilla upgrade; per-conn read loop; scope-gated handler dispatch; stable error-code constants (`bad_request`, `not_implemented`, `unknown_type`, `forbidden`, `internal`, `fs.not_found`, `fs.invalid_path`, `fs.io`).
  - `internal/fs/` — real `fs.read`: workspace-relative path joined to `Root`, `Clean`'d, prefix-checked via `filepath.Rel` (rejects absolute paths and `..` escapes); utf-8/base64 encoding per request; `fs.write`/`fs.list`/`fs.watch` wired but return `not_implemented`.
  - `internal/pty/` — all `pty.*` verbs return `not_implemented` (PTY lands in a later phase; `creack/pty` import deferred until it's actually needed).
  - `internal/workspace/` — `workspace.info` returns `{id, daemon_version}` from config; `Repo` omitted until git plumbing lands.
  - `Makefile` — `bootstrap`, `lint`, `test`, `build`, `run-local`, `clean`. The Go proto gen file is declared as a Make prerequisite, so `cd sandbox-daemon && make test` on a fresh clone auto-runs `proto/codegen/go.sh`.
  - `Dockerfile` — multi-stage; build context is the repo root so the daemon can see `proto/` for codegen. Output image: `debian:stable-slim` + `tini` + static daemon binary.
  - `.golangci.yml` — minimal config (errcheck/gofmt/goimports/govet/ineffassign/misspell/staticcheck/unused) with `local-prefixes` set to the module path.
  - `README.md` — env table, local-dev recipe, wire-format pointer.
- **Tests** (16 total):
  - `internal/config/config_test.go` — env happy path, missing-required-vars listing, non-dir workspace root.
  - `internal/ws/server_test.go` — full WS round-trip suite: healthz, missing/bad-signature/wrong-wid/expired-token upgrade rejections, `system.ping`, unknown primitive, `fs.read` (utf-8 + base64 + absolute-path-rejected + `..`-rejected + not-found), `fs.write` stub, insufficient-scope forbidden, malformed envelope.

### Modified

- **`.github/workflows/daemon.yml`** — added a `Regenerate Go proto client` step that runs `bash proto/codegen/go.sh` between `setup-go` and `vet`. The gen file is gitignored so CI needs to materialize it before any compile step touches `protogen`.

### Decisions

- **Module path mirrors proto's placeholder org.** `github.com/rommel-ade/rommel/sandbox-daemon` lines up with `github.com/rommel-ade/rommel/proto/clients/go`. Both flip together when the real GitHub org lands.
- **`replace ../proto/clients/go` in go.mod, not `go.work`.** Per the changelog 0.1.1 "Next" callout. A top-level `go.work` would let the replace go away — deferred to a follow-up since it's not blocking and changes a top-level invariant.
- **Local `ws.Frame` type with `json.RawMessage` payload.** Generated `protogen.Envelope` uses `interface{}` for payload (correct for JSON Schema, awkward for dispatch). The local Frame keeps the wire shape identical but lets handlers receive raw payload bytes — clean seam between codec and router.
- **`type: "system.ping"`, not `"ping"`.** The envelope schema's `type` pattern requires dotted form. `system.*` is reserved for daemon-level lifecycle (future `system.health`, `system.version`).
- **`WithValidMethods([]string{"EdDSA"})` on JWT parse.** Required to avoid `alg=none` / algorithm-confusion attacks; `jwt/v5` does not enforce a method allow-list by default.
- **Claims validated through `protogen.SessionTokenClaims.UnmarshalJSON`.** Parse → re-marshal → unmarshal pipes the bag through the schema's generated validation (required fields + scope-enum). One schema, no duplicated validation code in the daemon.
- **Path sandbox is `Clean` + `Rel` prefix check; no `EvalSymlinks`.** Confirmed with the user. Rejects absolute paths and `..` escapes; symlink-resolution is deferred until the daemon graduates from scaffolding (the daemon's own README and the completion doc both flag this).
- **Routes as a `map[string]Route`, not a switch.** Required scopes sit alongside handler functions in one screen of `cmd/daemon/main.go`. Adding a primitive is a map entry. Audit-friendly.
- **Stubs return `code: "not_implemented"`, every primitive is wired.** Every `primitives.md` §1 verb has a route entry. Clients discover the surface from the wire, not from team channels.
- **Daemon Makefile treats `proto/clients/go/gen/proto.go` as a prerequisite.** Cold-start `cd sandbox-daemon && make test` works on a fresh clone — Make calls `proto/codegen/go.sh` automatically.

### Cross-cutting: capability scoping is live

Phase 1 committed the scope vocabulary to the schema (`fs:r`, `fs:rw`, `pty:rw`, …). Phase 2 actually enforces it: `cmd/daemon/main.go::buildRoutes` binds each primitive to its required scopes (any-of), and the dispatcher returns `forbidden` if the token's `scope` array doesn't satisfy the route. The `TestFsRead_InsufficientScope_Forbidden` test confirms the gate fires for a `pty:rw`-only token trying `fs.read`.

### Verification

```sh
cd sandbox-daemon
make test                                # 16 tests, all pass
make build                               # → dist/sandbox-daemon (static binary)
make lint                                # go vet ./... clean

# Cold-start: proto gen file gets regenerated automatically
rm -rf ../proto/clients/go/gen
make test                                # → Make runs proto/codegen/go.sh, then tests pass
```

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §3: **`workspace-image/`** — Docker image that bakes the daemon binary plus baseline tools (`git`, `curl`, `ca-certificates`, `tini`), shipped to Fly's registry as the image used by the Machines API to spawn per-workspace VMs. The Dockerfile in `sandbox-daemon/` is already a working multi-stage build for the binary — the `workspace-image/` subtree wraps it into the deployable artifact (Fly app: `rommel-workspaces`).

---

## 0.1.1 — 2026-05-04

**Phase 1 — `proto/` Source-of-Truth + Codegen.** Completion doc: [`docs/completions/phase-1-proto.md`](./completions/phase-1-proto.md). Plan: [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §1.

Status: ✅ `make proto` regenerates 11 schemas → TS + Go + Pydantic v2 clients with **zero diff** on the second run. Generated Go compiles. Cross-cutting **session token contract** is now live in `proto/schemas/session-token.json`, unblocking §2 (daemon) and §4 (backend) integration.

### Added

- **`proto/schemas/`** — JSON Schema (draft 2020-12) source-of-truth. Real schemas for the proof-of-life surface area: `envelope.json` (WS wrapper), `session-token.json` (EdDSA broker JWT claims), `fs/read.json`, `pty/{open,input,output-event}.json`, `workspace/info.json`. Stub schemas for `fs/{write,list,watch-event}.json` and `pty/resize.json` so the surface area is visible.
- **Per-language codegen scripts** under `proto/codegen/`:
  - `ts.sh` — `npx --yes json-schema-to-typescript@^15`, one `.ts` per schema + auto-generated `index.ts` re-exporting all.
  - `go.sh` — `go run github.com/atombender/go-jsonschema@v0.18.0`, single `gen/proto.go` (package `protogen`) with `UnmarshalJSON` validation hooks.
  - `python.sh` — hermetic venv at `proto/codegen/.venv/` (bootstrapped on first run, version-marker-pinned), runs `datamodel-code-generator==0.31.2` → Pydantic v2 BaseModels.
- **`proto/codegen.sh`** — orchestrator that runs all three scripts. Equivalent to `make proto`.
- **Per-client packaging metadata** (committed; generated source is gitignored):
  - `proto/clients/ts/package.json` — `@rommel/proto`, pnpm workspace dep.
  - `proto/clients/go/go.mod` — `github.com/rommel-ade/rommel/proto/clients/go` (placeholder org).
  - `proto/clients/python/pyproject.toml` — `rommel-proto`, hatchling build.
- **`proto/README.md`** — how to add a schema, how to regenerate, format-choice rationale.

### Modified

- **`.gitignore`** — added `proto/codegen/.venv/` so the Python codegen venv isn't tracked.

### Removed

- `proto/schemas/funnel/.gitkeep`, `proto/schemas/git/.gitkeep` — confused `datamodel-code-generator` (warns on non-JSON files in input dirs). Directories will materialize when their first real schema lands; their existence is documented in `proto/README.md`.

### Decisions

- **JSON Schema, not Protobuf.** Daemon traffic is JSON-over-WebSocket — no binary framing layer to bolt on. Browser devtools render the wire format directly (huge for hot-path debugging). Codegen tooling on all three sides is mature. Schemas port to Protobuf field-for-field if profiling later demands it.
- **`$defs` + named subschemas + root `oneOf` for RPC shapes.** Drafting both a `$defs` block (named `FsReadRequest` / `FsReadResponse`) and a root `oneOf: [$ref, $ref]` produces clean named structs/classes in Go and Python *and* a discriminated TS union (`type FsRead = FsReadRequest | FsReadResponse`). One schema, three idiomatic outputs. Codified as the convention for future RPC schemas.
- **All codegen tools version-pinned.** `json-schema-to-typescript@^15`, `go-jsonschema@v0.18.0`, `datamodel-code-generator==0.31.2`. Reproducible CI is the whole point of this phase.
- **Hermetic Python venv beats global install.** `python.sh` bootstraps `.venv/` on first run with a `.installed-<version>` marker; bumping the version invalidates the marker and triggers a clean reinstall. `make proto` works from a fresh clone with just system Python.
- **Generated source gitignored; only metadata committed.** `proto/clients/*/{src,gen}/` are gitignored. `proto.yml` CI re-runs codegen and fails on diff — catches the "someone hand-edited the generated code" footgun.
- **Idempotency hinges on two flags.** `--disable-timestamp` (Python) kills the `# generated at <iso8601>` header; `LC_ALL=C sort -z` (TS script) kills locale-dependent file ordering. Without these, every CI run would produce a diff.

### Cross-cutting: session token contract is committed

`proto/schemas/session-token.json` settles the contract the scaffolding plan flagged as a §2/§4 prerequisite:

- **Algorithm:** EdDSA (Ed25519). Backend signs (private key from Fly secret); daemon verifies (public key baked into VM image at deploy time).
- **Claims:** `iss` (const `rommel-backend`), `sub` (user id), `aud` (const `rommel-daemon`), `wid` (workspace id), `scope` (capability list), `exp`, `iat`, `jti`. All required.
- **Scope vocabulary:** `fs:r`, `fs:rw`, `pty:rw`, `git:r`, `git:rw`, `funnel:r`, `funnel:rw`, `policy:r` — answers `primitives.md` cross-cutting Q5 (capability scoping) directly in the type system.

### Verification

```sh
make proto                              # first run: ~30s (bootstraps Python venv, fetches Go module)
cp -r proto/clients .snap
make proto                              # second run: ~3s
diff -r .snap proto/clients             # → empty (idempotent)
cd proto/clients/go && go build ./gen/...   # → exit 0
```

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §2: **`sandbox-daemon/`** — Go binary that accepts a WebSocket at `/ws?token=...`, validates `SessionTokenClaims` against an EdDSA pubkey from env, handles `ping → pong`, and implements real `fs.read` to prove the proto loop works end-to-end. Imports `github.com/rommel-ade/rommel/proto/clients/go/gen` (package `protogen`), likely via a `replace` directive in its own `go.mod` until a `go.work` lands at the repo root.

---

## 0.1.0 — 2026-05-04

**Phase 0 — Repo Root Scaffolding.** Completion doc: [`docs/completions/phase-0-scaffolding.md`](./completions/phase-0-scaffolding.md). Plan: [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §0.

Status: ✅ `make bootstrap && make lint && make build` all pass on a fresh clone. Subtrees (`frontend/`, `backend/`, `sandbox-daemon/`, `proto/`) intentionally absent — they land in later phases.

### Added

- **Toolchain pins** — `.nvmrc` (Node 20) and `.tool-versions` (Node 20.18.0, Go 1.22.8, Python 3.12.7, pnpm 9.12.0) as the single source of truth across all four toolchains.
- **Editor config** — `.editorconfig` with 2-space default, 4-space for Python/Go, tab for Makefile, and no trailing-whitespace trim on Markdown (preserves intentional double-space line breaks).
- **`.gitignore`** — covers Node (`node_modules/`, `.next/`), Python (`__pycache__/`, `.venv/`), Go (`sandbox-daemon/dist/`), deploy tooling (`.fly/`, `.vercel/`), and generated proto clients (`proto/clients/*/{src,gen}/`).
- **pnpm workspace root** — `package.json` (`"private": true`, pinned `packageManager`, engines specified, no runtime deps) and `pnpm-workspace.yaml` listing the eventual TS workspaces (`frontend/`, `proto/clients/ts/`). pnpm tolerates missing globs, so committing ahead of the dirs is safe.
- **`pnpm-lock.yaml`** — generated by `make bootstrap`.
- **Top-level `Makefile`** — acts as a *router*, not a build system. Targets `lint`, `test`, `build`, `bootstrap`, `clean` delegate into per-subtree Makefiles via a `run_if_exists` helper that no-ops with a friendly note when the subtree is absent. Keeps CI green during the multi-phase rollout.
- **`README.md`** — one-paragraph orientation, subtree table, pointers into `docs/`. Deliberately does not duplicate `vision.md`.
- **CI workflows** under `.github/workflows/` — `frontend.yml`, `backend.yml`, `daemon.yml`, `proto.yml`. Each is path-filtered and gates on a sentinel file (`frontend/package.json`, `backend/pyproject.toml`, `sandbox-daemon/go.mod`, `proto/codegen.sh`); skips cleanly if absent. Workflows "wake up" the moment their subtree lands.

### Decisions

- **Bare pnpm workspaces, not Turborepo.** `techstack.md` left this open. Turborepo's value (remote caching, task graphs) doesn't pay off until multiple TS packages do real work. Easy to layer on later.
- **CI is defensive (gate-and-skip), not deferred.** Rejected leaving `.github/workflows/` empty until each subtree exists — once subtrees start landing, "did I remember to add the workflow?" causes drift. Wiring path filters once now means the very first PR touching `frontend/` triggers the right job.
- **`Makefile` uses `run_if_exists` instead of hard-coded per-subtree targets.** Adding a subtree is a single `mkdir` + per-subtree Makefile away from being picked up by the root router; no edits to the root `Makefile` needed.
- **Generated proto clients are gitignored.** `make proto` regenerates them; the `proto.yml` workflow fails CI if regenerated output diverges from committed schemas. Avoids the classic "generated code committed for convenience, then drifts" footgun.

### Verification

```sh
make help        # prints targets
make bootstrap   # pnpm install (no workspaces yet → "Already up to date")
make lint        # all subtree gates skip cleanly
make build       # all subtree gates skip cleanly
```

CI workflows not yet triggered (no push), but gate logic was reviewed line-by-line against on-disk file-existence checks.

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §1: **`proto/`** — JSON Schema source-of-truth and codegen for TS/Go/Pydantic. Depends on settling the **session token contract** (cross-cutting section of the plan); confirm that decision before §2/§4 begin.
