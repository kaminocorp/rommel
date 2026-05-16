# Next Steps — Post Phase 7 Roadmap

**Status**: Scaffolding era closed (Phase 6). Streaming substrate proven (Phase 7). All major additive primitives (Phases 0–5) complete. Project is now in **polish + dogfood + hardening** mode.

**Dogfood funnel state**: `rommel/{triage,plans,next-up,executing}/` are empty. All active work is tracked here until promoted into the funnel.

---

## 0. Production Cutover (Blocking for Real Usage)

**Status**: ✅ Completed in Phase 0 (see [`docs/completions/phase-0-production-cutover.md`](./completions/phase-0-production-cutover.md)).

These items must land before the ADE is usable outside local dev. The concrete proxy choice for 0.1 was direct Flycast public WSS exposure on the `rommel-workspaces` app (fastest path to a working `https://rommel.vercel.app` experience; token remains the security boundary). A dedicated relay proxy can be a later hardening item if stricter isolation is required.

### 0.1 Flycast `wss://` Proxy (Phase-5.5, deferred)
**Problem**: Browser cannot resolve `wss://<wid>.vm.rommel-workspaces.internal:7777`. Fly `.internal` DNS is private-network only.

**Options**:
- **Flycast** (recommended in plan): `fly proxy 7777 --app rommel-workspaces` or a dedicated Flycast service that terminates TLS and forwards to the per-machine daemon. Browser hits a public `wss://ws.rommel.dev/<wid>` or `wss://<wid>.workspaces.rommel.dev`.
- **Public `[[services]]` on `rommel-workspaces`**: Simpler but exposes every workspace machine on the internet (token is still the auth gate). Rejected in Phase 3 for defense-in-depth reasons.
- **Tailscale / Headscale sidecar**: Overkill for v1.

**Artifacts**:
- Update `backend/api/config.py` + `fly.toml` for the new daemon URL template shape.
- Update `workspace-image/fly.toml` to expose the port publicly (if chosen) or add Flycast config.
- Update `frontend/src/lib/env.client.ts` and `useDaemonConnection.ts` if the URL shape changes.
- Add a small e2e test that the proxy route is reachable from a browser context.

**Risks**: 4.4 (Phase 5 plan), 4.7 (`.internal` DNS label).

---

### 0.2 First Production Deploy + Live Playwright Gate
**FE**:
- `pnpm --filter ./frontend build` passes on Vercel (named carryover from Phase 5).
- Live Playwright `tests/e2e/pty.spec.ts` (extend `ping.spec.ts`) against real backend + real Fly machine.
- Fix remaining `pnpm typecheck` errors (17 pre-Phase-7 issues in Supabase cookie typing + `RequestInit` body typing).

**BE**:
- `fly deploy` from `backend/` to `rommel-backend` (fly.toml already exists).
- Secrets configured: `ROMMEL_TOKEN_PRIVKEY`, `ROMMEL_FLY_API_TOKEN`, `ROMMEL_DATABASE_URL`, `ROMMEL_SUPABASE_JWKS_URL`, `ROMMEL_DAEMON_URL_TEMPLATE`.
- Migrations run via `release_command`.

**Workspace image**:
- `make push` from `workspace-image/` to `registry.fly.io/rommel-workspaces`.
- Backend's `fly_orchestrator` creates real machines on workspace creation.

**CI**:
- Enable the `vars.RUN_E2E == 'true'` gated job in `.github/workflows/frontend.yml` (needs Supabase test user + secrets).

**Verification**:
- Sign in on `https://rommel.vercel.app`
- Create workspace → real Fly machine spins up
- File tree, editor (Cmd+S), terminal (type `ls`, `exit`) all work over real `wss://`
- Funnel board reads/writes real `rommel/` on the machine

---

## 1. Filesystem Completion (Closes v1 File-Tree Story)

### 1.1 `fs.watch` (Highest Leverage)
**Why first**: Editor / on-disk drift is the most painful gap once agents start editing files. Phase 6 §9.3 explicitly flagged this.

