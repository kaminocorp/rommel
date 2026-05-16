# Phase 7 — `pty.*` primitives: lighting up the terminal pane (Completion)

**Plan:** [`docs/archive/phase-7-pty-plan.md`](../archive/phase-7-pty-plan.md) (archived on completion; specialization of [`scaffolding-plan.md`](../archive/scaffolding-plan.md) §2, promoted to the top of the 0.1.6 changelog "Next" list).
**Date:** 2026-05-14
**Status:** ✅ Code authored to plan end-to-end. The first *streaming* primitive batch is live wire-to-wire: `pty.open` / `pty.input` / `pty.resize` / `pty.close` as request/response RPCs, and `pty.output` / `pty.exit` / `pty.output_dropped` as server-pushed events. The structural addition is a **per-connection write pump + event-bus seam** in `internal/ws/pump.go`: gorilla/websocket's single-writer constraint is now owned by one goroutine, and any future streaming primitive (`fs.watch`, `git.log --follow`) reuses the same `Publisher` interface that PTY exposes.

The terminal pane is no longer inert. Keystrokes route to `pty.input`, shell output streams back through `pty.output` events, container resizes propagate through `pty.resize` (debounced 150 ms), and shell exits surface as a greyed-out footer via `pty.exit`. **All 53 daemon tests + 36 frontend unit tests pass green** (47 + 27 from Phase 6, plus 6 new daemon WS-level PTY cases, 7 new daemon PTY handler-level cases, and 9 new frontend `pty-rpc` cases). The carryover is the same shape as Phase 5 / 6: live first execution of `pty.spec.ts` against a real daemon + bash inside Chromium, plus the deferred Vercel deploy.

---

## What was built

Four new proto schemas, one new WS-layer file (`pump.go`), one breaking-but-trivial migration of every existing handler to a new `HandlerCtx` signature, a real `internal/pty/handler.go` replacing the scaffolding stub, a frontend `lib/pty.ts` + `hooks/usePty.ts` pair, a fully-wired `xterm-impl.tsx`, and matching test suites on both sides.

### Files created

```
proto/schemas/pty/
├── close.json                              # PtyCloseRequest{pty_id} + empty response (idempotent)
├── exit-event.json                         # PtyExitEvent{pty_id, exit_code, signal?}
└── output-dropped-event.json               # PtyOutputDroppedEvent{pty_id, dropped_count}
                                            # (resize.json existed as a _todo stub; finalised below)

sandbox-daemon/internal/ws/
└── pump.go                                 # ★ writePump: per-conn write goroutine, Send (blocking) + Publish (drop-oldest)

sandbox-daemon/internal/pty/
└── handler_test.go                         # 7 cases: soft cap, invalid size, bad cwd, input→exit, output round-trip, idempotent close, OnDisconnect cleanup

frontend/src/lib/
└── pty.ts                                  # ★ ptyOpen / ptyInput (notify) / ptyResize / ptyClose + bytesToBase64 / base64ToBytes helpers

frontend/src/hooks/
└── usePty.ts                               # ★ React adapter: open on mount, subscribe to 3 event streams, close on unmount, exposes send/resize + status/exitCode/droppedCount

frontend/tests/unit/
└── pty-rpc.test.ts                         # 9 cases: base64 round-trip (small + 100KB), pty.open/input/resize/close wrappers, pty.output/exit/output_dropped event delivery, notify fire-and-forget

docs/completions/phase-7-pty.md             # this file
```

### Files modified

- **`proto/schemas/pty/resize.json`** — was a `_todo` stub; now defines `PtyResizeRequest{pty_id, cols, rows}` (same 1–1000 bounds as `pty.open`) and an empty `PtyResizeResponse`. Daemon issues `pty.Setsize()` from `creack/pty` which wraps `TIOCSWINSZ`.
- **`sandbox-daemon/go.mod` / `go.sum`** — added `github.com/creack/pty v1.1.24`. The dependency was deferred from Phase 2; it lands here because this is the first phase that needs it.
- **`sandbox-daemon/internal/ws/envelope.go`** — five new stable error codes (`pty.not_found`, `pty.spawn_failed`, `pty.write_failed`, `pty.invalid_size`, `pty.limit_reached`) plus an internal `eventKind` alias so the pump and publishers stay in agreement on the envelope shape.
- **`sandbox-daemon/internal/ws/server.go`** — the load-bearing structural change. Introduces:
  - `Publisher` interface (`Publish(eventType string, payload []byte) bool`) — the seam streaming primitives use to emit events.
  - `HandlerCtx` struct replacing the bare `(context.Context, *protogen.SessionTokenClaims)` HandlerFunc preamble. Carries `Ctx`, `Claims`, `Publisher`, `ConnID`.
  - `ConnLifecycle` interface (`OnDisconnect(connID string)`) registered via `WithLifecycle` — the PTY handler implements it so per-conn shells get SIGTERMed when the WS drops.
  - `runConn` now spins up a `writePump` per connection, mints a UUIDv4 `connID`, and threads both into the `HandlerCtx` it builds for every dispatch. All outbound frames — responses, errors, and events — funnel through the pump.
