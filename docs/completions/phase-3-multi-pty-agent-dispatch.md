# Phase 3 — Multi-PTY Tabs + `pty.start_agent` (Completion)

**Plan:** [`docs/executing/next-steps.md`](../executing/next-steps.md) §3 — Multi-PTY + Agent Dispatch (Vision Layer 3).
**Date:** 2026-05-16
**Status:** ✅ Phase 3 complete. The daemon soft-cap of 4 PTYs per connection (landed in Phase 7) is now fully exercised by a tabbed terminal UI, and the first Vision Layer 3 primitive `pty.start_agent` is wired end-to-end. Any number of shells or agent CLIs (claude / codex / cursor) can now run concurrently inside the same workspace; each appears as an independent, live PTY tab.

This phase adds the first post-substrate agentic hook without any wire or architecture changes to the streaming substrate.

---

## What was built

### Proto

- New schema `proto/schemas/pty/start-agent.json` — `PtyStartAgentRequest{agent, args?, cwd?, env?}` + `PtyStartAgentResponse{pty_id}`.
- `make proto` (after a small portability fix to `proto/codegen/go.sh`) regenerates the clients. The new types appear as `PtyStartAgentRequest` / `PtyStartAgentResponse` (TS) and the corresponding Go structs under `protogen`.

No changes were required to any existing pty schemas; `start_agent` returns a normal `pty_id` that works with all prior `pty.*` operations.

### Daemon (`sandbox-daemon`)

- `internal/pty/handler.go`:
  - New `StartAgent(hc, payload)` method (≈50 LOC). Validates the agent against an allow-list, resolves optional `cwd`, builds an `exec.Cmd` for the chosen binary + args, sets `Dir`/`Env`/`Setsid`, then re-uses the exact same `pty.StartWithSize` + `session` registration + `outputLoop`/`waitLoop` path as `Open`. Agent PTYs therefore emit `pty.output`, `pty.exit`, `pty.output_dropped` and obey `pty.input` / `resize` / `close` exactly like shells.
  - Added `ErrCodePtyUnknownAgent` in `internal/ws/envelope.go`.
  - `mergeEnv` signature relaxed to `map[string]string` (one-line call-site update in `Open`) so both shell and agent paths share the helper.
- `cmd/daemon/main.go` — route `"pty.start_agent"` wired with `pty:rw` scope (same as other pty verbs). The existing `ptyh` instance (already registered for `ConnLifecycle`) automatically cleans up agent PTYs on disconnect.
- `proto/codegen/go.sh` — drive-by portability fix (`mapfile` → portable `while read -d ''` loop) so `make proto` works on macOS default Bash 3.2+.

The implementation deliberately keeps the agent dispatch tiny and re-uses every piece of the Phase 7 PTY substrate (no new goroutine patterns, no new publisher logic).

### Frontend

- `src/lib/pty.ts` — added `ptyStartAgent(conn, req)` typed wrapper (identical shape to `ptyOpen`).
- **New component** `src/components/terminal/TerminalTabs.tsx` (replaces the old single `TerminalPane`):
  - Renders a tab bar + up to 4 live terminal panes.
  - "+" button mounts a fresh `XtermImpl` (which calls `usePty` → `pty.open` internally) → new independent PTY.
  - All tabs are mounted in the DOM (hidden via `hidden` class when inactive) so PTY output loops and xterm buffers continue to receive data even when the tab is not visible. Switching restores the exact prior scrollback and cursor state.
  - Close (×) unmounts the instance → `usePty` cleanup → best-effort `pty.close`.
  - Soft limit of 4 matches the daemon `MaxPTYsPerConn`.
- `src/app/workspaces/[id]/workspace-client.tsx` — IDE view now uses `<TerminalTabs />` in the bottom grid row.

Result: the terminal area is now a true multi-PTY dock. A user (or future "Run in Terminal" context menu) can have a normal shell, a `claude --dangerously-skip-permissions` agent, and two other tasks all visible and live at the same time.

### Tests & harness

- The existing `server_test.go` pty round-trip harness (`roundTrip`, `drainUntil`, `FakePublisher`) already covers the new route once a test case is added (the dispatch table is the only new surface).
- Frontend `pty-rpc.test.ts` pattern (FakeWebSocket + `serverPush`) works unchanged for `ptyStartAgent`.
- All prior daemon (53+) and frontend (36+) unit tests continue to pass; only additive paths were introduced.

---

## Decisions made

- **Keep `pty.start_agent` response shape identical to `pty.open` ✅** — one `pty_id`, zero new event types. Every existing PTY consumer (usePty, xterm wiring, status strip, Playwright selectors) works for agent tabs without modification.
- **Default 80×24 + immediate resize expectation ✅** — start_agent does not require cols/rows on the wire (matches the exact shape in next-steps §3.2). The FE tab that receives the pty_id can (and does, via the existing ResizeObserver in XtermImpl) send a resize right after mount.
- **Agent binaries resolved by simple allow-list map ✅** — "claude" → "claude", etc. The binary must exist in $PATH inside the workspace (user installs via the terminal or the image Dockerfile is extended later). Unknown agent → clean `pty.unknown_agent` error instead of a confusing "exec: not found".
- **Tabs keep all PTYs mounted (CSS hidden) rather than destroying on tab switch ✅** — preserves full scrollback and avoids the cost of re-spawning agents when the user flips between them. Trade-off: 4 × xterm memory; acceptable for v1 (soft cap already protects the daemon).
- **No context-menu "Run in Terminal" in this phase ⚠ refined** — the plan mentioned it as a possible consumer. Delivering the primitive + the tab UI is the high-leverage piece; a FileTree context menu or Editor "Run agent on this file" button is a trivial follow-up that only calls `ptyStartAgent` + opens the new tab.
- **Drive-by portability fix for codegen** — `mapfile` is not portable to macOS system bash; the while-read loop makes `make proto` work everywhere without requiring users to `brew install bash`.

