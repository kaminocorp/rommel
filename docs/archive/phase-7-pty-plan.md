# Phase 7 Plan — `pty.*` primitives: lighting up the terminal pane

Specialization of [`scaffolding-plan.md`](./scaffolding-plan.md) §2 (the `creack/pty` line item deferred from Phase 2), promoted to the top of the §0.1.6 changelog "Next" list. First phase to use the closed-out substrate rather than build it.

**Phase 7 is the first *streaming* primitive batch.** Phases 1–6 shipped request/response RPCs only. PTY needs server-pushed `pty.output` events flowing without a matching request `id`. The envelope already supports this (`kind: "event"` in `proto/schemas/envelope.json` + `DaemonConnection.subscribe()` from Phase 5), but the daemon's WS server (`internal/ws/server.go::runConn`) is a strict read→dispatch→write loop with no server-push pathway. **The structural addition Phase 7 makes is a per-connection write pump + an event-bus seam** that any future streaming primitive (`fs.watch`, `git.log --follow`, etc.) reuses.

Pattern after this phase: streaming primitives are additive in the same five-seam way request/response ones are, plus they publish via `conn.Publish(eventType, payload)` on the event bus.

---

## 0 — Decisions to lock in before authoring

### 0.1 — Shell choice: `/bin/bash` with fallback to `/bin/sh` ✅

`workspace-image/`'s Debian base ships both. Spawn `bash` if `$SHELL` is unset (which it usually is in a non-login VM init); honour `$SHELL` if the workspace's baked env sets it. **No `--login`**: we don't want `/etc/profile` slowing first-keystroke latency. `bashrc` runs if it exists (interactive shell), which is what the user expects.

### 0.2 — PTY identity: opaque UUIDv4 strings ✅

`PtyOpenResponse.pty_id` is already typed as `string` in `proto/schemas/pty/open.json`; lock in UUIDv4 so debug logs and event-bus topic names are unambiguous. Daemon mints it; client never invents one.

### 0.3 — One PTY per WebSocket connection in v1 ⚠ refined

The schemas support N PTYs per connection (every payload carries `pty_id`). The frontend's `TerminalPane.tsx` only shows one terminal. v1: enforce a soft cap of 4 PTYs per connection on the daemon (defence against runaway-agent bugs), but no UI for >1. Multi-PTY tabs is a follow-up; the wire contract doesn't need to change to enable it.

### 0.4 — `pty.output` framing: base64-encoded bytes, daemon-controlled coalescing ✅

`output-event.json` already specifies base64 (correct — PTY output is arbitrary bytes, including raw escape sequences and partial UTF-8 sequences mid-stream; JSON-safe encoding is non-negotiable). Daemon batches reads with a 4 KiB buffer and a 5 ms flush timer — keeps per-frame overhead reasonable for chatty output (`yes`, `find /`) without adding human-perceptible latency for keystroke echo.

### 0.5 — `pty.input` is fire-and-forget ✅

`input.json` already documents this. Handler returns `(nil, nil)` per the `HandlerFunc` contract reserved for this case in `internal/ws/server.go:22-23`. Errors (closed PTY, decode failure) come back via the envelope's `error` kind correlated by request `id` — already supported.

### 0.6 — `pty.resize` schema: needs finalising ⚠ blocker

`proto/schemas/pty/resize.json` is currently a `_todo` stub. Phase 7 finalises it:

```json
{ "pty_id": "<uuid>", "cols": 1..1000, "rows": 1..1000 }
```

Same `cols`/`rows` bounds as `pty.open`. Response is empty `{}` (acks the resize; no payload needed). Daemon calls `pty.Setsize()` from `creack/pty` which wraps `TIOCSWINSZ`.

### 0.7 — `pty.close` schema: doesn't exist yet ⚠ blocker

No `proto/schemas/pty/close.json` file. Phase 7 creates it:

```json
{ "pty_id": "<uuid>" }    // request
{}                         // response
```

Also: define the **`pty.exit` server-push event** — daemon emits it when the shell process exits (e.g. user types `exit`), payload `{pty_id, exit_code}`. Lets the frontend grey out the terminal and offer a "restart" button instead of silently hanging.

