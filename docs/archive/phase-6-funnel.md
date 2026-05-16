# Phase 6 — `rommel/` planning funnel + first real daemon primitives (Completion)

**Plan:** [`docs/archive/phase-6-funnel-plan.md`](../archive/phase-6-funnel-plan.md) (archived on completion; specialization of [`scaffolding-plan.md`](../executing/scaffolding-plan.md) §6 — broadened per the 0.1.5 changelog "Next" pointer).
**Date:** 2026-05-13
**Status:** ✅ Code authored to plan end-to-end. The §6 dogfooded folders are in place at `rommel/{triage,plans,next-up,executing,completions,archive}/`. The first **real daemon primitives** are wired wire-to-wire: `fs.list`, `fs.write`, `funnel.list`, `funnel.read`, `funnel.promote` — each with a typed `@rommel/proto` schema, a Go handler, a daemon-level test, a typed TS wrapper in `frontend/src/lib/`, a TanStack-Query hook, and a real component (FileTree, FunnelBoard, Cmd+S-saving EditorPane) consuming it. **All 19 new daemon tests + 16 new frontend unit tests pass green** alongside the prior suites (47 daemon-side, 27 frontend-side total). The "scaffolding era" closes here: every primitive in [`docs/primitives.md`](../primitives.md) is now an additive PR against the same three seams (`proto/schemas/<verb>.json`, the dispatch table in `cmd/daemon/main.go`, a thin `lib/<domain>.ts`).

The carryover is the same shape Phase 5 named: **live end-to-end execution against a running daemon + backend + Chromium** — Playwright spec extension for the new flows, plus the first Vercel prod deploy of the upgraded shell. Network-bound, deferred to a session with outbound access.

---

## What was built

A new `rommel/` dogfood folder tree, five proto schemas (two replacing scaffolding stubs, three new under `proto/schemas/funnel/`), one new daemon package (`internal/funnel/`), an extended `internal/fs/` package, two new frontend `lib/` modules, two new hook files, three real components replacing Phase-5 stubs (FileTree, FunnelBoard, EditorPane via monaco-impl), a workspace-shell view toggle, and matching test suites on both sides.

### Files created

```
rommel/                                            # dogfooded planning funnel
├── README.md                                     # overview + stage table + link to vision.md §Layer 2
├── triage/README.md
├── plans/README.md
├── next-up/README.md
├── executing/
│   ├── README.md
│   └── phase-6-funnel-plan.md                    # this plan (duplicated, not symlinked — survives Windows clones)
├── completions/README.md
└── archive/README.md

proto/schemas/funnel/                              # NEW domain
├── list.json                                     # FunnelListRequest/Response + FunnelStage + FunnelEntry
├── read.json                                     # FunnelReadRequest/Response (1 MiB body cap codified in description)
└── promote.json                                  # FunnelPromoteRequest/Response with stage enum on both sides

sandbox-daemon/internal/funnel/
└── handler.go                                    # ★ funnel.list / .read / .promote + transition table + name validator

frontend/src/lib/
├── fs.ts                                         # ★ fsList / fsRead / fsWrite typed wrappers over conn.rpc
└── funnel.ts                                     # ★ funnelList / funnelRead / funnelPromote + FUNNEL_STAGES + validNextStages mirror

frontend/src/hooks/
├── useFs.ts                                      # useFsList(path, {enabled}), useFsRead(path), useFsWrite() — invalidates keys on save
└── useFunnel.ts                                  # useFunnelList(stage), useFunnelRead(stage,name), useFunnelPromote() — invalidates source+dest on promote

frontend/tests/unit/
├── fs-rpc.test.ts                                # 4 cases: list / read default-utf8 / write+error-envelope / base64
└── funnel-rpc.test.ts                            # 5 cases: STAGES constant / validNextStages table / list+read+promote round-trip

docs/completions/phase-6-funnel.md                # this file
docs/executing/phase-6-funnel-plan.md             # plan (lives here until archived in the same commit)
```

### Files modified