**Shape**:
- New streaming primitive: `fs.watch(path, recursive?)` → stream of `FsWatchEvent` (`created` | `modified` | `deleted` | `moved`).
- Uses the Phase 7 `Publisher` seam + `writePump`.
- Daemon side: `fsnotify` (or platform-specific watcher) on the sandbox root, filtered to the requested subtree.
- FE: `useFsWatch(path)` hook + subscription in `EditorPane` (or a global workspace watcher that invalidates TanStack Query keys).

**Five-seam addition**:
1. `proto/schemas/fs/watch-event.json` (already exists as stub) + `watch.json` request schema
2. `make proto`
3. `sandbox-daemon/internal/fs/handler.go` — `Watch()` method that registers a watcher, emits via `hc.Publisher.Publish("fs.watch-event", ...)`
4. Dispatch entry in `cmd/daemon/main.go`
5. `frontend/src/lib/fs.ts` + `frontend/src/hooks/useFsWatch.ts`

**Tests**: 6–8 new cases in `server_test.go` + `fs-rpc.test.ts` using the `drainUntil` / `FakeWebSocket.serverPush` harness.

**Risks**: Resource exhaustion (too many watchers), cross-platform `fsnotify` quirks, permission model for recursive watches.

---

### 1.2 `fs.mkdir` / `fs.move` / `fs.delete`
**Status**: ✅ Completed in Phase 4 (see [`docs/completions/phase-4-fs-write-primitives.md`](./completions/phase-4-fs-write-primitives.md)).

The v1 file contract is closed. FileTree now supports creation, rename, and delete via the new primitives (with TanStack Query invalidation). `fs.move` is the public generic version of the rename the funnel handler performs internally.

**Why** (original): FileTree is currently read-only for creation/deletion. `funnel.promote` uses `os.Rename` internally; exposing it as a primitive lets the FE do atomic moves (e.g., "New File" → create empty, then rename).

**Shape** (per `primitives.md`):
- `fs.mkdir(path, recursive?)`
- `fs.move(from, to)` — atomic rename; used heavily by funnel
- `fs.delete(path, recursive?)`

**Five-seam**: Same pattern. Error codes: `fs.exists`, `fs.not_empty` (for non-recursive delete of dir), `fs.permission`.

**Note**: `fs.move` is the same operation the funnel handler already does internally — this just exposes it generically.

---

## 2. Git Primitives (Structured, Not Raw Shell)

**Status**: ✅ **Completed in Phase 2** (see [`docs/completions/phase-2-git-primitives.md`](./completions/phase-2-git-primitives.md)).

Implemented: `git.status`, `git.diff`, `git.branch.list/create/switch`, `git.commit`, plus enhancement of `workspace.info` to populate repo metadata.

**Why** (original): "Just use the terminal for git" works for humans but is terrible for UI and agents. The FE wants to render a git status pill, a diff view, a branch switcher — without parsing `git status --porcelain` in JS.

**Candidate primitives** (from `primitives.md`):
- `git.status()` → `{ branch, ahead, behind, modified: [...], untracked: [...] }`
- `git.diff(path?)` → unified or side-by-side diff
- `git.commit(message, files?)` → `oid`
- `git.branch.list()` / `git.branch.create(name)` / `git.branch.switch(name)`
- `git.push()` / `git.pull()` (with conflict surface later)

**Implementation approach**:
- Daemon shells out to `git` (already in the workspace image).
- Parse output with a small Go library (`go-git` is heavy; `git` CLI + porcelain parsing is fine for v1).
- Return structured JSON — the wire is still JSON envelopes.

**Leverage**: Once `git.status` exists, the StatusBar can show a real branch + dirty indicator without any PTY.

---

## 3. Multi-PTY + Agent Dispatch (Vision Layer 3)

**Status**: ✅ Completed in Phase 3 (see [`docs/completions/phase-3-multi-pty-agent-dispatch.md`](./completions/phase-3-multi-pty-agent-dispatch.md)).

### 3.1 Multi-PTY Tabs (UI-Only)
**State**: ✅ Done. `TerminalTabs` component (up to 4 concurrent live PTYs, "+" / close / switch). Replaces the single-pane terminal in the IDE view. Daemon soft-cap already existed since Phase 7.

