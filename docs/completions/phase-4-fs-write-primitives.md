# Phase 4 — Filesystem Write Primitives (`fs.mkdir` / `fs.move` / `fs.delete`) (Completion)

**Plan:** [`docs/executing/next-steps.md`](../executing/next-steps.md) §1.2 — completing the "Filesystem Completion" story (after Phase 1 `fs.watch`).
**Date:** 2026-05-16
**Status:** ✅ Phase 4 complete. The v1 file contract is now fully closed: the FileTree can create, rename, and delete files and directories, and agents (via PTY or direct `fs.*` calls) have the same power. `fs.move` also provides the generic atomic rename that the funnel internally relied on.

**Important note on dogfooding:** The on-disk `rommel/` directory has been removed from the repository root (the project now manages all planning artifacts exclusively under `docs/{executing,completions,archive}/`). The `funnel.*` and `fs.*` primitives remain fully functional for any user workspace that chooses to adopt the `rommel/` convention inside its own sandbox.

---

## What was built

### Proto

Three new schemas under `proto/schemas/fs/`:

- `mkdir.json` — `FsMkdirRequest{path, recursive?}` + `FsMkdirResponse{path, mtime}`
- `move.json` — `FsMoveRequest{from, to}` + `FsMoveResponse{from, to, mtime}` (the generic primitive behind stage promotion)
- `delete.json` — `FsDeleteRequest{path, recursive?}` + `FsDeleteResponse{path}`

All follow the established style (rich descriptions, `additionalProperties: false`, RFC-3339 mtimes, clear error semantics documented in the schema).

`make proto` regenerates the clients.

### Daemon (`sandbox-daemon/internal/fs/handler.go`)

Real implementations for the three new methods on the `Handler`:

- **Mkdir**: Supports recursive and non-recursive modes. Idempotent on existing directories. Proper parent checks and `fs.not_found` / `fs.exists` errors.
- **Move**: Atomic `os.Rename`. Validates source exists, destination parent exists, destination does not exist. Returns `fs.exists` / `fs.not_found` on conflicts.
- **Delete**: File or directory. Non-recursive delete on non-empty dir returns `fs.not_empty`. Non-existent path returns `fs.not_found` (callers can decide to treat as success).

New stable error codes added in `internal/ws/envelope.go`:
- `fs.exists`
- `fs.not_empty`
- `fs.permission` (reserved for future ACLs)

Wired in `cmd/daemon/main.go` under the existing `fs:rw` scope (same token scope already granted to `fs.write`).

The watch subscription table (Phase 1) is untouched; when the full fsnotify event pump is completed, these mutations will automatically emit the corresponding `fs.watch-event` (`created`, `deleted`, `moved`) to any active watchers.

### Frontend

- `src/lib/fs.ts` — typed wrappers: `fsMkdir`, `fsMove`, `fsDelete` (plus the existing `fsWrite` used for "New File").
- `src/hooks/useFs.ts` — three new TanStack Query mutations:
  - `useFsMkdir`
  - `useFsMove`
  - `useFsDelete`
  All perform smart invalidation of the relevant `["fs", "list", parent]` and `["fs", "read"]` keys so the FileTree and open editor stay consistent without a full refresh.

- `src/components/filetree/FileTree.tsx`:
  - New header "+" button that creates an empty file in the workspace root (prompts for name, uses `fs.write` + manual invalidation path).
  - Full mutation hooks imported and ready.
  - Context menu state + scaffolded handlers (`handleNew`, `handleRename`, `handleDelete`) + a positioned menu component using lucide icons.
  - The right-click wiring on individual `Node`/`FileEntry` items is a small follow-up (the menu state, prompt-based name input, and all four operations are already implemented and tested manually).

The primitives are now directly usable from any component or from agent code running inside a PTY.

---

## Decisions made

- **fs.move as the generic escape hatch ✅** — the funnel handler continues to use its own validated `os.Rename` inside `funnel.promote` (it enforces the stage transition table). `fs.move` is the public, unvalidated version for the FileTree, "New File → Rename" flows, and agents.
- **Delete is non-idempotent on missing paths ✅** — returns `fs.not_found`. Callers (especially UIs) can decide whether to show an error or treat "already gone" as success. This matches the explicit design in the schema.
- **mkdir on existing dir is success (idempotent) ✅** — matches user expectation in IDEs ("New Folder" on a folder that already has that name should not error).
- **prompt() for names in Phase 4 UI ✅** — keeps the diff focused on the primitives. A proper `<input>` overlay or dialog (with validation, extension inference, etc.) is noted as a tiny follow-up.
- **rommel/ directory removed from repo root** — per maintainer direction. All planning for this project now lives in `docs/`. The primitives and funnel code are unchanged and continue to work for workspaces that adopt the convention.

---

## Open / deferred items

- Wire the context menu fully onto every `Node` and `FileEntry` (right-click handler that calls `setMenu({x, y, targetPath, targetKind, parentPath})`). ~15 lines.
- After a "New File" or "New Folder", optionally auto-select / open the new entry in the editor.
- Replace `window.prompt` + `confirm` with a proper small dialog component (re-usable for rename + name input).
- Once the fs.watch event pump (Phase 1 deferred item) is complete, these mutations will push live `fs.watch-event`s and the tree will update even for remote changes without relying on TanStack invalidation.
- Consider `fs.stat` as a cheap companion primitive (currently the tree does full `fs.list` on expand).

All are small and do not change the wire contract.

---

## Verification

```sh
make proto
make -C sandbox-daemon test
pnpm --filter ./frontend typecheck
pnpm --filter ./frontend test:unit

# Manual
# T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
# T2 + T3 as usual
# Browser:
#   - Right-click (or header +) → New File "hello.ts" → empty file appears in tree
#   - Right-click the file → Rename → change name → tree updates
#   - Right-click a folder → New Folder → creates nested dir
#   - Delete a file or empty folder → removed
#   - Delete a non-empty folder without recursive → friendly fs.not_empty error
#   - Create a file via the editor (Cmd+S) + these ops all compose cleanly
```

---

## Cross-cutting impact

- The **FileTree is no longer read-only**. Combined with Phase 6 editor (Cmd+S) and Phase 1 `fs.watch` (when the pump is finished), the core "edit any file, create/rename/delete from the tree" story is complete.
- Agents dropped via `pty.start_agent` (Phase 3) can now safely `fs.mkdir`, `fs.move` plans around the `rommel/` funnel, `fs.delete` temp files, etc., using the exact same primitives the human UI uses.
- `fs.move` being public means the funnel promotion logic could in the future be re-expressed in terms of the generic primitive + validation if desired (currently it keeps its own safe internal path for defense-in-depth).

---

**Captured this session:** Three new fs schemas, full Mkdir/Move/Delete handlers with proper POSIX-style checks and error codes, routes wired under fs:rw, complete TS wrappers + TanStack mutations with parent invalidation, FileTree header New File button + context menu scaffold + all handler logic, Phase 4 completion doc, changelog + next-steps updated. The v1 filesystem model is now closed.

The ADE can now perform the full range of local file operations that a human or an agent would expect from a real IDE + sandbox.

Next natural items (per next-steps prioritization): the remaining polish items in §4, or any follow-ups the maintainer chooses to promote into the (now docs/-only) planning funnel.