### 0.8 — Connection close = best-effort kill of all PTYs ✅

When the WS connection drops (refresh, navigate away, network blip), daemon SIGTERMs every PTY owned by that connection, waits 200 ms, then SIGKILLs survivors. No "reconnect to existing PTY" in v1 — refresh = fresh shell. Persistent shells (tmux, screen) work *inside* the PTY for users who want them; daemon doesn't try to be tmux.

### 0.9 — Working directory: defaults to workspace root ✅

`PtyOpenRequest.cwd` is already optional in the schema. When unset, daemon uses `cfg.WorkspaceRoot`. When set, path-sandbox using the same `resolve()` helper `internal/fs/handler.go` uses — no escapes outside the workspace root.

### 0.10 — Environment: workspace baseline + per-PTY merge ✅

Daemon process inherits its env from the VM (PATH, HOME, USER, plus whatever the workspace image bakes). `PtyOpenRequest.env` merges on top — useful for setting `TERM=xterm-256color`, `LANG=C.UTF-8`, or per-session `OPENAI_API_KEY` if an agent needs it. **The frontend always sets `TERM=xterm-256color` and `COLORTERM=truecolor`** because xterm.js renders them; daemon doesn't default it (let the client decide).

### 0.11 — Backpressure: bounded channel + drop policy with a count ⚠ refined

A `cat /dev/urandom | base64` from inside the PTY will firehose `pty.output` events faster than the browser can render. Two options:

- A) Unbounded channel, hope for the best.
- **B) Bounded channel (256 buffered events), drop oldest on overflow, emit a `pty.output_dropped` event with `{pty_id, dropped_count}` so the UI can flash "output truncated"**.

Picking **B**. The 256-event buffer at 4 KiB/event gives ~1 MiB of in-flight tolerance — enough to ride out frontend GC pauses, not enough to OOM the daemon on a runaway process. Drop policy is "oldest first" because newer output is more relevant to the user.

### 0.12 — Frontend: xterm.js write/read wiring, one PTY per `TerminalPane` ✅

Phase 5 mounted xterm.js inside `terminal/xterm-impl.tsx` but left it inert (welcome banner only). Phase 7 wires:
- `xterm.onData(d => ptyInput(conn, ptyId, b64(d)))` — every keystroke.
- `useSubscription("pty.output", e => if (e.pty_id === ptyId) xterm.write(b64decode(e.data)))` — fan-in.
- `fit.onResize(({cols, rows}) => debounce(150ms, () => ptyResize(conn, ptyId, cols, rows)))` — terminal-grid → kernel.
- On `pty.exit` event: write `\r\n[process exited (code N)]\r\n` into xterm and disable input.

### 0.13 — Tests at the wire layer for the new event-stream machinery ⚠ load-bearing

Phase 6's `tests/unit/funnel-rpc.test.ts` pattern works fine for request/response. The new event-stream path needs a fresh test seam:
- **Daemon side**: extend the existing `server_test.go` harness so the test client can register an event handler before sending a request, then assert events arrive *interleaved* with the response. The harness already mints tokens and dials WS; this adds a `recvEvents(predicate)` helper.
- **Frontend side**: extend `FakeWebSocket` so the test can `serverPush(eventFrame)` mid-test, and add `tests/unit/pty-rpc.test.ts` covering: open → input round-trips → output event delivered → resize → close → exit event.

---

## 1 — `proto/` (Seam 1: schemas)

### 1.1 — Finalise `proto/schemas/pty/resize.json`

Replace the `_todo` stub with `PtyResizeRequest{pty_id, cols, rows}` + empty `PtyResizeResponse`. Same `cols`/`rows` bounds as `open.json` (1–1000). Same `oneOf` Request/Response wrapper pattern as `funnel/*.json`.

### 1.2 — Create `proto/schemas/pty/close.json`

`PtyCloseRequest{pty_id}` + empty `PtyCloseResponse`. Idempotent on the daemon (closing an already-closed PTY returns success, not an error — keeps unmount cleanup simple).

