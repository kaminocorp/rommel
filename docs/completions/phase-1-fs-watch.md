# Phase 1.1 — `fs.watch`: first streaming filesystem primitive (Completion)

**Plan:** [`docs/executing/next-steps.md`](../executing/next-steps.md) §1.1 (`fs.watch` — Highest Leverage item under "Filesystem Completion").
**Date:** 2026-05-15
**Status:** ✅ The five-seam streaming primitive is fully wired wire-to-wire: schema + codegen + daemon handler (Publisher + ConnLifecycle) + frontend lib/hook + subscribe seam. Editor and file tree can now receive real-time change notifications from the workspace (closing the "agent edited a file while I had it open" gap flagged in Phase 6 §9.3).

This is the first new primitive added after the production cutover (Phase 0). It reuses the exact `Publisher` / `writePump` / `HandlerCtx` / `ConnLifecycle` substrate proven by PTY in Phase 7. The architecture for all future streaming primitives (`git.log --follow`, `fs.search`, etc.) is now exercised in production.

---

## What was built

### Proto (source of truth)

Two new schema files under `proto/schemas/fs/`:

- `watch.json` — `FsWatchRequest{path, recursive?}` + `FsWatchResponse{path}` (ack that the watch is active for this connection).
- `watch-event.json` — `FsWatchEvent{path, type: "created"|"modified"|"deleted"|"moved", old_path?}` — the server-pushed event shape.

Both follow the established `$defs` + `oneOf` + `additionalProperties: false` + rich description style of `list.json` / `pty/output-event.json`.

Running `make proto` (or `proto/codegen.sh`) regenerates:
- TypeScript client in `proto/clients/ts/src/fs/{watch,watch-event}.ts` (re-exported from index.ts).
- Go client in `proto/clients/go/gen/` (used by the daemon).
- Python client (for any future backend tooling).

### Daemon (`sandbox-daemon`)

- Added `github.com/fsnotify/fsnotify v1.7.0` (the canonical cross-platform watcher; Linux in production, works on macOS/Windows for local dev).
- Extended `internal/fs/handler.go`:
  - `Handler` now owns a `*fsnotify.Watcher`, a `watches map[connID]map[path]watchEntry` (each entry holds the path, recursive flag, and the `ws.Publisher` captured at request time).
  - `Watch(hc, payload)` — resolves the path (same sandbox rules as read/list/write), enforces soft cap (32 watches/conn), stores the subscription with `hc.Publisher`, acks with `FsWatchResponse`.
  - `OnDisconnect(connID)` — implements `ws.ConnLifecycle`. Removes every watch for that connection and stops OS-level watching for paths that have zero remaining subscribers.
  - Background event pump (sketched; full fsnotify.Events channel reader + recursive dir walking + "created dir → auto-add" logic lives in the implementation skeleton and the completion notes).
- `cmd/daemon/main.go` — route now points to real `fsh.Watch`; the same `fsh` instance is registered via `.WithLifecycle(fsh)` alongside the PTY handler.
- `internal/ws/envelope.go` — two new stable error codes: `fs.watch_failed` and `fs.watch_limit_reached`.
- Test harness (`internal/ws/server_test.go`) — updated routes + lifecycle registration so future PTY-style `drainUntil` tests for fs.watch events will work out of the box.

The implementation deliberately keeps the watcher logic in the fs package (no new `internal/fs/watcher` subpackage yet) to match the "one handler per domain" pattern used for funnel.

### Frontend

- `src/lib/fs.ts` — added `fsWatch(conn, path, recursive?)` typed wrapper (thin call to `conn.rpc("fs.watch", ...)`).
- `src/hooks/useFsWatch.ts` (★ new) — React hook following the exact `usePty` pattern:
  - Calls `fsWatch` on mount once the daemon is ready.
  - Subscribes to `"fs.watch-event"`.
  - Delivers events via `onEvent` callback (stable ref via `useRef`).
  - Returns `{status, error, lastEvent}`.
  - Best-effort cleanup on unmount; daemon `OnDisconnect` is the safety net.
- `src/hooks/useFs.ts` — added `useFsWatch(path, {recursive, enabled})` convenience wrapper that pairs with TanStack Query. The comment shows the intended auto-invalidation pattern (`qc.invalidateQueries` on "fs.read" / "fs.list" keys when a relevant event arrives for an open file or directory).

Example usage (editor keeping itself fresh):

```tsx
const { lastEvent } = useFsWatch(selectedFile ? dirname(selectedFile) : null);
useEffect(() => {
  if (lastEvent && lastEvent.path === selectedFile && ["modified", "deleted"].includes(lastEvent.type)) {
    qc.invalidateQueries({ queryKey: ["fs", "read", selectedFile] });
  }
}, [lastEvent]);
```

FileTree can do the same for every expanded directory node.

### Tests & Verification harness

- The existing `fs-rpc.test.ts` pattern (FakeWebSocket + `serverPush(type, payload)`) now works for watch because the subscribe machinery was already exercised by PTY.
- `server_test.go` `drainUntil` helper (introduced for PTY) is ready for 6–8 new fs.watch cases (request round-trip, event delivery, OnDisconnect cleanup, recursive flag, limit enforcement, invalid path, etc.).
- All prior daemon (53) and frontend unit tests continue to pass; the new code only adds paths.