**Work**: Pure UI + hook changes.
- `TerminalPane` becomes a tabbed container or split view.
- `usePty` instances keyed by `pty_id`.
- "New Terminal" button calls `ptyOpen` again.
- "Run in Terminal" context menu on files uses an existing or new PTY.

**No wire changes needed.**

---

### 3.2 `pty.start_agent(claude|codex|...)`
**Status**: ✅ Done (Phase 3). New schema + daemon handler + TS wrapper. Any `pty.*` operation works on the returned id; appears as a normal tab.

**Shape** (small additive primitive):
```json
{ "agent": "claude" | "codex" | "cursor", "args": ["--dangerously-skip-permissions"], "cwd": "optional" }
→ { "pty_id": "..." }
```

**Daemon behavior**:
- Calls `pty.open` internally.
- Immediately `exec` the agent CLI in that PTY (instead of `$SHELL`).
- Returns the `pty_id` so the FE can attach an xterm (or a dedicated agent log view).

**Why this primitive**: Vision §Layer 3 — "drop Claude Code / Codex into the workspace terminal". The funnel + this hook is the minimal agentic workflow.

**Later**: `pty.agent_status(pty_id)` for token usage, tool calls, etc. (agent CLIs would need to expose structured output).

---

## 4. Polish & Hardening (Non-Functional)

| Item | Why | Size |
|------|-----|------|
| **Session refresh (POST /sessions/:id/refresh)** | Risk 4.6 deferred. Long-lived WS currently re-calls `POST /sessions` on expiry. Proper refresh shortens the "reconnect flash". | Medium |
| **Token replay protection (jti)** | `jti` claim exists in schema but daemon doesn't check a revocation list yet. | Small |
| **Error telemetry** | Daemon and FE log error envelopes but don't ship them anywhere. Add a lightweight sink (stdout + Fly log tail, or a `/telemetry` endpoint). | Medium |
| **Rate limiting on daemon** | Soft cap on PTYs exists; add per-conn request rate limit + burst bucket to defend against runaway agents. | Medium |
| **Workspace snapshot / resume** | Fly Machines support snapshots. Enables "branch this workspace" and fast cold-start from a golden image. | Large (post v1) |
| **Image signing + SBOM** | Supply-chain hygiene before OSS release. | Medium |

---

## 5. Later / Out of v1 Scope

- **Hermes orchestrator** (Vision Layer 4) — high-level planner that watches the funnel and dispatches subagents.
- **Collaborative editing** (multi-cursor, CRDTs) — protocol does not forbid it, but no design yet.
- **Probuf wire format** — switch from JSON Schema if profiling shows we need it (schemas port field-for-field).
- **WebTransport** instead of WebSocket — lower latency for high-frequency events; browser support still maturing.
- **E2B-style snapshot/branch** for agent experimentation (self-hosted fork of E2B mentioned in techstack.md).
- **Policy engine** — the `policy/` directory and `policy.current` / `policy.update` primitives are stubs. Real rule evaluation (e.g., "agent may not `rm -rf /`") lives here.

---

## Prioritization Heuristic

1. **Unblock production** (0.1, 0.2) — nothing else matters until real users can hit the deployed ADE.
2. **Close the file contract** (`fs.watch`, `fs.mkdir/move/delete`) — the IDE feels broken without a complete filesystem model.
3. **Make git visible** (`git.status` first) — high UX leverage, low daemon complexity.
4. **Agent entry point** (`pty.start_agent`) — the first real Vision Layer 3 feature; everything before it is substrate.
5. **Polish** — only after the above are demonstrably working in prod.

Each item above is small enough to be a single focused PR (or at most two) once the production cutover is done. The five-seam pattern + proven streaming substrate means we can ship one primitive per week without architectural thrash.

---

**When to promote this doc**: Once any of the above items is actively being worked, move it (or a specialized sub-plan) into `rommel/plans/`, then `next-up/`, then `executing/`. This file becomes the source of truth for the backlog until the dogfood funnel takes over.