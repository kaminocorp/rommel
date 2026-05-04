# Primitives

The fundamental verbs of the Rommel ADE — the operations that, taken together, define what the system can do. Listed for discussion, not yet locked in.

Three surfaces, three sets of primitives:

1. **Daemon primitives** — what runs *inside* a workspace, exposed to the browser over WebSocket. The hot path. This is the protocol that defines what the ADE *is*.
2. **Backend primitives** — control-plane HTTP API. What the frontend asks the backend about workspaces, sessions, policy.
3. **Frontend concepts** — UI-level building blocks (not protocol, but worth enumerating).

---

## 1. Daemon primitives (browser ↔ daemon WebSocket)

This is the most important surface — it defines what an "ADE session" actually does. Grouped by domain.

### Filesystem

- `fs.read(path)` → file contents + metadata
- `fs.write(path, contents)` → ack
- `fs.list(path)` → directory entries (name, type, size, mtime)
- `fs.stat(path)` → file metadata only
- `fs.mkdir(path, recursive)` → ack
- `fs.move(from, to)` → ack (used heavily for `.rommel/` stage promotion)
- `fs.delete(path, recursive)` → ack
- `fs.watch(path, recursive)` → stream of change events (created, modified, deleted, moved)
- `fs.search(query, path, glob)` → stream of matches (grep-like)

**Open question**: do we expose file *patches* (diff-based writes) as a primitive, or always send full file contents? Patches matter for collaborative editing and for agents that produce diffs. Probably yes, eventually: `fs.patch(path, diff)`.

### Terminal (PTY)

- `pty.open(cols, rows, cwd, env)` → `pty_id`
- `pty.input(pty_id, data)` → ack (user keystrokes / agent input)
- `pty.output(pty_id, data)` → stream (PTY output to render in xterm)
- `pty.resize(pty_id, cols, rows)` → ack
- `pty.close(pty_id)` → ack

**Open question**: do we want a separate non-PTY exec primitive (`process.exec(cmd, args)` with structured stdout/stderr/exit)? Useful for agents that don't need a terminal — they just want command output. Probably yes.

### Git

Two philosophies:

- **A) Just shell out:** git is already accessible via `pty.open` running `git`. No special primitive needed.
- **B) Structured git verbs:** `git.status()`, `git.diff()`, `git.commit(msg)`, `git.branch.create/list/switch`, `git.push()`, `git.pull()`. Returns parsed structured data the UI can render natively.

**Recommendation**: B. The frontend wants to render git status as a UI element, not parse `git status` output. Structured primitives make that clean. The daemon's implementation can shell out internally.

### Workspace lifecycle (daemon-internal)

- `workspace.info()` → workspace metadata (id, repo, branch, recent activity)
- `workspace.health()` → resource usage, daemon version
- `workspace.shutdown()` → graceful shutdown (rare; usually backend-initiated)

### `.rommel/` planning funnel

These could be implemented as conventions over `fs.move`, but giving them first-class verbs makes the protocol self-documenting and lets the daemon enforce funnel rules (e.g., "can't promote to Executing without a plan"):

- `funnel.list(stage)` → md files in a given stage (Triage, Plans, NextUp, Executing, Completions, Archive)
- `funnel.promote(file, from_stage, to_stage)` → ack (atomic move + validation)
- `funnel.current_executing()` → what an agent is working on right now

**Open question**: should funnel verbs live in the daemon (close to the filesystem) or in the backend (where policy lives)? Leaning daemon, with backend able to push policy that constrains valid transitions.

### Policy / rules

- `policy.current()` → the active rule set for this workspace (loaded from backend at boot)
- `policy.update(bundle)` → backend pushes a new rule set; daemon hot-reloads (control-channel only)

---

## 2. Backend primitives (frontend ↔ backend HTTP)

CRUD + orchestration. RESTful for boring stuff, RPC-flavored for actions.

### Auth

- `POST /auth/oauth/github/callback` → exchange OAuth code for session
- `GET /auth/me` → current user
- `POST /auth/logout`

### Workspaces

- `GET /workspaces` → list user's workspaces
- `POST /workspaces` → create new workspace (clone repo, provision Fly Machine)
- `GET /workspaces/:id` → workspace metadata
- `POST /workspaces/:id/start` → wake a stopped workspace
- `POST /workspaces/:id/stop` → put workspace to sleep (Fly auto-stop)
- `DELETE /workspaces/:id` → destroy workspace + volume

### Sessions (the broker — Pattern B's heart)

- `POST /workspaces/:id/sessions` → returns `{ daemon_url, token, expires_at }`. Browser uses this to connect directly to the daemon.
- `POST /sessions/:id/refresh` → extend before expiry

### Repos

- `GET /repos` → list importable repos from connected GitHub account
- `POST /repos/import` → kick off an import (clone into a new workspace)

### Policy / rules (admin)

- `GET /policy` → current global + per-workspace policy
- `PUT /policy/workspaces/:id` → update workspace-specific rules
- Backend evaluates policy on its own actions (e.g., "can this user create another workspace under their quota?") and ships policy bundles to daemons on spawn.

### Agent dispatch (later — Layer 3 / Hermes)

- `POST /workspaces/:id/agents` → spawn an agent task in the workspace (initially: open Claude Code in the terminal)
- `GET /workspaces/:id/agents` → currently running agents
- `DELETE /agents/:id` → stop an agent

---

## 3. Frontend concepts (UI primitives)

Not protocol — just the conceptual building blocks of the IDE shell.

- **Workspace shell** — top-level chrome (header, status bar, command palette).
- **File tree** — virtualized tree, drives `fs.list` / `fs.watch`.
- **Editor pane** — Monaco instance per open file, drives `fs.read` / `fs.write`.
- **Terminal pane** — xterm.js instance per `pty_id`.
- **Funnel board** — kanban view over `.rommel/`, drives `funnel.*` primitives. Renders md files as cards; drag-to-promote becomes `funnel.promote`.
- **Plan viewer** — md preview + edit for individual plans.
- **Git panel** — structured view over `git.*` primitives.
- **Agent panel** — view of running agents, their current task, recent output.
- **Command palette** — keyboard-first dispatcher, fires any of the above.

---

## Cross-cutting questions for discussion

1. **Protocol format**: Protobuf (strict, fast, harder to debug) vs JSON-over-WS (flexible, debuggable, slightly looser). Leaning JSON for v1, switch to Protobuf if profile shows we need it.
2. **Streaming primitives**: which verbs need to be streams vs request-response? `fs.watch`, `pty.output`, `fs.search`, `policy.update` are clearly streams. Everything else is RPC.
3. **Idempotency**: do we need request IDs / dedup for write operations? Probably yes — network blips on `fs.write` shouldn't double-write.
4. **Multi-cursor / multi-client**: can two clients be connected to one daemon at once (e.g., two browser tabs)? Should they see each other's edits? Out of scope for v1, but the protocol should not foreclose it.
5. **Capability scoping**: should session tokens carry scopes (e.g., "this token can only read, not write")? Useful for sharing read-only sessions, future agent identity model. Probably yes; cheap to design in now.
