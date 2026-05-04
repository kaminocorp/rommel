# Vision

A sophisticated **Agent Development Environment (ADE)** — an agent-native IDE that runs in the browser and replaces VSCode for my day-to-day work.

## What it is

A minimalist, cloud-based IDE designed from the ground up to host both human architects and coding agents in the same workspace. Stripped down to the essentials, with a planning layer baked into the filesystem so ideas flow deterministically from brainstorm to shipped code.

**Workspace = Repo.** Each workspace is a real Linux sandbox in the cloud with a persistent filesystem, the repo cloned in, and env files in place.

## Layer 1 — The barebones browser IDE

The foundational shell. Everything else sits on top.

- **Repo import** and git support (pull, push, commit, branch).
- **File/folder navigator.**
- **In-browser editor** — full read/write. I can open any file and edit it directly (Monaco or CodeMirror under the hood). No LSP/intellisense in v1; just fast, syntax-highlighted editing.
- **Terminal access** for arbitrary commands — `git`, `fly deploy`, package managers, etc. CLI-first is fine; UI buttons can wrap common actions later.
- **Env files** live in the workspace like any other file.

## Layer 2 — The planning funnel (`.rommel/`)

A dedicated root-level folder (`.rommel/` or `rommel/`) that holds all planning artifacts as markdown. Optionally committed to the repo on push — my choice per workspace.

The browser surfaces this folder as a structured planning UI, not just a file tree:

- **Triage / Canvas** — a free-form space where I dump raw ideas as md files. Files can fly around; no structure required.
- **Plans** — promoted ideas, fleshed out into concrete plans.
- **Next Up** — the agent backlog. Plans here are queued for execution.
- **Executing** — what an agent is currently working on.
- **Completions** — one md file per completed phase, plus a final rollup per plan.
- **Archive** — completed work that has been git-pushed and shipped to production.

Promotion between stages is a deliberate act (mine or an agent's). The folders are simultaneously a **kanban board for me** and a **deterministic state machine for agents**.

## Layer 3 — Native agentic workflows

Lightweight, opinionated automations stitched into the funnel transitions to keep agent work disciplined — structure enforced by the system, not just by prompts. Exact shape TBD; the funnel itself is the primary discipline mechanism in v1.

## Layer 4 — Hermes (later)

A cognitive orchestrator that sits on top of the ADE as the eventual human-replacement layer: high-level planning, task decomposition, dispatching subagents, watching the funnel. Out of scope for v1 — the ADE has to be a reliable substrate first.

## Build order

1. Browser IDE shell (file tree, editor, git, terminal) on top of a per-workspace cloud sandbox.
2. The `.rommel/` planning UI and funnel.
3. Coding-agent integration (drop Claude Code / Codex into the workspace terminal).
4. Hermes orchestrator on top.
