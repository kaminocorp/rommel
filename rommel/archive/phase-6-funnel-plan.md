# Phase 6 Plan — `rommel/` planning funnel + first real daemon primitives

Specialization of [`scaffolding-plan.md`](./scaffolding-plan.md) §6, broadened per the §0.1.5 changelog "Next" pointer: the §6 folders alone are too small to be a Phase, so this phase bundles them with the **first batch of real daemon primitives** that light up the IDE shell — `fs.list`, `fs.write`, and the `funnel.*` family — plus the frontend components that consume them.

Pattern: after this phase, every primitive in `docs/primitives.md` is an additive PR against the WS dispatch table + the corresponding `lib/<domain>.ts` typed wrapper. The "scaffolding" era closes here.

---

## 0 — Decisions to lock in before authoring

### 0.1 — `rommel/` (visible), not `.rommel/` ✅

`docs/vision.md` allows either. The kanban-on-disk concept argues for visible (it's user-facing content, not tool metadata). Confirmed with user. Changelog prose retroactively reads as referring to the visible folder; no rename needed elsewhere.

### 0.2 — Stage folder names: kebab-case ✅

`rommel/{triage,plans,next-up,executing,completions,archive}/`. Linux-friendly, no shell-quoting hazards, easy globbing. Display names ("Next Up") get formatted in the UI layer; on disk it's `next-up`.

### 0.3 — Funnel root location: convention, not configuration ✅

The daemon derives the funnel root as `<WorkspaceRoot>/rommel`. No new env var. If the folder is missing (non-rommel-style workspace), `funnel.list` returns an empty list rather than erroring — the funnel is opt-in at the workspace level. Same path-sandbox rules apply as for `fs.*`.

### 0.4 — Funnel primitives shipping this phase ✅

From `docs/primitives.md` §1.5:

- `funnel.list(stage)` — entries (name, mtime, size) under one stage
- `funnel.read(stage, name)` — markdown body of one entry
- `funnel.promote(name, from, to)` — atomic move + stage-validation

Deferred:
- `funnel.current_executing()` — syntactic sugar over `funnel.list("executing")`; UI computes it
- `funnel.write` — out of scope; `fs.write` works on `rommel/**` already since it's under the workspace root, and v1 wants the funnel UI read-mostly

### 0.5 — Stage promotion ordering: linear, with archive-anywhere ⚠ refined

`primitives.md` says "atomic move + validation" but doesn't enumerate valid transitions. v1 rules:

- Forward-only along `triage → plans → next-up → executing → completions → archive`
- Plus: anything can go to `archive` directly (kill-switch for ideas that fizzle)
- Anything else (e.g. completions → triage) returns `funnel.invalid_transition`

The daemon enforces this — keeps the FE simple (it can show all six promote buttons and let the daemon reject invalid ones).

### 0.6 — `fs.list` shape: entries with `kind` ∈ {file, dir, symlink} ✅

Mirrors POSIX `d_type`. No recursion in v1 (tree expansion = one `fs.list` per opened directory). `mtime` RFC 3339, `size` bytes. Hidden files (leading `.`) included by default — they matter for the IDE.

### 0.7 — `fs.write` shape: full-content overwrite, no patches ✅

Per `primitives.md` cross-cutting Q "patches": deferred. v1 sends full file contents on every save. `mode` field reserved for future (`create` vs `overwrite` vs `append`); v1 supports `overwrite` only.

### 0.8 — Browser-side editor: single open file, dirty state, Cmd+S save ⚠ refined

No tabs in v1 — the editor holds one file at a time; clicking a new file replaces the buffer (with a dirty-state confirm if needed; v1 just discards — explicit save is a deliberate act). Cmd+S triggers `fs.write`; the title bar pill flips to "saved" briefly. The dirty-state confirm is a follow-up.

### 0.9 — Daemon connection sharing: extend the connection store ⚠ refined

Phase 5's `useDaemonConnection` is a void hook — it creates and tears down the `DaemonConnection` inside its own effect, never exposing the instance. Phase 6 needs FileTree, FunnelBoard, and EditorPane to all share one connection. Two options:

- **A) Extend connection store with `daemon: DaemonConnection | null`** — minimal diff, no provider plumbing. Components read the connection via a zustand selector.
- B) React context provider wrapping the workspace.