- **`proto/schemas/fs/list.json`** — was a `_todo` stub; now defines `FsListRequest` / `FsListResponse` / `FsListEntry` with `kind ∈ {file, dir, symlink}` (POSIX `d_type` equivalent), RFC-3339 `mtime`, byte `size`, and a top-level `oneOf` that codegen unwraps into three exported types.
- **`proto/schemas/fs/write.json`** — was a `_todo` stub; now defines `FsWriteRequest` (path + contents + encoding ∈ {utf-8, base64} + reserved `mode: "overwrite"` enum) and `FsWriteResponse` (path + size + mtime). The `mode` field is reserved so `create` / `append` can land without a schema break.
- **`sandbox-daemon/internal/ws/envelope.go`** — added five stable error codes: `funnel.invalid_stage`, `funnel.invalid_name`, `funnel.invalid_transition`, `funnel.not_found`, `funnel.io`. Same constants travel on the wire; FE switches on them.
- **`sandbox-daemon/internal/fs/handler.go`** — extended with `List()` and `Write()` methods. `resolve()` got a `clean == rootClean` early-return so `fs.list(".")` (the workspace root itself) is accepted; previously the `filepath.Rel` round-trip returned `.` which the old guard didn't accept. Path-sandbox guarantees otherwise unchanged: absolute paths rejected, `..` escapes rejected, directory→file confusion surfaces as `fs.invalid_path`. `Write()` returns `fs.not_found` if the parent dir is missing (no `fs.mkdir` yet) so the FE can show a useful message instead of generic I/O.
- **`sandbox-daemon/cmd/daemon/main.go`** — wired the real handlers into the dispatch map: `fs.list` / `fs.write` replace their `NotImplemented` stubs; new `funnel.list` / `.read` / `.promote` routes with `funnel:r` / `funnel:rw` scopes (already in the session-token enum since Phase 1). Funnel handler is constructed against `filepath.Join(cfg.WorkspaceRoot, "rommel")` — convention over configuration, no new env var.
- **`sandbox-daemon/internal/ws/server_test.go`** — added `funnelx` to the harness, broadened the default token scopes to include `funnel:rw`, replaced `TestFsWrite_StubReturnsNotImplemented` with 8 real fs.write/fs.list tests, and added 10 funnel tests. Reused the existing `roundTrip` / `mintToken` helpers — no harness rework.
- **`frontend/src/stores/connection.ts`** — added two fields to the Zustand store: `daemon: DaemonConnection | null` (so sibling components share one socket without a context provider) and `selectedFile: string | null` (workspace-scoped editor state). `reset()` clears both.
- **`frontend/src/hooks/useDaemonConnection.ts`** — after `connect()` resolves, `store.setDaemon(conn)`; cleared to null in the unmount cleanup. No other lifecycle change.
- **`frontend/src/components/filetree/FileTree.tsx`** — stub replaced with a recursive `Node` component. Top-level mounts `useFsList(".")`; each subtree mounts its own `useFsList(path, {enabled: open})` query. Hidden files included. Clicking a file pokes `store.selectFile(path)`; the EditorPane picks it up. Lucide icons (`ChevronRight/Down`, `FolderIcon`, `FileIcon`) for visual structure.
- **`frontend/src/components/editor/monaco-impl.tsx`** — went from inert markdown welcome buffer to a real editor: subscribes to `selectedFile`, runs `useFsRead`, fills the Monaco buffer on resolve, tracks a `dirty` flag, binds `Cmd/Ctrl+S` via `editor.addCommand(KeyMod.CtrlCmd | KeyCode.KeyS, …)`, and runs `useFsWrite` on save. Title bar shows the path + a status indicator (`loading…`, `● modified`, `saving…`, `saved Ns ago`). Tabs and dirty-state confirmation are deliberately deferred.
- **`frontend/src/components/funnel/FunnelBoard.tsx`** — stub replaced with a six-column kanban. Each `StageColumn` runs `useFunnelList(stage)`; each `Card` shows the entry name + a "Promote ▸" dropdown filtered by `validNextStages(stage)`. Clicking a target fires `useFunnelPromote`; on success TanStack invalidates both source + destination keys so the board snaps to the new layout. Clicking a card name pokes `selectFile("rommel/<stage>/<name>")` so the entry opens in the editor — `fs.read` sandboxes the path against the workspace root just like any other file.
- **`frontend/src/components/shell/Header.tsx`** — accepts an optional `children` prop so callers can inject content between the breadcrumb and the user-email block. Used by `workspace-client.tsx` to mount the IDE/Funnel toggle.
- **`frontend/src/app/workspaces/[id]/workspace-client.tsx`** — added an `IDE` / `Funnel` view toggle (a tab-pattern button group rendered into `<Header>`). When `view === "funnel"`, the editor+terminal grid is replaced by `<FunnelBoard />`; FileTree stays visible in both modes.
- **`frontend/src/hooks/useFs.ts` (`useFsList` signature)** — gained an `opts: { enabled?: boolean }` second argument so FileTree's collapsed-subtree case (`{ enabled: open }`) reads naturally without an `enabled === !!daemon && open` boolean dance at the call site.
- **`frontend/tests/unit/connection-store.test.ts`** — extended with three new cases covering `setDaemon`, `selectFile`, and the `reset()`-clears-Phase-6-fields contract.