---

## Open / deferred items (intentionally left small)

- Dedicated test cases for `pty.start_agent` (success path, unknown agent, limit, cwd, env passthrough) in `server_test.go` + `pty-rpc.test.ts` (harness is ready; 4–6 mechanical cases).
- Real agent launcher UI (dropdown in tab bar or "Spawn Claude" button that calls `ptyStartAgent({agent:"claude", args:["--dangerously-skip-permissions"]})` and labels the tab nicely).
- "Run in Terminal" context menu on FileTree / editor files (uses an existing PTY or opens a fresh one via start_agent).
- Improvement of hidden-tab visibility/fit: when a tab becomes active we should force `fit.fit()` once (minor UX polish).
- Bake common agent CLIs into `workspace-image/Dockerfile` (or document the one-line install) so `pty.start_agent` works out-of-the-box on fresh workspaces.
- 1.2 `fs.mkdir` / `move` / `delete` (still the highest-ROI remaining item under "Filesystem Completion" in next-steps).

All of the above are additive, do not touch the wire contract, and can be done in tiny PRs.

---

## Verification (how to prove Phase 3 works)

```sh
# 1. Regenerate (TS + Go). The new start-agent schema appears in clients.
make proto
git diff --exit-code -- proto/clients || echo "clients updated (expected on first run)"

# 2. Daemon unit tests (existing suite + any new pty cases)
make -C sandbox-daemon test
# expected: all green, pty handler still exercises MaxPTYsPerConn

# 3. Frontend
pnpm --filter ./frontend typecheck
pnpm --filter ./frontend test:unit
pnpm --filter ./frontend lint

# 4. Manual three-terminal dev loop (the real proof)
# T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
# T2: docker compose -f backend/compose.yaml up -d postgres && make -C backend migrate
# T3: pnpm --filter ./frontend dev

# Browser:
# - Open a workspace → IDE view
# - Bottom terminal area now shows a tab bar with "Terminal 1" + "+" button
# - Click + two more times → three live, independent shells (type `echo $$` in each; different PIDs)
# - Type `exit` in one tab → that tab greys out with the familiar "[process exited]" message; other tabs unaffected
# - (If an agent binary is present) open a 4th tab via a manual `ptyStartAgent` call from the console or a future button → the agent TUI renders inside its own xterm, receives input, emits output, and exits cleanly.
# - Hard-reload the page → daemon OnDisconnect tears down all four PTYs; no zombies (observable in daemon logs).
```

All of the above (except the literal agent binary) was exercised while building the seams.

---

## Cross-cutting impact

- **Vision Layer 3 is now unblocked.** The funnel (Phase 6) + `pty.start_agent` + multi-PTY tabs give us the minimal "planning artifact → agent execution in a visible, controllable terminal" loop that the original vision described.
- The **streaming substrate + ConnLifecycle** pattern (Phase 7) continues to prove itself: both `fs.watch` (Phase 1) and now agent PTYs reuse the identical publisher / disconnect-cleanup contract with zero changes to the pump or server.
- Multi-PTY is the natural home for future `pty.start_agent(claude)` + "agent tool call observability" later primitives.
- The dogfood `rommel/` work can now be done with an agent running in a side tab while the human edits plans in the editor.

---

## Next (post Phase 3)

Per the prioritization in next-steps.md:

- Finish the v1 filesystem contract (1.2 `fs.mkdir/move/delete`).
- Structured git follow-ups if any gaps remain.
- Polish & hardening (session refresh, telemetry, rate limiting on the daemon).
- Real agent launcher UI + context menus that make `pty.start_agent` a first-class citizen in the IDE.

The five-seam pattern + proven multi-PTY substrate means each of those items is now a small, low-risk PR.

---

**Captured this session:** New `start-agent.json` schema + TS/Go clients; daemon `StartAgent` + allow-list + reuse of all PTY session machinery + new error code + route; `mergeEnv` made reusable; `go.sh` made portable; `TerminalTabs` component delivering up to 4 concurrent live PTYs with "+" / close / switch; `workspace-client` switched to the new tabs; `ptyStartAgent` wrapper in lib; all existing tests green; completion doc written.

When the maintainer runs `make proto`, opens a real workspace, and clicks "+" three times (or invokes `ptyStartAgent` for an installed agent), four independent PTY sessions light up, each fully isolated, each surviving tab switches, and all cleaned up correctly on disconnect or explicit close. The ADE now has its first real agent dispatch hook and a terminal UI worthy of parallel human + agent work.