### 1.3 — Create `proto/schemas/pty/exit-event.json`

`{pty_id, exit_code: int, signal?: string}`. `signal` populated when the shell died from a signal rather than `exit(N)` (e.g. SIGKILL'd by the connection-close path). Frontend uses this to choose the message.

### 1.4 — Create `proto/schemas/pty/output-dropped-event.json`

`{pty_id, dropped_count: int}`. Per §0.11.

### 1.5 — Regenerate clients

`make proto` produces fresh `clients/ts`, `clients/go`, `clients/python`. CI's `proto.yml` workflow fails if codegen drifts — no manual editing of generated files.

---

## 2 — `sandbox-daemon/` (Seams 2 + 3: dispatch + handler + WS infrastructure)

### 2.1 — New WS infrastructure: write pump + event bus

The load-bearing structural change. New file `internal/ws/pump.go`:

- **Per-connection `writeCh chan *Frame`** owned by the `runConn` lifecycle. A goroutine drains it onto the socket; the read loop becomes the only thing on the goroutine that owns reads. Solves gorilla/websocket's "concurrent writes panic" without sprinkling mutexes everywhere.
- **`type Publisher interface { Publish(eventType string, payload any) }`** — passed into handlers via a new `HandlerCtx` so the PTY handler can emit `pty.output` events without holding a reference to the socket.
- **Backpressure**: `writeCh` is buffered 256 events deep; if full, drop oldest event (not request response — those are correctness-critical and use a separate `respCh` with no drop policy).

Update `HandlerFunc` signature to take `HandlerCtx` instead of bare `context.Context`:

```go
type HandlerCtx struct {
    Ctx       context.Context
    Claims    *protogen.SessionTokenClaims
    Publisher Publisher  // nil for non-streaming primitives
}
type HandlerFunc func(hc HandlerCtx, payload json.RawMessage) (json.RawMessage, *protogen.EnvelopeError)
```

This is the one breaking change to `internal/ws/`. All Phase 6 handlers get a trivial diff (one new param, unused).

### 2.2 — `internal/pty/handler.go`: real implementation

Replace the `NotImplemented` stub. Structure:

```go
type Handler struct {
    cfg     *config.Config
    mu      sync.Mutex
    ptys    map[string]*ptySession      // keyed by pty_id
}

type ptySession struct {
    id       string
    cmd      *exec.Cmd
    file     *os.File       // PTY master from creack/pty
    publish  Publisher      // captured at open-time
    done     chan struct{}
}
```

Methods:
- `Open(hc, req) -> resp` — `pty.Start(exec.Command(shell))`, set initial size, mint UUID, register, spawn the output goroutine which loops `file.Read` → base64 → `hc.Publisher.Publish("pty.output", {pty_id, data})` until EOF, then publishes `pty.exit`.
- `Input(hc, req) -> (nil, nil)` — base64 decode, `file.Write`. Fire-and-forget.
- `Resize(hc, req) -> resp` — `pty.Setsize(file, &pty.Winsize{Rows, Cols})`.
- `Close(hc, req) -> resp` — signal `done`, `file.Close()`, `cmd.Process.Kill()` after 200 ms grace. Idempotent.

**Per-connection PTY ownership**: the `Handler` is daemon-global, but each PTY is tagged with its owning connection's identity (a per-conn UUID passed via `HandlerCtx`). `runConn`'s deferred cleanup walks the map and closes any PTY belonging to the disconnecting conn.

### 2.3 — Dispatch wiring in `cmd/daemon/main.go`

Replace the four `NotImplemented` entries:

```go
"pty.open":   {RequiredScope: ptyRw, Fn: ptyh.Open},
"pty.input":  {RequiredScope: ptyRw, Fn: ptyh.Input},
"pty.resize": {RequiredScope: ptyRw, Fn: ptyh.Resize},
"pty.close":  {RequiredScope: ptyRw, Fn: ptyh.Close},   // NEW route
```

`pty:rw` scope already exists in the session-token enum since Phase 1. No backend change.

### 2.4 — Error codes in `internal/ws/envelope.go`

Add stable codes used by the new handlers:
- `pty.not_found` — `pty_id` doesn't exist (or already closed).
- `pty.spawn_failed` — shell process couldn't start (e.g. workspace missing `/bin/bash`).
- `pty.write_failed` — write to the PTY master failed (typically because the shell exited mid-input).
- `pty.invalid_size` — cols/rows out of range. (JSON-schema validation would catch most, but defence in depth.)

### 2.5 — Tests: `internal/ws/server_test.go` + new `internal/pty/handler_test.go`

- New harness helper `recvEvents(t, ws, predicate, timeout)` that drains incoming frames until one matches `predicate` (used to assert `pty.output` arrival).
- `server_test.go`: 4–6 cases covering the write-pump correctness — concurrent publish + response don't interleave bytes, drop policy fires when channel is full, disconnection cleans up sessions.
- `handler_test.go`: spawn a shell, write `echo hello\n`, assert `pty.output` arrives with `hello\r\n` in the decoded stream. Use `bash -c 'cat'` for a deterministic input→output test that avoids prompt/escape-sequence noise.
- Estimated 12–15 new tests. Target: green hermetic suite, no network, no shared global state.

---

## 3 — `frontend/` (Seams 4 + 5: TS wrapper + hook + component)

### 3.1 — `frontend/src/lib/pty.ts`

Typed wrappers, ~50 LOC:

```ts
export function ptyOpen(conn, req: PtyOpenRequest): Promise<PtyOpenResponse>
export function ptyInput(conn, ptyId: string, dataBytes: Uint8Array): void  // base64-encodes internally
export function ptyResize(conn, ptyId: string, cols: number, rows: number): Promise<void>
export function ptyClose(conn, ptyId: string): Promise<void>
```

Plus base64 helpers (`bytesToB64`, `b64ToBytes`) — small, dependency-free, browser-native via `btoa`/`atob` after converting to/from Latin-1. **No `Buffer` shim**: keep the bundle clean.

### 3.2 — `frontend/src/hooks/usePty.ts`

```ts
export function usePty(opts: { cols, rows, cwd?, env? }): {
  ptyId: string | null
  status: "opening" | "ready" | "exited" | "error"
  exitCode: number | null
  output: Subscription<Uint8Array>     // event source the component drains into xterm
  send: (data: Uint8Array) => void
  resize: (cols, rows) => void
}
```

Internally:
- On mount: `ptyOpen` → captures `ptyId`.
- Subscribes to `pty.output` / `pty.exit` / `pty.output_dropped` via `DaemonConnection.subscribe`, filtered by `ptyId`.
- On unmount: `ptyClose` (best-effort; daemon also cleans up on disconnect).
- Doesn't try to be xterm — exposes a typed event stream and lets the component own xterm's lifecycle.

### 3.3 — `frontend/src/components/terminal/xterm-impl.tsx`: real wiring

Replaces the inert welcome banner. Mounts xterm + fit + web-links addons (already done in Phase 5), then:

- On daemon connection ready: `usePty({ cols: term.cols, rows: term.rows })`.
- `term.onData(d => pty.send(new TextEncoder().encode(d)))`.
- `pty.output.subscribe(bytes => term.write(bytes))` — xterm.js accepts `Uint8Array` directly, no decoding needed (PTY bytes pass through verbatim, escape sequences and all).
- `ResizeObserver` → `fit.fit()` → `pty.resize(term.cols, term.rows)`, debounced 150 ms.
- On `pty.status === "exited"`: write a grey footer, disable input cursor.
- Status indicator: small text in the terminal pane's title strip — "ready" / "opening" / "exited (code 0)".

### 3.4 — Tests: `frontend/tests/unit/pty-rpc.test.ts`

5–7 cases against an upgraded `FakeWebSocket` (new `serverPush` method):
- `ptyOpen` round-trips and parses `pty_id`.
- `ptyInput` produces a fire-and-forget frame (no response expected, no promise resolves/rejects).
- Subscriber receives `pty.output` events filtered by `pty_id`.
- `ptyResize` round-trip.
- `ptyClose` round-trip; subsequent calls are idempotent.
- `pty.exit` event flips the hook's `status` to `"exited"` and captures `exit_code`.
- `pty.output_dropped` event surfaces as a `droppedCount` field on the hook.

Plus: extend `tests/unit/daemon.test.ts` with one case covering the new `subscribe()` event-correlation path (which was authored in Phase 5 but never exercised by a real consumer).

---

## 4 — Risks

### 4.1 — Concurrent writes to the gorilla/websocket connection ⚠ high

`gorilla/websocket` panics on concurrent writes. Today the daemon's read loop is the only writer (`s.writeFrame` from `runConn`'s goroutine). Adding PTY-output goroutines that need to write events introduces concurrency. **Mitigation**: §2.1's write pump — a single goroutine owns the socket's write side, all publishers funnel through `writeCh`.