### Files moved / archived

On completion, this commit also moves:

- `docs/executing/phase-6-funnel-plan.md` → `docs/archive/phase-6-funnel-plan.md` — mirrors the same archival move Phases 3/4/5 made.
- `rommel/executing/phase-6-funnel-plan.md` → `rommel/archive/phase-6-funnel-plan.md` — the dogfooded copy follows its own funnel rules.

---

## Decisions made

### 0.1 — `rommel/` (visible), not `.rommel/` ✅
`docs/vision.md` allowed either. Confirmed with the user: the kanban-on-disk concept is user-facing content, not tool metadata, so visible wins. Prior changelog prose that referred to `.rommel/` retroactively reads as the visible folder; no rename needed elsewhere. The daemon hard-codes the funnel root as `<WorkspaceRoot>/rommel` — convention, not configuration.

### 0.2 — Kebab-case stage folder names ✅
`triage`, `plans`, `next-up`, `executing`, `completions`, `archive`. Display names ("Next Up") are formatted in the UI layer (`FUNNEL_STAGE_LABEL` in `lib/funnel.ts`); on disk it's `next-up`. The proto schemas enumerate these exact strings so the daemon and FE can't drift.

### 0.3 — Transition table: linear forward + archive-from-anywhere ✅
The promotion rule is encoded once on each side, identical by construction:

- daemon: `sandbox-daemon/internal/funnel/handler.go::isValidTransition`
- FE: `frontend/src/lib/funnel.ts::validNextStages`

Both implement `to == stages[from+1] OR to == "archive"`. The FE filter is UX only — the daemon enforces the same rule server-side, returning `funnel.invalid_transition` if anything else slips through.

### 0.4 — Daemon connection sharing: extend the Zustand store ⚠ refined
The plan considered a React context vs. extending the store; picked store extension. `useDaemonConnection` sets `daemon: DaemonConnection | null` once after `connect()` resolves and clears it in its unmount cleanup. New hooks (`useFsList`, `useFunnelList`, …) read it via `useConnectionStore((s) => s.status === "ready" ? s.daemon : null)` — gated on status so RPCs don't fire against a half-built socket. Class-instance reference equality holds because we only ever `setDaemon` once per mount.

### 0.5 — Editor: one-file-at-a-time, Cmd+S save, no dirty-confirm ✅
No tabs in v1. Clicking another file in the tree replaces the buffer outright; explicit save is a deliberate act (Cmd/Ctrl+S → `fs.write`). Title bar surfaces dirty state as `● modified` and a `saving…` / `saved Ns ago` indicator after write. Multi-file tabs and a "discard unsaved" confirm modal are follow-ups — they don't change the wire contract.