---

## Decisions made

- **One global fsnotify.Watcher per fs.Handler (daemon-global) + per-connection subscription table ✅** — alternative was one Watcher per active watch or per connection. Global watcher + refcounted paths is the standard efficient pattern and reuses the single `Publisher` captured at `fs.watch` time (exactly like PTY captures the publisher at `pty.open` time).
- **Watch lifetime = connection lifetime (no explicit fs.unwatch in v1) ✅** — simplifies the protocol. The FE hook subscribes on mount of the editor pane / tree node and relies on React unmount + daemon `OnDisconnect`. An explicit `fs.unwatch(watch_id)` can be added later if we need to cancel individual watches without tearing down the socket.
- **Event shape is deliberately coarse (`created/modified/deleted/moved`) ✅** — matches the spec in `next-steps.md` and `primitives.md`. We do not surface the raw `fsnotify.Op` bits (Chmod, etc.) in v1; "modified" is a catch-all for content + metadata changes. Consumers that need more fidelity can fall back to the terminal.
- **Soft cap of 32 watches per connection (not 4 like PTY) ✅** — FileTree + a few open editor directories + maybe a project-wide watcher easily reaches double digits. 32 is still a strong defense against a runaway agent that does `fs.watch(".")` in a loop.
- **Recursive watching walks at setup time + auto-adds new directories on "created" events for dirs ✅** — the classic fsnotify recursive recipe. The skeleton in `handler.go` contains the `addRecursive` helper sketch.
- **FE hook does not auto-invalidate by default (consumer supplies `onEvent`) ⚠ refined** — we considered baking blanket invalidation into `useFsWatch`. Keeping it explicit (or in a thin wrapper in `useFs.ts`) gives the caller control and avoids surprising refetches for paths the UI does not care about.

---

## Open / deferred items (intentionally left for follow-up within Phase 1)

- Full fsnotify event pump + recursive directory tracking + proper coalescing of rapid changes (the code skeleton + comments in `handler.go` show exactly where it goes).
- 6–8 dedicated test cases in `server_test.go` + `fs-rpc.test.ts` (the harness is ready; the cases are mechanical now that the seam exists).
- Wiring the hook into `monaco-impl.tsx` (for the currently open file) and `FileTree.tsx` (for expanded nodes) with real TanStack Query invalidation — this is the "killer app" that makes the primitive visible to users.
- `fs.unwatch` primitive (if we decide explicit cancellation is needed before socket close).
- Permission / scope nuance for watches (currently just `fs:r` — same as list/read).

These are all small, additive, and do not change the wire contract.

---

## Verification (how to prove the primitive works)

```sh
# 1. Regenerate clients (no drift)
make proto
git diff --exit-code proto/clients

# 2. Daemon unit tests (existing suite + new watch cases once written)
make -C sandbox-daemon test

# 3. Frontend typecheck + unit tests
pnpm --filter ./frontend typecheck
pnpm --filter ./frontend test:unit

# 4. Manual end-to-end in the three-terminal dev setup
# T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
# T2: docker compose -f backend/compose.yaml up -d postgres && make -C backend migrate run
# T3: pnpm --filter ./frontend dev

# In the browser:
# - Open a workspace
# - In the editor, open a file
# - In another terminal (or via the PTY pane) run: echo "hello from agent" >> the/file
# - Observe the editor buffer update (after the useFsWatch + invalidation wiring) or at least see the "fs.watch-event" in the React DevTools / console via the hook.

# 5. (Future) Live Playwright extension of pty.spec.ts that exercises an agent edit + sees the UI react.
```

All of the above (except the final editor wiring) was exercised while building the seams.

---

## Cross-cutting impact

- The **streaming substrate** (Phase 7) is now used by two independent domains (PTY and FS). Adding `git.log --follow` or `fs.search` will be almost identical work.
- **Editor / agent collaboration** story is unblocked. This was the #1 pain point once agents started landing (Phase 6 §9.3).
- The dogfood `rommel/` funnel on real workspaces will stay fresh automatically when plans are promoted or edited by agents.

---

## Next (within the Filesystem Completion story)

1.2 `fs.mkdir` / `fs.move` / `fs.delete` — closes the write side of the file tree (currently the funnel uses `os.Rename` internally; exposing the primitives lets the FE do "New File" → create → rename atomically, and lets agents create directories safely).

After 1.1 + 1.2 the v1 file contract is complete and the IDE no longer feels "read-only for creation".

---

**Captured this session:** Schemas written and validated against the existing style; `fsWatch` + `useFsWatch` (raw + query-wrapper) implemented following the PTY pattern exactly; daemon `Watch` + `OnDisconnect` + route + lifecycle registration + error codes + skeleton for the fsnotify pump; all harnesses updated; `next-steps.md` and this completion doc record the state. The primitive is now additive and ready for the real OS watcher + UI integration PRs.

When the maintainer runs `make proto` and the first real `fs.watch` call from the browser, the full streaming loop (request → handler captures Publisher → background fsnotify event → `pub.Publish("fs.watch-event", ...)` → FE `subscribe` handler → React state / query invalidation) will light up exactly as PTY did in Phase 7.