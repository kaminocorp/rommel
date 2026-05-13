# Changelog

All notable changes to this project, latest on top. Each entry links to the corresponding completion doc under [`docs/completions/`](./completions/).

## Index

- [**0.1.3** — 2026-05-13](#013--2026-05-13) — `workspace-image/`: Fly Machine VM image — baked daemon binary, EdDSA pubkey, `git`/`curl`/`tini`; canonical Dockerfile.
- [**0.1.2** — 2026-05-12](#012--2026-05-12) — `sandbox-daemon/`: Go WS server with EdDSA token validation, `system.ping`, and real `fs.read`.
- [**0.1.1** — 2026-05-04](#011--2026-05-04) — `proto/` source-of-truth + codegen for TS/Go/Pydantic; session token contract committed.
- [**0.1.0** — 2026-05-04](#010--2026-05-04) — Repo root scaffolding: monorepo plumbing, defensive CI, no subtree code yet.

---

## 0.1.3 — 2026-05-13

**Phase 3 — `workspace-image/` Fly Machine image.** Completion doc: [`docs/completions/phase-3-workspace-image.md`](./completions/phase-3-workspace-image.md). Plan: [`docs/executing/phase-3-workspace-image-plan.md`](./executing/phase-3-workspace-image-plan.md).

Status: ✅ Local image build, smoke test, and signal-handling all green. `make -C workspace-image build` produces `rommel-workspaces:<git-sha>` from repo-root context in ~25 s warm (~110 s cold); compressed registry size **66 MiB**. The image boots under `tini`, the entrypoint loads the EdDSA pubkey from `/etc/rommel/token.pubkey` into `ROMMEL_TOKEN_PUBKEY`, fails fast on missing `ROMMEL_WID`, and forwards SIGTERM to the daemon's graceful shutdown (sub-second drain). Fly-side `fly machine run` cold-start measurement is the one verification deferred — needs `fly auth login`, recipe baked into `workspace-image/README.md`.

### Added

- **`workspace-image/`** subtree:
  - `Dockerfile` — multi-stage: `golang:1.23` builder regenerates the proto Go client and compiles a static `-trimpath -ldflags="-s -w"` daemon binary; runtime stage is `debian:stable-slim` + `apt(ca-certificates curl git tini)` + daemon binary + baked `rootfs/`. Build context is the repo root.
  - `fly.toml` — `app = "rommel-workspaces"`, `internal_port = 7777`, **no `[[services]]`** (internal Flycast/`.internal` only), **no volumes** (the backend attaches one per workspace via the Machines API). `[[restart]] policy = "on-failure"`.
  - `Makefile` — `build` / `push` / `run-local` / `clean`. Same `IMAGE=… TAG=…` env override pattern as the daemon's Makefile.
  - `.gitignore` — local-only pubkey overrides (`*.pubkey.local`, `*.pem.local`).
  - `rootfs/etc/rommel/daemon.env.example` — documents every `ROMMEL_*` env the daemon reads.
  - `rootfs/etc/rommel/token.pubkey.example` — real Ed25519 PEM committed for dev builds; the matching private key was generated in `/tmp/`, used only to derive the pubkey, then deleted in the same `openssl` step, so the dev verifier is intentionally unrecoverable.
  - `scripts/build.sh` — `cd $(git rev-parse --show-toplevel)` then `docker build -f workspace-image/Dockerfile ... .` with `--build-arg ROMMEL_TOKEN_PUBKEY_FILE`. `TAG_LATEST=true` opt-in for `:latest`.
  - `scripts/push.sh` — `flyctl auth whoami` gate, `flyctl auth docker` credential install, then `docker tag` + `docker push` to `registry.fly.io/rommel-workspaces:<tag>`.
  - `scripts/entrypoint.sh` — `set -euo pipefail` bash; loads the PEM into `ROMMEL_TOKEN_PUBKEY` (the daemon parses PEM contents, not a file path); fails fast on missing `ROMMEL_WID`; `exec`'s the daemon under tini.
  - `README.md` — full build / smoke / push / cold-start recipe + gotchas (build-context, `.dockerignore` location, pubkey rotation, no public services).
- **`.dockerignore`** at the repo root — new file written for `workspace-image/`'s build context. Sweeps out `.git/`, `.github/`, `.claude/`, `.rommel/`, `docs/`, `frontend/`, `backend/`, `infra/`, all `node_modules/`, `.next/`, `.venv/`, generated proto clients, env files. Documented as the canonical ignore for any future Dockerfile built from repo root.
- **`.github/workflows/workspace-image.yml`** — path-filtered on `workspace-image/**`, `sandbox-daemon/**`, `proto/**`, `.dockerignore`, and the workflow itself. Gates on `workspace-image/Dockerfile` existing (same skip-when-absent pattern as `daemon.yml`/`frontend.yml`/`backend.yml`/`proto.yml`). PR runs `scripts/build.sh` with `TAG_LATEST=false`; `push` to `main` additionally runs `superfly/flyctl-actions/setup-flyctl` + `scripts/push.sh` with `FLY_API_TOKEN` from secrets and `TAG_LATEST=true`.

### Modified

- **Top-level `Makefile`** — added `workspace-image` to the `build` and `clean` target lists via the existing `run_if_exists` helper. `lint`/`test` deliberately untouched (the image has neither — CI builds it instead).
- **`sandbox-daemon/README.md`** — replaced the "Building the Docker image" section with a pointer to `workspace-image/`. Inner-loop dev (`make run-local` on Go source) is unchanged.

### Removed

- **`sandbox-daemon/Dockerfile`** — per Decision 0.1 of the Phase-3 plan. The workspace-image Dockerfile is now the only Dockerfile in the repo. Keeping a near-duplicate in `sandbox-daemon/` would have diverged the moment one was updated without the other; the daemon's local-dev path doesn't need Docker.

### Decisions

- **Single Dockerfile, in `workspace-image/`.** The daemon's binary is built from source inside `workspace-image/Dockerfile`'s build stage. No second Dockerfile, no cross-Dockerfile `FROM` plumbing.
- **EdDSA pubkey baked as a file via `ARG ROMMEL_TOKEN_PUBKEY_FILE`.** PEM lives at `/etc/rommel/token.pubkey`; entrypoint exports its contents into `ROMMEL_TOKEN_PUBKEY` before `exec`'ing the daemon. Rotation requires a rebuild — intentional, so tokens can never outlive the deploy that minted their verifier.
- **No `[[services]]` in `fly.toml`.** Workspaces are reachable only via `.flycast` / `.internal` DNS on port 7777. If `0.0.0.0` exposure ever shows up here, the EdDSA scope-gate becomes the *last* line of defense rather than defense-in-depth.
- **`ROMMEL_WORKSPACE_ROOT=/workspace` as Dockerfile `ENV` + `WORKDIR /workspace`.** Pairs cleanly with Fly volumes (attached over the same path per workspace by the backend) and lets bare `docker run` work without a volume mount.
- **Repo-root `.dockerignore`.** Docker only reads `<context-root>/.dockerignore`; per-Dockerfile ignores would require BuildKit-only extensions we don't want. Future Dockerfiles built from repo-root context should extend it, not shadow it.
- **Tag by git SHA; `:latest` on main only.** PR builds never tag `:latest`; `TAG_LATEST=true` is an opt-in flag the CI sets only on `push` to `main`.
- **Builder bumped to `golang:1.23`.** The Phase-3 plan and the deleted `sandbox-daemon/Dockerfile` both used `golang:1.22`. Upstream `github.com/atombender/go-jsonschema@v0.18.0` (invoked by `proto/codegen/go.sh`) raised its toolchain floor to 1.23; the build failed at the codegen step until we bumped the builder. The runtime stage is unchanged; the daemon's `go.mod` declares `go 1.22` as a minimum, which a 1.23 toolchain honours. **Follow-up:** `daemon.yml` and `proto.yml` pin `setup-go@v5` `go-version: "1.22"` and will hit the same wall in CI — bump in the next PR.

### Cross-cutting: production token-pubkey baking path is live

Phase 1 settled the contract; Phase 2 made the daemon verify against it; Phase 3 closes the loop on **how the verifier reaches the daemon in production**. PEM is baked into the image layer at build time, written to `/etc/rommel/token.pubkey`, and loaded by the entrypoint. Backend signing key (Phase 4) and daemon verifying key are now provably tied to a deployed image SHA — the property we wanted from Decision 0.2.

### Verification

```sh
make -C workspace-image build                # → rommel-workspaces:<short-sha>, ~25s warm
docker image inspect rommel-workspaces:<sha> --format '{{.Size}}'   # → 69,355,305 bytes (66 MiB)

# happy-path smoke
docker run -d --rm -p 7777:7777 -e ROMMEL_WID="dev-workspace" rommel-workspaces:<sha>
curl -fsS http://localhost:7777/healthz      # → "ok" on first poll (<200ms after container start)
# daemon log line: "daemon: listening on :7777 (wid=dev-workspace, root=/workspace)"

# signal-forwarding smoke
time docker stop -t 10 <cid>                 # → 0m0.133s  (tini → daemon graceful shutdown)

# fail-fast smoke
docker run --rm rommel-workspaces:<sha>      # → "entrypoint: ROMMEL_WID is required ..." exit 1
```

Deferred: `fly machine run` cold-start measurement (needs `fly auth login`; recipe in `workspace-image/README.md` §"Deploy a machine and measure cold start").

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §4: **`backend/`** — FastAPI control plane. Newly unblocked by Phase 3: `POST /workspaces/:id/sessions` has a real verifier to mint tokens for; `services/fly_orchestrator.py`'s `create_machine` has a real image ref (`registry.fly.io/rommel-workspaces:<sha>`); the Pattern B loop (browser → backend `/sessions` → daemon WS) is now wire-realistic on the daemon side.

---

## 0.1.2 — 2026-05-12

**Phase 2 — `sandbox-daemon/` Go binary.** Completion doc: [`docs/completions/phase-2-sandbox-daemon.md`](./completions/phase-2-sandbox-daemon.md). Plan: [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §2.

Status: ✅ Single Go binary (`sandbox-daemon`) that upgrades `/ws?token=…` to a WebSocket, validates EdDSA-signed broker tokens against `protogen.SessionTokenClaims`, round-trips `system.ping`, and implements `fs.read` with a workspace-root path sandbox. Every other primitive from `primitives.md` §1 returns a `not_implemented` error envelope so the surface area is visible. 13 WS-level tests + 3 config tests, all green; `go vet` clean; static binary builds via `make build`.

### Added

- **`sandbox-daemon/`** Go module (module path `github.com/rommel-ade/rommel/sandbox-daemon`, Go 1.22):
  - `cmd/daemon/main.go` — config load, route table, `http.Server` with `/healthz` (unauthenticated) and `/ws`, graceful shutdown on SIGINT/SIGTERM.
  - `internal/config/` — env parsing (`ROMMEL_PORT`, `ROMMEL_WORKSPACE_ROOT`, `ROMMEL_WID`, `ROMMEL_TOKEN_PUBKEY` as PEM-encoded Ed25519). Fails fast with **all** errors listed (not first-fail), so an under-configured deploy gets one diagnostic, not three.
  - `internal/auth/` — `Verify(token, pub, expectedWID)` enforces `alg=EdDSA` allow-list, `iss=rommel-backend`, `aud=rommel-daemon`, `exp > now`, `wid` match; runs claims through `protogen.SessionTokenClaims.UnmarshalJSON` for required-field + scope-enum validation; ships a `HasAnyScope` helper for the dispatcher.
  - `internal/ws/` — local `Frame` wire type (with `json.RawMessage` payload) wrapping `protogen.Envelope`; gorilla upgrade; per-conn read loop; scope-gated handler dispatch; stable error-code constants (`bad_request`, `not_implemented`, `unknown_type`, `forbidden`, `internal`, `fs.not_found`, `fs.invalid_path`, `fs.io`).
  - `internal/fs/` — real `fs.read`: workspace-relative path joined to `Root`, `Clean`'d, prefix-checked via `filepath.Rel` (rejects absolute paths and `..` escapes); utf-8/base64 encoding per request; `fs.write`/`fs.list`/`fs.watch` wired but return `not_implemented`.
  - `internal/pty/` — all `pty.*` verbs return `not_implemented` (PTY lands in a later phase; `creack/pty` import deferred until it's actually needed).
  - `internal/workspace/` — `workspace.info` returns `{id, daemon_version}` from config; `Repo` omitted until git plumbing lands.
  - `Makefile` — `bootstrap`, `lint`, `test`, `build`, `run-local`, `clean`. The Go proto gen file is declared as a Make prerequisite, so `cd sandbox-daemon && make test` on a fresh clone auto-runs `proto/codegen/go.sh`.
  - `Dockerfile` — multi-stage; build context is the repo root so the daemon can see `proto/` for codegen. Output image: `debian:stable-slim` + `tini` + static daemon binary.
  - `.golangci.yml` — minimal config (errcheck/gofmt/goimports/govet/ineffassign/misspell/staticcheck/unused) with `local-prefixes` set to the module path.
  - `README.md` — env table, local-dev recipe, wire-format pointer.
- **Tests** (16 total):
  - `internal/config/config_test.go` — env happy path, missing-required-vars listing, non-dir workspace root.
  - `internal/ws/server_test.go` — full WS round-trip suite: healthz, missing/bad-signature/wrong-wid/expired-token upgrade rejections, `system.ping`, unknown primitive, `fs.read` (utf-8 + base64 + absolute-path-rejected + `..`-rejected + not-found), `fs.write` stub, insufficient-scope forbidden, malformed envelope.

### Modified

- **`.github/workflows/daemon.yml`** — added a `Regenerate Go proto client` step that runs `bash proto/codegen/go.sh` between `setup-go` and `vet`. The gen file is gitignored so CI needs to materialize it before any compile step touches `protogen`.

### Decisions

- **Module path mirrors proto's placeholder org.** `github.com/rommel-ade/rommel/sandbox-daemon` lines up with `github.com/rommel-ade/rommel/proto/clients/go`. Both flip together when the real GitHub org lands.
- **`replace ../proto/clients/go` in go.mod, not `go.work`.** Per the changelog 0.1.1 "Next" callout. A top-level `go.work` would let the replace go away — deferred to a follow-up since it's not blocking and changes a top-level invariant.
- **Local `ws.Frame` type with `json.RawMessage` payload.** Generated `protogen.Envelope` uses `interface{}` for payload (correct for JSON Schema, awkward for dispatch). The local Frame keeps the wire shape identical but lets handlers receive raw payload bytes — clean seam between codec and router.
- **`type: "system.ping"`, not `"ping"`.** The envelope schema's `type` pattern requires dotted form. `system.*` is reserved for daemon-level lifecycle (future `system.health`, `system.version`).
- **`WithValidMethods([]string{"EdDSA"})` on JWT parse.** Required to avoid `alg=none` / algorithm-confusion attacks; `jwt/v5` does not enforce a method allow-list by default.
- **Claims validated through `protogen.SessionTokenClaims.UnmarshalJSON`.** Parse → re-marshal → unmarshal pipes the bag through the schema's generated validation (required fields + scope-enum). One schema, no duplicated validation code in the daemon.
- **Path sandbox is `Clean` + `Rel` prefix check; no `EvalSymlinks`.** Confirmed with the user. Rejects absolute paths and `..` escapes; symlink-resolution is deferred until the daemon graduates from scaffolding (the daemon's own README and the completion doc both flag this).
- **Routes as a `map[string]Route`, not a switch.** Required scopes sit alongside handler functions in one screen of `cmd/daemon/main.go`. Adding a primitive is a map entry. Audit-friendly.
- **Stubs return `code: "not_implemented"`, every primitive is wired.** Every `primitives.md` §1 verb has a route entry. Clients discover the surface from the wire, not from team channels.
- **Daemon Makefile treats `proto/clients/go/gen/proto.go` as a prerequisite.** Cold-start `cd sandbox-daemon && make test` works on a fresh clone — Make calls `proto/codegen/go.sh` automatically.

### Cross-cutting: capability scoping is live

Phase 1 committed the scope vocabulary to the schema (`fs:r`, `fs:rw`, `pty:rw`, …). Phase 2 actually enforces it: `cmd/daemon/main.go::buildRoutes` binds each primitive to its required scopes (any-of), and the dispatcher returns `forbidden` if the token's `scope` array doesn't satisfy the route. The `TestFsRead_InsufficientScope_Forbidden` test confirms the gate fires for a `pty:rw`-only token trying `fs.read`.

### Verification

```sh
cd sandbox-daemon
make test                                # 16 tests, all pass
make build                               # → dist/sandbox-daemon (static binary)
make lint                                # go vet ./... clean

# Cold-start: proto gen file gets regenerated automatically
rm -rf ../proto/clients/go/gen
make test                                # → Make runs proto/codegen/go.sh, then tests pass
```

### Next

Per [`docs/executing/scaffolding-plan.md`](./executing/scaffolding-plan.md) §3: **`workspace-image/`** — Docker image that bakes the daemon binary plus baseline tools (`git`, `curl`, `ca-certificates`, `tini`), shipped to Fly's registry as the image used by the Machines API to spawn per-workspace VMs. The Dockerfile in `sandbox-daemon/` is already a working multi-stage build for the binary — the `workspace-image/` subtree wraps it into the deployable artifact (Fly app: `rommel-workspaces`).

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
