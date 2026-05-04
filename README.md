# Rommel

An Agent Development Environment — a minimalist, browser-based IDE designed from the ground up to host both human architects and coding agents in the same cloud-sandboxed workspace.

See [`docs/vision.md`](docs/vision.md) for the product vision, [`docs/techstack.md`](docs/techstack.md) for the locked-in technical choices, and [`docs/executing/scaffolding-plan.md`](docs/executing/scaffolding-plan.md) for the current build plan.

## Quick start

```sh
make bootstrap   # install all toolchain deps (pnpm, poetry, go modules)
make dev         # spin up frontend + backend + local daemon
make lint        # run all linters
make test        # run all tests
make build       # build everything (no deploy)
```

## Repo layout

| Path                | What it is                                           |
|---------------------|------------------------------------------------------|
| `frontend/`         | Next.js + Monaco IDE shell, deployed to Vercel       |
| `backend/`          | FastAPI control plane, deployed to Fly.io            |
| `sandbox-daemon/`   | Go binary baked into the workspace VM image         |
| `workspace-image/`  | Dockerfile for the per-workspace Fly Machine VM     |
| `proto/`            | JSON Schema source-of-truth + codegen for TS/Go/Py  |
| `docs/`             | Vision, tech stack, primitives, plans               |
| `.rommel/`          | This project's own dogfooded planning funnel        |
| `infra/`            | IaC placeholder (empty by design until needed)      |

Each top-level directory deploys independently. The Makefile is a router that delegates into per-subtree scripts.