### 4.2 — PTY output races shell exit ⚠ medium

If the shell prints "hello\n" then immediately exits, the daemon's read goroutine might emit `pty.exit` before the final `pty.output` has been flushed. **Mitigation**: flush pending output buffer synchronously *before* publishing `pty.exit`, and order writes through the same `writeCh` so they can't reorder on the wire.

### 4.3 — xterm resize storms ⚠ medium

Dragging a pane divider fires `ResizeObserver` at frame rate (60 Hz). Each event currently maps to one `pty.resize` RPC. **Mitigation**: 150 ms trailing debounce on the frontend, and `pty.Setsize` is cheap (one ioctl) on the daemon side. Don't over-engineer.

### 4.4 — Cold-start latency from spawning bash ⚠ low

First `pty.open` includes the fork+exec of `bash`. On a Fly Machine that just woke up, this stacks on top of the ~250 ms machine cold-start. User-perceptible latency target: <500 ms from "click workspace" to "prompt visible." **Mitigation**: pre-warm the shell on workspace mount (open PTY eagerly even if the terminal pane isn't focused). v1 lazy is fine; pre-warm is a follow-up if measured slow.

### 4.5 — UTF-8 mid-sequence chunking ⚠ low

PTY output is bytes, not characters; a 4 KiB chunk can split a multi-byte UTF-8 sequence. xterm.js handles this correctly (it has its own decoder that buffers partial sequences). Base64 transport is byte-clean. **Mitigation**: none needed; flagging because it's the kind of thing that looks like a bug if you don't know to look for it.

### 4.6 — `creack/pty` on macOS dev vs Linux prod ⚠ low

`creack/pty` supports both, but PTY allocation paths differ (`/dev/ptmx` vs `openpty`). Dev on macOS, prod on Debian. **Mitigation**: integration test runs in CI's Linux container; macOS dev is best-effort.

### 4.7 — Backpressure threshold tuning ⚠ medium

256-event buffer is a guess. Too small: legitimate verbose output (a `make` log) gets dropped. Too large: a malicious or buggy process can pin daemon memory. **Mitigation**: ship 256, add a daemon log line on every drop with `pty_id` + `dropped_count` so we can tune from real workloads. Don't make it configurable in v1.

### 4.8 — Connection-close PTY cleanup is best-effort ⚠ low

If the daemon process crashes, child shells become orphans (reparented to init). On Fly, the machine restart cycles the whole filesystem so orphans don't accumulate persistently. **Mitigation**: none for v1. Note for the eventual "persistent workspaces" feature.

---

## 5 — Verification

```sh
# Codegen: all four pty schemas regenerate clean, no drift
make proto
git diff --exit-code proto/clients

# Daemon unit suite — expect ~60 cases after Phase 7 (47 from Phase 6 + ~13 new):
make -C sandbox-daemon test

# Frontend unit suite — expect ~35 cases (27 from Phase 6 + ~8 new):
pnpm --filter ./frontend test:unit
pnpm --filter ./frontend lint

# Three-terminal end-to-end:
#   T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
#   T2: docker compose -f backend/compose.yaml up -d postgres && make -C backend migrate run
#   T3: pnpm --filter ./frontend dev
# Browser:
#   - Sign in → open the dev workspace
#   - Terminal pane shows "opening..." then a bash prompt
#   - Type `ls`; output renders
#   - Type `cat /etc/os-release`; multi-line output renders with correct line wrapping
#   - Drag the terminal pane edge → contents reflow correctly
#   - Type `exit` → pane greys out with "[process exited (code 0)]"
#   - Refresh page → new prompt comes up cleanly (no zombie PTYs in daemon logs)
#
# Stress smoke:
#   - Open the terminal, run `yes | head -c 10000000` (~10 MiB of "y\n")
#   - Confirm the daemon logs "pty.output dropped N events" once (channel saturation)
#   - Confirm the terminal eventually catches up; no daemon OOM
```

### Playwright integration gate (extension of Phase 5's `ping.spec.ts`)

New `tests/e2e/pty.spec.ts`:
- Sign in, navigate to `/workspaces/dev`.
- Wait for `[data-testid=connection-pill][data-status=ready]`.
- Wait for `[data-testid=terminal-status][data-state=ready]` (new attribute on the pane).
- Programmatically write `echo phase-7-works\r` to the xterm via its `paste` API.
- Wait for `phase-7-works` to appear in the rendered terminal DOM (xterm exposes the buffer as text).
- Assert latency: `echo` round-trip completes inside 1 s on the CI runner.

---

## 6 — Out of scope (deliberately deferred)

- **Multi-PTY tabs.** The schema allows it, the daemon enforces a soft cap of 4, the frontend shows 1. Tabs are a UI concern, not a wire-protocol one.
- **Reconnect-to-existing-PTY.** Refresh = fresh shell. tmux/screen inside the PTY handle persistent sessions for users who want them.
- **Per-process I/O isolation.** A PTY is one process group. Splitting stdout/stderr or hooking specific child processes is out of scope — that's a structured-shell concern, not a terminal one.
- **`pty.start_agent(claude|codex)`.** Convenience wrapper for "open a PTY and immediately exec a coding-agent CLI." Belongs to Vision Layer 3 once `pty.*` is stable; not Phase 7.
- **Recording / scrollback persistence.** xterm.js owns scrollback in browser memory; refresh clears it. Persistent scrollback is a database concern, not a daemon one.
- **ConPTY (Windows).** Daemon runs on Linux only (Fly Machines). Not even a question.
- **`fs.watch`, `git.*`, `fs.mkdir/move/delete`.** Next phases. Each is one additive PR against the now-proven five-seam pattern + (for `fs.watch`) the new event-bus seam Phase 7 introduces.

---

## 7 — Suggested commit shape

One PR, mirrored from Phase 6's structure:

1. **Schemas** — `proto/schemas/pty/{resize,close,exit-event,output-dropped-event}.json`. Run `make proto`.
2. **WS infrastructure** — `internal/ws/pump.go` + `HandlerCtx` migration across existing handlers (trivial diff). Tests for the pump in `server_test.go`.
3. **PTY handler** — `internal/pty/handler.go` real impl + `handler_test.go`. Dispatch wiring in `cmd/daemon/main.go`. New error codes in `envelope.go`.
4. **Frontend wrappers** — `lib/pty.ts`, `hooks/usePty.ts`, `pty-rpc.test.ts`. `FakeWebSocket` upgrade for `serverPush`.
5. **Terminal pane wiring** — `components/terminal/xterm-impl.tsx` real impl. Manual + Playwright verification.
6. **Completion doc** — `docs/completions/phase-7-pty.md`. Archive this plan: `docs/executing/phase-7-pty-plan.md` → `docs/archive/phase-7-pty-plan.md`. Same move for the dogfooded copy under `rommel/executing/` → `rommel/archive/` (using `funnel.promote` itself — meta-dogfooding).

Estimated LOC: ~600 daemon (handler + pump + tests), ~400 frontend (wrappers + hook + xterm wiring + tests), ~120 schema/codegen drift. Comparable to Phase 6.