Picking A. The store already exists, already lives at the same lifecycle layer, and class instances in zustand work fine as long as the reference is set once per mount and reset on unmount.

### 0.10 — Frontend tests stay at the lib/hook layer ✅

Prior phases keep tests in `tests/unit/*.test.ts` exercising library/store layers against a fake WebSocket. No RTL component tests yet. Phase 6 follows: add `fs-rpc.test.ts` and `funnel-rpc.test.ts` covering the typed-wrapper layer; the component changes get integration coverage via the existing Playwright spec extended.

---

## 1 — `rommel/` dogfood folder structure

Authoring (no compute):

```
rommel/
├── README.md              # what this is, links to docs/vision.md §Layer 2
├── triage/README.md
├── plans/README.md
├── next-up/README.md
├── executing/
│   ├── README.md
│   └── phase-6-funnel-plan.md   # this very file, duplicated (not symlinked — survives Windows clones)
├── completions/README.md
└── archive/README.md
```

Each stage README is one paragraph: "what belongs here, what comes next, who promotes."

The dogfood copy of this plan lands under `rommel/executing/` *before* implementation starts. On completion, both copies move to `archive/` — `docs/archive/` for human GitHub browsing, `rommel/archive/` for the dogfooded board.

---

## 2 — proto schemas

Fill in the stubs at `proto/schemas/fs/{list,write}.json` (currently `_todo` placeholders). Add new schemas:

```
proto/schemas/funnel/
├── list.json
├── read.json
└── promote.json
```

Each follows the `FsRead` shape: a `$defs` section with `<Title>Request` and `<Title>Response`, plus a top-level `oneOf` over them so codegen produces both. Stage enum is shared — defined inline (small enough to inline).

Re-run `make proto` to regenerate TS / Go / Pydantic clients. The codegen scripts walk the schemas tree and mirror it under `clients/<lang>/`, so no script edits needed.

---

## 3 — daemon: `fs.list`, `fs.write`

Extend `sandbox-daemon/internal/fs/handler.go`:

- `List(ctx, claims, payload) (json.RawMessage, *EnvelopeError)` — reads `req.Path`, resolves via the existing `resolve()` sandboxer, `os.ReadDir`, marshals entries with kind ∈ {file, dir, symlink}.
- `Write(ctx, claims, payload) (json.RawMessage, *EnvelopeError)` — decodes contents per encoding (utf-8 / base64), writes via `os.WriteFile` with 0o644. Creates parent dirs if missing? v1: no — write fails if parent missing, caller can `fs.mkdir` first. (`fs.mkdir` lands when needed.)

Wire into `cmd/daemon/main.go` dispatch table — replace the `NotImplemented` stubs with the real handlers.

---

## 4 — daemon: `funnel.list`, `funnel.read`, `funnel.promote`

New package `sandbox-daemon/internal/funnel/handler.go`:

- `Handler{ Root string }` where `Root` is `<WorkspaceRoot>/rommel`.
- Path safety mirrors `fs.Handler.resolve()` — stage names are validated against the enum, entry names are validated as filename-only (no `/`, no `..`, no leading `.` per v1 — funnel cards are user-content markdown, not hidden files).
- `List(req.Stage)` — `os.ReadDir(filepath.Join(Root, stage))`, returns entries. Missing stage dir returns empty list (workspace doesn't have a funnel yet).
- `Read(req.Stage, req.Name)` — `os.ReadFile`, returns the markdown body. Caps at 1 MiB to avoid an editor DoS.
- `Promote(req.Name, req.From, req.To)` — validates the transition table from §0.5, then `os.Rename` from `<Root>/<from>/<name>` to `<Root>/<to>/<name>`. Atomic on POSIX.

Three new stable error codes in `ws/envelope.go`:

```
ErrCodeFunnelInvalidStage      = "funnel.invalid_stage"
ErrCodeFunnelInvalidTransition = "funnel.invalid_transition"
ErrCodeFunnelInvalidName       = "funnel.invalid_name"
```

Wire into `cmd/daemon/main.go` with `funnel:r` / `funnel:rw` scopes (already in the session-token enum since Phase 1).

---

## 5 — daemon tests

Extend `sandbox-daemon/internal/ws/server_test.go`:

- `TestFsList_HappyPath` — seed a few files in tempdir, list, assert entries.
- `TestFsList_NotADir` — list a file path; assert `fs.invalid_path`.
- `TestFsList_NotFound` — assert `fs.not_found`.
- `TestFsWrite_Creates` — write a new file, then `fs.read` it back, assert contents.
- `TestFsWrite_Overwrites` — write twice, assert second wins.
- `TestFsWrite_AbsolutePath_Rejected` — `fs.invalid_path`.
- `TestFsWrite_InsufficientScope_Forbidden` — token with only `fs:r`.

New `sandbox-daemon/internal/funnel/handler_test.go` (or extend server_test.go — same harness):

- `TestFunnelList_HappyPath` — seed entries under `rommel/triage/`, list, assert.
- `TestFunnelList_MissingStage_ReturnsEmpty` — no `rommel/` at all → empty list, not error.
- `TestFunnelList_InvalidStage_Rejected` — stage="nope" → `funnel.invalid_stage`.
- `TestFunnelRead_HappyPath`.
- `TestFunnelRead_InvalidName_Rejected` — name="../etc/passwd" → `funnel.invalid_name`.
- `TestFunnelPromote_HappyPath` — triage → plans; file appears under plans.
- `TestFunnelPromote_BackwardsRejected` — plans → triage → `funnel.invalid_transition`.
- `TestFunnelPromote_ArchiveFromAnywhere` — completions → archive (allowed).

---

## 6 — frontend: typed wrappers + hooks

New files:

- `src/lib/fs.ts` — typed wrappers: `fsList(conn, path)`, `fsRead(conn, path)`, `fsWrite(conn, path, contents)`. Each calls `conn.rpc(...)` with the generated request type and unwraps the response. ~30 lines.
- `src/lib/funnel.ts` — same shape: `funnelList`, `funnelRead`, `funnelPromote`.
- `src/hooks/useFs.ts` — TanStack Query wrappers over the lib functions. `useFsList(path)`, `useFsRead(path)`, `useFsWrite()`. `useFsList` is keyed `["fs", "list", path]`; invalidation on write.
- `src/hooks/useFunnel.ts` — `useFunnelList(stage)`, `useFunnelRead(stage, name)`, `useFunnelPromote()`. Keys: `["funnel", "list", stage]`, `["funnel", "read", stage, name]`. `useFunnelPromote` invalidates the source + destination lists.

These hooks read the shared `DaemonConnection` via `useConnectionStore((s) => s.daemon)`.

### Connection-store extension

Add `daemon: DaemonConnection | null` field + `setDaemon` setter to `src/stores/connection.ts`. Reset to `null` in `reset()`. `useDaemonConnection.ts` sets it after `connect()` resolves and clears it on unmount.

---

## 7 — frontend: FileTree, EditorPane (with save), FunnelBoard

### FileTree

Replaces the stub. Lists top-level via `useFsList(".")`, renders entries with an indent + chevron for dirs. Clicking a dir toggles expansion (one `fs.list` per opened dir, no recursion). Clicking a file calls a new store action `selectFile(path)` — EditorPane reads selected path from the store.

### EditorPane

Currently inert. Phase 6 adds:

- Reads `selectedFile` from the connection store.
- When `selectedFile` changes, calls `fsRead` and replaces the Monaco buffer.
- Keyboard binding: Cmd/Ctrl+S → `fsWrite` with the current buffer contents. Toast or pill flash on success.
- "Welcome" buffer remains when no file is selected.

### FunnelBoard

Replaces the stub. Six columns, one per stage. Each column calls `useFunnelList(stage)` and renders entries as cards (name + truncated body preview via `useFunnelRead` on hover/expand — but v1 just shows the name to avoid 6× N RPCs on mount).

Each card has a `Promote →` dropdown showing the valid next stages per §0.5. Selecting triggers `useFunnelPromote`. Invalid transitions are filtered client-side from the dropdown; the daemon enforces server-side regardless.

### Header toggle: IDE / Funnel

Add a two-button toggle in `Header.tsx` — `IDE` (default) and `Funnel`. Local state in `WorkspaceClient`. When `view === "funnel"`, the editor+terminal grid is replaced by `<FunnelBoard />`. File tree stays visible in both modes (still useful as nav).

---

## 8 — frontend tests

Extend `tests/unit/`:

- `fs-rpc.test.ts` — exercise the new `lib/fs.ts` wrappers through the FakeWebSocket pattern. Covers the success path, the error envelope path, and the encoding=base64 round-trip.
- `funnel-rpc.test.ts` — same shape for `lib/funnel.ts`.
- Extend `connection-store.test.ts` with the new `daemon` field + `setDaemon` action.

Component tests deferred — the Playwright spec extension below covers integration.

### Playwright extension (gated on `RUN_E2E`)

Add to `tests/e2e/ping.spec.ts` (or a new `tests/e2e/funnel.spec.ts`): after sign-in, navigate to workspace, click "Funnel", assert at least one stage column renders, click "IDE" to return, click a file in the file tree, assert Monaco shows its contents. CI: same opt-in gate.

---

## 9 — Risks

- **9.1 — `os.Rename` across mount points.** On Fly Machines the workspace is one volume, so triage/ and plans/ are guaranteed on the same FS. `os.Rename` is atomic. If we ever support multi-volume workspaces, fall back to copy+delete.
- **9.2 — fs.write parent-dir-missing UX.** Returns `fs.not_found` (parent doesn't exist) — confusing-but-correct. Mitigation: an `fs.mkdir` primitive lands in a follow-up; meanwhile, the FE doesn't expose a "create file in new folder" affordance in v1.
- **9.3 — Editor buffer / on-disk drift.** Without `fs.watch`, an out-of-band edit (terminal `echo > foo`) won't refresh the editor. v1 documents this in the README; `fs.watch` is the structural fix in a later phase.
- **9.4 — Funnel card content size cap.** 1 MiB hard limit on `funnel.read` — keeps a runaway markdown file from oom-ing the browser. Returned as `fs.io` for now (since the daemon errors at read time).
- **9.5 — Connection-store class-instance reference equality.** Zustand re-renders on shallow inequality; if `setDaemon` is called more than once per mount, components depending on it re-render. Mitigation: set exactly once after `connect()` resolves, clear once on unmount.

---

## 10 — Verification (the integration gate)

```sh
# Daemon unit suite (hermetic, no network):
make -C sandbox-daemon test

# Frontend unit suite (hermetic, FakeWebSocket):
pnpm --filter ./frontend test:unit

# Three-terminal end-to-end:
#   T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
#       (the daemon root happens to BE the rommel repo, so the funnel folders are real)
#   T2: docker compose -f backend/compose.yaml up -d postgres && make -C backend migrate run
#   T3: pnpm --filter ./frontend dev
# Browser: sign in → open the dev workspace → file tree lists the repo root, click rommel/executing/phase-6-funnel-plan.md → editor opens it → toggle "Funnel" → six columns render → promote phase-6-funnel-plan.md from executing to completions → board updates.
```

Network-bound items still deferred: live Playwright CI run, Vercel preview deploy.

---

## 11 — Out of scope (called out so they don't sneak in)

- `fs.mkdir`, `fs.move`, `fs.delete`, `fs.stat`, `fs.watch`, `fs.search` — additive PRs.
- `funnel.current_executing()` — computed FE-side from `funnel.list("executing")`.
- `funnel.write` — `fs.write` covers it.
- Tabs in the editor; dirty-state confirm modal.
- Drag-and-drop promote in the funnel UI (keyboard + button only in v1).
- File-tree virtualization (will matter past ~1000 entries).