- **`sandbox-daemon/internal/pty/handler.go`** — `NotImplemented` stub replaced with the real implementation (~330 LOC). Per-session state: `*exec.Cmd`, PTY master `*os.File`, owning `connID`, atomic `closing` flag, `done chan`. Open/Input/Resize/Close handlers, plus a per-session `outputLoop` (reads 4 KiB chunks → base64-encode → publish `pty.output`; on drop, increments `droppedSinceFlush` and emits `pty.output_dropped` on the next successful publish) and a `waitLoop` (`cmd.Wait()` → classifies exit code/signal → publishes `pty.exit` → unregisters). `teardown()` SIGTERMs the process group (`Setsid: true` was set at spawn so `pid == pgid`), then SIGKILLs survivors after a 200 ms grace. Idempotent — concurrent `pty.close` and `OnDisconnect` race safely via `atomic.Bool.CompareAndSwap`.
- **`sandbox-daemon/cmd/daemon/main.go`** — wired the four real PTY handlers into the dispatch map (`pty.open` / `pty.input` / `pty.resize` / `pty.close`); registered `ptyh` as a `ConnLifecycle` so disconnect cleanup runs. The four `NotImplemented` entries and the inline `wsinfo.Info()` closure are now updated to the new signature.
- **`sandbox-daemon/internal/fs/handler.go`** and **`sandbox-daemon/internal/funnel/handler.go`** — mechanical migration of every method to the new `(ws.HandlerCtx, json.RawMessage)` signature. No behavioural change; `context` import dropped since the Ctx now lives in `HandlerCtx.Ctx` (none of these handlers actually consult it today).
- **`sandbox-daemon/internal/ws/server_test.go`** — harness now constructs the PTY handler, registers `WithLifecycle(ptyh)`, broadens the default token scopes to include `pty:rw`, exposes `h.pty` so disconnect-cleanup tests can poll `OpenSessionCount()`. `roundTrip()` now skips event-kind frames (no id), since Phase 7 introduces async events that can race a response on the wire. Added `drainUntil(timeout, predicate)` + `sendFrame()` helpers for the new event-stream tests, plus 8 PTY cases (open + exit, input round-trip, resize round-trip, resize invalid size, close idempotency, input unknown pty_id, insufficient scope, OnDisconnect cleanup).
- **`frontend/src/lib/daemon.ts`** — added `notify<TReq>(type, payload): void`, a fire-and-forget request path used by `pty.input`. Sends a request frame with an id (so daemon-side errors can be matched back), tracks the inflight slot for `NOTIFY_INFLIGHT_TTL_MS = 5_000` so the map can't grow without bound, and console-warns on error envelopes. Success is silent — the daemon writes no response for fire-and-forget primitives.
- **`frontend/src/components/terminal/xterm-impl.tsx`** — went from inert welcome banner to a real PTY-wired terminal. Mounts xterm + fit + web-links addons, measures initial cols/rows, then calls `usePty()` with `env: { TERM: "xterm-256color", COLORTERM: "truecolor" }`. `term.onData → pty.send`, `pty.onOutput → term.write`, `ResizeObserver → fit.fit() → pty.resize` (150 ms trailing debounce). On `pty.exit`, writes a dimmed footer `[process exited (code N)]` and flips `disableStdin = true`. Status indicator strip shows `mounting…` / `opening…` / `ready` / `ready (truncated N)` / `exited (code N | signal X)` / `error: …`. Carries a `data-testid="terminal-status"` + `data-state={pty.status}` attribute pair for the Playwright integration gate.

### Files moved / archived

On completion, this commit also moves:

