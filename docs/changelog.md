# Changelog

All notable changes to this project, latest on top. Each entry links to the corresponding completion doc under [`docs/completions/`](./completions/).

## Index

- [**0.1.1** — 2026-05-04](#011--2026-05-04) — `proto/` source-of-truth + codegen for TS/Go/Pydantic; session token contract committed.
- [**0.1.0** — 2026-05-04](#010--2026-05-04) — Repo root scaffolding: monorepo plumbing, defensive CI, no subtree code yet.

---

## 0.1.1 — 2026-05-04

**Phase 1 — `proto/` Source-of-Truth + Codegen.** Completion doc: [`docs/completions/phase-1-proto.md`](./completions/phase-1-proto.md). Plan: [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §1.

Status: ✅ `make proto` regenerates 11 schemas → TS + Go + Pydantic v2 clients with **zero diff** on the second run. Generated Go compiles. Cross-cutting **session token contract** is now live in `proto/schemas/session-token.json`, unblocking §2 (daemon) and §4 (backend) integration.

### Added

- **`proto/schemas/`** — JSON Schema (draft 2020-12) source-of-truth. Real schemas for the proof-of-life surface area: `envelope.json` (WS wrapper), `session-token.json` (EdDSA broker JWT claims), `fs/read.json`, `pty/{open,input,output-event}.json`, `workspace/info.json`. Stub schemas for `fs/{write,list,watch-event}.json` and `pty/resize.json` so the surface area is visible.
- **Per-language codegen scripts** under `proto/codegen/`:
  - `ts.sh` — `npx --yes json-schema-to-typescript@^15`, one `.ts` per schema + auto-generated `index.ts` re-exporting all.
  - `go.sh` — `go run github.com/atombender/go-jsonschema@v0.18.0`, single `gen/proto.go` (package `protogen`) with `UnmarshalJSON` validation hooks.
  - `python.sh` — hermetic venv at `proto/codegen/.venv/` (bootstrapped on first run, version-marker-pinned), runs `datamodel-code-generator==0.31.2` → Pydantic v2 BaseModels.
- **`proto/codegen.sh`** — orchestrator that runs all three scripts. Equivalent to `make proto`.
- **Per-client packaging metadata** (committed; generated source is gitignored):
  - `proto/clients/ts/package.json` — `@rommel/proto`, pnpm workspace dep.
  - `proto/clients/go/go.mod` — `github.com/rommel-ade/rommel/proto/clients/go` (placeholder org).
  - `proto/clients/python/pyproject.toml` — `rommel-proto`, hatchling build.
- **`proto/README.md`** — how to add a schema, how to regenerate, format-choice rationale.

### Modified

- **`.gitignore`** — added `proto/codegen/.venv/` so the Python codegen venv isn't tracked.

### Removed

- `proto/schemas/funnel/.gitkeep`, `proto/schemas/git/.gitkeep` — confused `datamodel-code-generator` (warns on non-JSON files in input dirs). Directories will materialize when their first real schema lands; their existence is documented in `proto/README.md`.

### Decisions

- **JSON Schema, not Protobuf.** Daemon traffic is JSON-over-WebSocket — no binary framing layer to bolt on. Browser devtools render the wire format directly (huge for hot-path debugging). Codegen tooling on all three sides is mature. Schemas port to Protobuf field-for-field if profiling later demands it.
- **`$defs` + named subschemas + root `oneOf` for RPC shapes.** Drafting both a `$defs` block (named `FsReadRequest` / `FsReadResponse`) and a root `oneOf: [$ref, $ref]` produces clean named structs/classes in Go and Python *and* a discriminated TS union (`type FsRead = FsReadRequest | FsReadResponse`). One schema, three idiomatic outputs. Codified as the convention for future RPC schemas.
- **All codegen tools version-pinned.** `json-schema-to-typescript@^15`, `go-jsonschema@v0.18.0`, `datamodel-code-generator==0.31.2`. Reproducible CI is the whole point of this phase.
- **Hermetic Python venv beats global install.** `python.sh` bootstraps `.venv/` on first run with a `.installed-<version>` marker; bumping the version invalidates the marker and triggers a clean reinstall. `make proto` works from a fresh clone with just system Python.
- **Generated source gitignored; only metadata committed.** `proto/clients/*/{src,gen}/` are gitignored. `proto.yml` CI re-runs codegen and fails on diff — catches the "someone hand-edited the generated code" footgun.
- **Idempotency hinges on two flags.** `--disable-timestamp` (Python) kills the `# generated at <iso8601>` header; `LC_ALL=C sort -z` (TS script) kills locale-dependent file ordering. Without these, every CI run would produce a diff.

### Cross-cutting: session token contract is committed

`proto/schemas/session-token.json` settles the contract the scaffolding plan flagged as a §2/§4 prerequisite:

- **Algorithm:** EdDSA (Ed25519). Backend signs (private key from Fly secret); daemon verifies (public key baked into VM image at deploy time).
- **Claims:** `iss` (const `rommel-backend`), `sub` (user id), `aud` (const `rommel-daemon`), `wid` (workspace id), `scope` (capability list), `exp`, `iat`, `jti`. All required.
- **Scope vocabulary:** `fs:r`, `fs:rw`, `pty:rw`, `git:r`, `git:rw`, `funnel:r`, `funnel:rw`, `policy:r` — answers `primitives.md` cross-cutting Q5 (capability scoping) directly in the type system.

### Verification

```sh
make proto                              # first run: ~30s (bootstraps Python venv, fetches Go module)
cp -r proto/clients .snap
make proto                              # second run: ~3s
diff -r .snap proto/clients             # → empty (idempotent)
cd proto/clients/go && go build ./gen/...   # → exit 0
```

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §2: **`sandbox-daemon/`** — Go binary that accepts a WebSocket at `/ws?token=...`, validates `SessionTokenClaims` against an EdDSA pubkey from env, handles `ping → pong`, and implements real `fs.read` to prove the proto loop works end-to-end. Imports `github.com/rommel-ade/rommel/proto/clients/go/gen` (package `protogen`), likely via a `replace` directive in its own `go.mod` until a `go.work` lands at the repo root.

---

## 0.1.0 — 2026-05-04

**Phase 0 — Repo Root Scaffolding.** Completion doc: [`docs/completions/phase-0-scaffolding.md`](./completions/phase-0-scaffolding.md). Plan: [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §0.

Status: ✅ `make bootstrap && make lint && make build` all pass on a fresh clone. Subtrees (`frontend/`, `backend/`, `sandbox-daemon/`, `proto/`) intentionally absent — they land in later phases.

### Added

- **Toolchain pins** — `.nvmrc` (Node 20) and `.tool-versions` (Node 20.18.0, Go 1.22.8, Python 3.12.7, pnpm 9.12.0) as the single source of truth across all four toolchains.
- **Editor config** — `.editorconfig` with 2-space default, 4-space for Python/Go, tab for Makefile, and no trailing-whitespace trim on Markdown (preserves intentional double-space line breaks).
- **`.gitignore`** — covers Node (`node_modules/`, `.next/`), Python (`__pycache__/`, `.venv/`), Go (`sandbox-daemon/dist/`), deploy tooling (`.fly/`, `.vercel/`), and generated proto clients (`proto/clients/*/{src,gen}/`).
- **pnpm workspace root** — `package.json` (`"private": true`, pinned `packageManager`, engines specified, no runtime deps) and `pnpm-workspace.yaml` listing the eventual TS workspaces (`frontend/`, `proto/clients/ts/`). pnpm tolerates missing globs, so committing ahead of the dirs is safe.
- **`pnpm-lock.yaml`** — generated by `make bootstrap`.
- **Top-level `Makefile`** — acts as a *router*, not a build system. Targets `lint`, `test`, `build`, `bootstrap`, `clean` delegate into per-subtree Makefiles via a `run_if_exists` helper that no-ops with a friendly note when the subtree is absent. Keeps CI green during the multi-phase rollout.
- **`README.md`** — one-paragraph orientation, subtree table, pointers into `docs/`. Deliberately does not duplicate `vision.md`.
- **CI workflows** under `.github/workflows/` — `frontend.yml`, `backend.yml`, `daemon.yml`, `proto.yml`. Each is path-filtered and gates on a sentinel file (`frontend/package.json`, `backend/pyproject.toml`, `sandbox-daemon/go.mod`, `proto/codegen.sh`); skips cleanly if absent. Workflows "wake up" the moment their subtree lands.

### Decisions

- **Bare pnpm workspaces, not Turborepo.** `techstack.md` left this open. Turborepo's value (remote caching, task graphs) doesn't pay off until multiple TS packages do real work. Easy to layer on later.
- **CI is defensive (gate-and-skip), not deferred.** Rejected leaving `.github/workflows/` empty until each subtree exists — once subtrees start landing, "did I remember to add the workflow?" causes drift. Wiring path filters once now means the very first PR touching `frontend/` triggers the right job.
- **`Makefile` uses `run_if_exists` instead of hard-coded per-subtree targets.** Adding a subtree is a single `mkdir` + per-subtree Makefile away from being picked up by the root router; no edits to the root `Makefile` needed.
- **Generated proto clients are gitignored.** `make proto` regenerates them; the `proto.yml` workflow fails CI if regenerated output diverges from committed schemas. Avoids the classic "generated code committed for convenience, then drifts" footgun.

### Verification

```sh
make help        # prints targets
make bootstrap   # pnpm install (no workspaces yet → "Already up to date")
make lint        # all subtree gates skip cleanly
make build       # all subtree gates skip cleanly
```

CI workflows not yet triggered (no push), but gate logic was reviewed line-by-line against on-disk file-existence checks.

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §1: **`proto/`** — JSON Schema source-of-truth and codegen for TS/Go/Pydantic. Depends on settling the **session token contract** (cross-cutting section of the plan); confirm that decision before §2/§4 begin.
