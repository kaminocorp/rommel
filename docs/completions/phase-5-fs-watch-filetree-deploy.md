# Phase 5 — `fs.watch` Event Pump, FileTree Context Menu, and Deployment Readiness (Completion)

**Plan:** Remaining items after Phases 0–4 (real `fs.watch` implementation deferred from Phase 1 + FileTree write UX + production deploy readiness).
**Date:** 2026-05-16
**Status:** ✅ All remaining high-leverage items from the post-Phase-4 review are complete.

This phase closes the last major gaps in the v1 filesystem story and makes the ADE ready for a real production deployment.

---

## What was built

### 1. Real `fs.watch` Event Pump (the big missing piece from Phase 1)

The skeleton in `sandbox-daemon/internal/fs/handler.go` has been replaced with a fully functional implementation:

- Proper initialization of a single `*fsnotify.Watcher` (lazy, on first `fs.watch` call).
- Reference-counted path management (`watched` map) so overlapping recursive watches from multiple connections are efficient.
- Recursive directory walking on `fs.watch(..., recursive: true)`.
- Auto-addition of newly created directories inside recursive watches (standard fsnotify recursive pattern).
- Background `runEventLoop` goroutine that processes `watcher.Events` and `watcher.Errors`.
- Event translation:
  - `Create` → `created`
  - `Write` → `modified`
  - `Remove` → `deleted`
  - `Rename` → `moved`
- Publishing of `fs.watch-event` via the per-connection `Publisher` captured at watch time.
- Correct cleanup in `OnDisconnect` (decrements refcounts and removes paths from the OS watcher when no longer needed).

The frontend `useFsWatch` hook (already present since Phase 1) now receives real events. Consumers (EditorPane, FileTree) can use `onEvent` callbacks or TanStack Query invalidation to keep the UI fresh when agents or other processes modify the filesystem.

### 2. FileTree Context Menu — Fully Wired

The scaffold from Phase 4 is now live:

- Right-click on any directory or file opens a context menu.
- Supported actions:
  - **New File** (in the clicked directory)
  - **New Folder** (in the clicked directory)
  - **Rename** (atomic `fs.move`)
  - **Delete** (with recursive confirmation for directories)
- Uses `prompt()` + `confirm()` for v1 name input (kept simple).
- All operations use the Phase 4 mutation hooks (`useFsMkdir`, `useFsMove`, `useFsDelete`) with proper TanStack Query invalidation, so the tree updates immediately.
- The root-level "New File" header button continues to work.

The FileTree is now a fully functional two-way filesystem browser.

### 3. Deployment Readiness

All changes are additive on the already-production substrate from Phase 0:

- **Workspace image**: Must be rebuilt and repushed (`make push` from `workspace-image/`) because the daemon binary now contains the real `fs.watch` pump + the three write primitives.
- **Frontend**: `pnpm build` (new FileTree context menu + `useFsMkdir` etc. already imported).
- **Proto**: `make proto` (no schema changes in this phase, but good hygiene).
- **Backend**: No changes (FastAPI control plane untouched).
- **Database**: No schema changes.

The existing Playwright e2e gates (`ping.spec.ts`, `pty.spec.ts`) plus the new filesystem operations are ready to run against a live Vercel + Fly deployment.

A short operator note was added to the completion for the required image rebuild step.

---

## Decisions & Trade-offs

- **Best-effort "moved" events** — fsnotify does not always provide the old path in a single event across platforms. For v1 we emit `type: "moved"` with the destination path. This is sufficient for most UI invalidation use cases.
- **Single global fsnotify.Watcher** — efficient and matches the design chosen in Phase 1.
- **No `fs.unwatch` primitive yet** — still relying on connection lifetime + `OnDisconnect`. Explicit unwatch can be added later if needed.
- **prompt()/confirm() for names** — acceptable for Phase 5. A proper dialog component is a small future polish item.
- **Deployment requires workspace image rebuild** — unavoidable once daemon code changes. This is documented clearly.

---

## Verification

```sh
make proto
make -C sandbox-daemon test
pnpm --filter ./frontend typecheck
pnpm --filter ./frontend test:unit

# Full manual test (three-terminal)
# T1 daemon, T2 backend, T3 frontend

# In browser:
# - Right-click folders → New File / New Folder / Rename / Delete all work
# - Create/edit a file from one terminal while watching it in the editor → live update via fs.watch-event
# - FileTree reflects all mutations in real time
```

---

## Impact

With this phase, the following are now true:

- The filesystem contract is **complete and live** (list/read/write/watch + mkdir/move/delete).
- The FileTree is a proper bidirectional IDE file browser.
- `fs.watch` finally delivers on the promise made in Phase 1 and unblocks reliable editor ↔ agent collaboration.
- The entire system (Phases 0–5) is in a state where a production deployment is not only possible but recommended for dogfooding.

---

**Captured this session:** Real fsnotify-based `fs.watch` event pump with recursive support and proper publishing; full wiring of the FileTree context menu; deployment readiness steps documented. All remaining items from the post-Phase-4 review are now closed. Phase 5 completion recorded.

The Rommel ADE is now feature-complete for v1 filesystem + terminal + git + agent dispatch, and ready to be deployed to real users on Vercel + Fly.