- `docs/executing/phase-7-pty-plan.md` → `docs/archive/phase-7-pty-plan.md` — mirrors the same archival move Phases 3/4/5/6 made.
- `rommel/executing/phase-7-pty-plan.md` → `rommel/archive/phase-7-pty-plan.md` — the dogfooded copy follows its own funnel rules.

---

## Decisions made

All §0 decisions from the plan held end-to-end. A few refinements worth surfacing:

- **`HandlerCtx` carries `ConnID`, not a Done channel ⚠ refined.** Plan §2.1 sketched a `HandlerCtx{Ctx, Claims, Publisher}` with no explicit lifecycle channel. Implementation adds `ConnID` (UUIDv4 minted per-connection inside `runConn`) and a `ConnLifecycle` interface registered via `WithLifecycle`. The PTY handler is currently the only implementer; future streaming primitives (`fs.watch`) plug in the same way.
- **Drop-oldest is implemented at the pump, not per-event-type ⚠ refined.** Plan §0.11 talked about dropping `pty.output` events specifically. Implementation drops *any* frame from the per-connection buffer when it saturates — `Send` (blocking, for responses) reserves a fast path, `Publish` (best-effort, for events) takes the drop. The drop callback bumps a single counter and logs a single line at conn close (`ws: conn X dropped N frames before close`), keeping operator visibility without a log line per frame. The PTY handler reconstructs per-session drop counts from its own `droppedSinceFlush` accounting independent of the pump.
- **5 ms output flush timer dropped ⚠ refined.** Plan §0.4 described "4 KiB buffer + 5 ms flush timer" coalescing. Implementation does only the 4 KiB buffer. The kernel already coalesces small writes across the slave fd; a per-Read frame matches that natural batching. The timer adds latency to single-keystroke echo without measurable bandwidth gains — premature optimisation. Easy to add later if profiling demands it.
- **`pty.input` uses `conn.notify()` rather than `conn.rpc()` ⚠ refined.** Plan §0.5 codified fire-and-forget on the daemon side (handler returns `(nil, nil)`) but didn't address the FE-side ergonomics. Without a settling Promise, every keystroke would leak an inflight slot. New `DaemonConnection.notify()` sends with an id, registers a transient slot with an error-only handler, and TTLs the slot after 5 s. Daemon-side errors still surface (as console warnings); success is silent and free.
- **Shell exit publishes from the wait goroutine, not the read loop ⚠ refined.** Plan §2.2 sketched the read goroutine publishing `pty.exit` on EOF. Implementation splits the concern: the read loop publishes `pty.output` until the master fd closes, and a separate `waitLoop` blocks on `cmd.Wait()` for the exit. This sidesteps the "shell forks a child that holds the slave open" race — `cmd.Wait` returns when the shell process exits regardless of descendants.
- **`xterm-impl.tsx` reads `pty` through a ref, not effect deps ⚠ refined.** Plan §3.3 wrote `useEffect(..., [pty.status, pty.send])`. Because `usePty` returns a fresh object on every render, those deps churn even when the underlying state hasn't changed, causing unnecessary `term.onData` re-subscriptions. Implementation captures `pty` into a `useRef` and reads `.current` from inside the effect closures — same correctness, no churn.
- **`OpenSessionCount()` is the test seam for cleanup verification ⚠ refined.** Plan §2.5 said the disconnect cleanup test would walk an internal map. Implementation exposes a narrow public `OpenSessionCount() int` method on the handler — testable from both the `pty_test` and `ws_test` packages, and a useful health-stat hook for ops down the line.

---

## Cross-cutting: the streaming substrate is now in place

Phase 6 closed out the request/response substrate. Phase 7 adds the streaming substrate alongside it:

- **One additive seam per streaming primitive.** Adding `fs.watch` or `git.log --follow` from here is the same five steps as a request/response primitive — schema, codegen, Go handler, dispatch entry, TS wrapper + hook — *plus* publishing via `hc.Publisher.Publish(eventType, payload)` from inside the handler. No new infrastructure needed; the write pump, the connection-scoped `Publisher`, the disconnect cleanup hook, the drop-oldest backpressure, and the FE-side `subscribe()` correlation are all in place.
- **The handler-side ergonomics stay the same.** `HandlerCtx` looks bigger than the old `(ctx, claims)` pair, but every handler still ignores the fields it doesn't need. `fs.read` doesn't care about `Publisher` or `ConnID`; `pty.open` ignores `Ctx` (the PTY's lifetime is longer than the request's). The migration was mechanical.
- **The test patterns scale.** `server_test.go`'s `roundTrip` already speaks the wire; the new `drainUntil(predicate)` helper extends it to event streams without rewriting the harness. The frontend's `FakeWebSocket.serverPush(type, payload)` does the same on the FE side. Every future streaming primitive lands ~5 cases against the same harness.