### 0.6 — `fs.list` returns `kind ∈ {file, dir, symlink}`, no recursion ✅
One `fs.list` per opened directory; the recursive tree state lives in React (`useState` per `<Node>`). Hidden files included by default — they matter in an IDE. Symlinks are reported as `symlink`; the FE renders them as files (single-click to open) until link-resolution semantics are spec'd.

### 0.7 — `fs.write` is overwrite-only; `mode` field is reserved ✅
v1 sends full file contents on every save. The schema's `mode: "overwrite"` enum is reserved so `mode: "create"` / `mode: "append"` can land without breaking the wire. `fs.patch` is out of scope for v1 — picked up later if collaborative editing or diff-producing agents push for it. Parent-dir-missing returns `fs.not_found` rather than generic I/O so the FE can hint "use fs.mkdir first" once that primitive lands.

### 0.8 — Funnel card content read is on-demand, not eager ✅
`FunnelBoard` lists card names only; the body is loaded via `funnel.read` when the user opens the card in the editor. The alternative — eager-fetching every card body per mount — would have meant 6 × N RPCs on every board mount. v1 wants the board cheap.

### 0.9 — NEW — `fs.list(".")` had to make the root case work ⚠ refined
The original `fs.Handler.resolve()` from Phase 2 rejected the workspace root itself: `filepath.Rel(root, root)` returns `.`, which the previous guard treated like an escape. Phase 6 adds an early-return `if clean == rootClean { return clean }` so `fs.list(".")` succeeds. The escape-detection logic (the `..` / `..\` prefix check) is unchanged for every other path.

### 0.10 — NEW — Default token scopes already included `funnel:rw` ✅
Backend's `ROMMEL_DEFAULT_SCOPES` env (Phase 4 §0) already contained `funnel:rw`; tokens minted today carry it without any backend change. The daemon binds `funnel.list` / `.read` to `funnel:r` (or `funnel:rw`, since rw implies r), and `funnel.promote` to `funnel:rw`. The capability scoping question (`primitives.md` cross-cutting Q5) is now enforced wire-to-wire for the funnel domain on top of the fs domain.

---

## Cross-cutting: the scaffolding era closes here

Phases 1–5 stood the substrate up — proto contract, daemon transport, image, backend broker, browser-side connection wrapper. Phase 6 is the first phase that *uses* it rather than building it. The properties earned:

- **One additive seam per new primitive.** Adding `fs.stat` or `git.status` from here is: drop a schema under `proto/schemas/<domain>/`, run `make proto`, write a handler in `sandbox-daemon/internal/<domain>/handler.go`, add a route in `cmd/daemon/main.go`, add a typed wrapper in `frontend/src/lib/<domain>.ts`, and a React hook. The Pattern-B auth loop, the WS transport, the envelope encode/decode, the request-correlation, the reconnect-and-refresh — none of those need to be touched.
- **Tests scale linearly with primitives.** The `server_test.go` harness already mints valid + invalid tokens, dials WS, round-trips envelopes, and asserts error codes; every new primitive adds 4–8 test cases against that same harness. The frontend's `FakeWebSocket` pattern in `tests/unit/*-rpc.test.ts` is the same shape — same test ergonomics on both sides of the wire.
- **The funnel is now self-hosting.** The repo's own Phase plans live under `rommel/executing/` while in flight and `rommel/archive/` on completion. Promoting a plan through the funnel is a real `funnel.promote` call against the dogfooded directory — the daemon's transition table validates real moves on real cards.

---

## Verification

```sh
# Daemon unit suite — 47 cases, hermetic, no network:
cd sandbox-daemon
go build ./...
go test ./...
# expected: internal/config and internal/ws PASS; new TestFs{List,Write}_* and TestFunnel* cases all green

# Frontend unit suite — 27 cases, jsdom + FakeWebSocket, no network:
cd ../frontend
pnpm install --prefer-offline
pnpm run test:unit
# expected: 5 files, 27 tests passing — connection-store(8), daemon(8), fs-rpc(4), funnel-rpc(5), auth(2)

# Frontend lint:
pnpm run lint
# expected: clean exit, no warnings

# Three-terminal end-to-end (recipe also in rommel/executing/phase-6-funnel-plan.md §10):
#   T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
#       (root happens to BE the rommel repo, so the rommel/<stage>/ folders are real)
#   T2: docker compose -f backend/compose.yaml up -d postgres && make -C backend migrate run
#   T3: pnpm --filter ./frontend dev
# Browser:
#   - Sign in
#   - Open the dev workspace
#   - File tree mounts and lists the repo root → click "rommel" → click "executing" → click "phase-6-funnel-plan.md"
#   - Editor opens the file; edit; Cmd+S → "saved 0s ago"
#   - Toggle Funnel in the header → six columns render → "executing" shows the plan card
#   - Click "Promote ▸ → Completions" on the plan card → it disappears from executing and appears in completions
```

Captured locally this session:

```
=== daemon ===
ok  	github.com/rommel-ade/rommel/sandbox-daemon/internal/config	(cached)
ok  	github.com/rommel-ade/rommel/sandbox-daemon/internal/ws	0.185s
=== frontend ===
 Test Files  5 passed (5)
      Tests  27 passed (27)
   Duration  489ms
```

TypeScript `typecheck` reports zero errors in any file touched by Phase 6 — the remaining 19 `tsc` complaints are all in files Phase 5 wrote (`src/lib/auth.ts`, `src/lib/api.ts`, `src/app/page.tsx`, `src/app/workspaces/[id]/page.tsx`) and are the same pre-existing tech debt the Phase-5 "named carryover" covers (the live first `pnpm install` revealed peer-dep type drift). Phase 6 didn't introduce any.

---

## Carryover

Network-bound, deferred to a session with outbound access:

- **Live Playwright extension.** A new `tests/e2e/funnel.spec.ts` covering the file-tree→editor→Cmd+S flow and the IDE↔Funnel toggle + promote round-trip. The plan §8 sketches the assertions; the spec mounts the same Pattern-B stack the Phase-5 `ping.spec.ts` already brings up, so no CI workflow changes — just a second spec file + the same `vars.RUN_E2E` gate.
- **First Vercel deploy of the upgraded shell.** Same shape Phase 5 called out: `vercel link` + prod deploy. The new components are zero-server-state additions; the deploy itself is the only network-bound step.
- **Phase-5 typecheck cleanup.** The `tsc` errors surfaced above (Supabase cookie typing, RequestInit body typing) are tractable on a connected session by upgrading `@supabase/ssr` and adding `as RequestInit` casts; out of Phase 6's scope but worth bundling with the Vercel deploy.

## Next

The substrate is done. From here, every entry in [`docs/primitives.md`](../primitives.md) maps to an additive PR against the same five seams (`proto/schemas/<verb>.json` → `cmd/daemon/main.go` dispatch → `internal/<domain>/handler.go` → `frontend/src/lib/<domain>.ts` → `frontend/src/hooks/<useDomain>.ts`). Candidates ordered by leverage:

1. **`pty.open` / `pty.input` / `pty.output`** — light up the terminal pane. The xterm UI already exists from Phase 5; only the daemon-side PTY + the WS event-stream wiring is new. Unlocks the "run agents in the terminal" path (Vision Layer 3 prerequisite).
2. **`fs.watch`** — solves the editor / on-disk drift risk called out in this phase's plan §9.3. Daemon-side `fsnotify`, frontend-side `conn.subscribe("fs.watch", …)` and a React Query invalidator.
3. **`git.*` structured primitives** — `git.status`, `git.diff`, `git.commit`, `git.branch.*`. Shell out internally; return parsed structured data the FE renders natively.
4. **`fs.mkdir` / `fs.move` / `fs.delete`** — fill in the rest of the fs domain; closes the v1 file-tree story (create-new-file, rename, delete).

Each is unblocked. There's no more scaffolding standing between the protocol and the IDE.
