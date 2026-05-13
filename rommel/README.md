# `rommel/` — the planning funnel (Layer 2)

This directory **is** the planning funnel described in [`docs/vision.md`](../docs/vision.md) §Layer 2 — a kanban-shaped markdown filesystem that doubles as a deterministic state machine for agents. The Rommel ADE reads this folder via the daemon's `funnel.*` primitives and renders it as a board in the browser.

Six stages, in flow order:

| Stage          | What lives here                                                                 |
|----------------|---------------------------------------------------------------------------------|
| `triage/`      | Raw ideas. Free-form markdown dumps. No structure required.                     |
| `plans/`       | Promoted ideas, fleshed out into concrete plans.                                |
| `next-up/`     | The agent backlog. Plans queued for execution.                                  |
| `executing/`   | What is being worked on right now. One file at a time, ideally.                 |
| `completions/` | Finished work, one md file per completed phase.                                 |
| `archive/`     | Shipped and pushed to production — or killed early.                             |

Promotion is a deliberate act — `funnel.promote(name, from, to)` validates the transition (forward-only along the chain, plus archive-from-anywhere as a kill switch) before moving the file.

This repo dogfoods its own funnel: every Phase plan lives under `rommel/executing/` while the work is happening, and gets duplicated to `rommel/archive/` on completion (mirrored in `docs/archive/` for GitHub-friendly browsing).