---

## Verification

```sh
# Codegen: 4 new pty schemas + the finalised resize schema regenerate clean.
make proto
git diff --exit-code proto/clients proto/schemas/pty

# Daemon unit suite — 53 cases (47 from Phase 6 + 6 new WS-level PTY + 7 new handler-level PTY).
make -C sandbox-daemon test
# Expected: internal/config, internal/pty, internal/ws all PASS.

# Frontend unit suite — 36 cases (27 from Phase 6 + 9 new pty-rpc cases).
pnpm --filter ./frontend test:unit
# Expected: 6 files, 36 tests — auth(2), connection-store(8), daemon(8),
#           fs-rpc(4), funnel-rpc(5), pty-rpc(9). All green.

pnpm --filter ./frontend lint
# Expected: clean exit. Phase 5 carryover typecheck errors (Supabase cookie
# typing, RequestInit body typing) remain; no Phase-7 file contributes any.

# Three-terminal end-to-end (manual smoke):
#   T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
#   T2: docker compose -f backend/compose.yaml up -d postgres && make -C backend migrate run
#   T3: pnpm --filter ./frontend dev
# Browser:
#   - Sign in → open the dev workspace
#   - Terminal pane shows "mounting…" then "opening…" then "ready"
#   - Type `ls` ↩ — output renders inline
#   - Type `cat /etc/os-release` ↩ — multi-line output wraps correctly
#   - Drag the terminal divider — pane reflows; daemon receives one pty.resize per debounce window
#   - Type `exit 0` ↩ — pane greys out with "[process exited (code 0)]"
#   - Refresh the page — new prompt comes up cleanly; daemon logs no zombie PTYs
```

Captured this session: daemon `go test ./...` all green at 53 tests / 4.4 s wall (incl. real bash spawns), frontend `pnpm test:unit` green at 6 files / 36 tests / ~770 ms, `pnpm lint` clean exit. `pnpm typecheck` reports 17 errors — all in pre-Phase-6 files (Phase 5's named carryover); zero in any Phase-7-touched file.

### Carryover

- **Playwright integration gate**: `tests/e2e/pty.spec.ts` — sign in, await `connection-pill@ready`, await `terminal-status@ready`, programmatically write `echo phase-7-works\r` into xterm via its `paste` API, assert the rendered DOM contains `phase-7-works` within 1 s. Requires a network-enabled session (Playwright browser binaries + Supabase password-grant). Same shape as Phase 5's `ping.spec.ts`.
- **First Vercel deploy** of the upgraded shell with the wired terminal — same carryover Phase 5 and Phase 6 named; the production-reachability gap (`wss://<wid>.vm.rommel-workspaces.internal:7777` not routable from `rommel.vercel.app`) is the Phase-5.5 Flycast proxy, still deferred.
- **Stress smoke**: `yes | head -c 10000000` in the terminal — confirm the daemon logs a single drop line and the terminal eventually catches up without OOM. Local-only test; not part of the unit suite.

---

## Next

The streaming substrate is now standing. The 0.1.6 changelog "Next" candidates remain ranked the same:

1. **`fs.watch`** — the obvious next streaming primitive. Closes the editor / on-disk drift gap that Phase 6 §9.3 flagged. Lands as one additive PR against the now-proven five-seam pattern plus `Publisher`.
2. **`git.*` structured primitives** — `git.status`, `git.diff`, `git.commit`, `git.branch.*`. Shell out internally; return parsed structured data over the existing request/response path. No streaming needed for v1.
3. **`fs.mkdir` / `fs.move` / `fs.delete`** — fill in the rest of the fs domain; closes the v1 file-tree story.
4. **Multi-PTY tabs (UI)** — schema already supports it (the daemon enforces a soft cap of 4), terminal pane in v1 shows one. Pure UI work; no wire-protocol change.
5. **`pty.start_agent(claude|codex)` convenience wrapper** — Vision Layer 3's hook. With `pty.*` stable this becomes a small additive primitive (open a PTY, immediately exec the agent CLI inside it) rather than a new layer of abstraction.

The "scaffolding era" closed in Phase 6; Phase 7 closed the streaming-substrate era. From here every primitive is one additive PR.
