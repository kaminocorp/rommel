# Phase 1 — `proto/` Source-of-Truth + Codegen (Completion)

**Plan:** [`docs/executing/scaffolding-plan.md`](../executing/scaffolding-plan.md) §1
**Date:** 2026-05-04
**Status:** ✅ Complete. `make proto` regenerates all three clients with **zero diff** on the second run; Go output compiles; outputs visually inspected and look right.

---

## What was built

A single source of truth (`proto/schemas/`) producing three generated clients (TS, Go, Pydantic v2) via per-language codegen scripts plus a top-level `proto/codegen.sh` orchestrator.

### Files created

```
proto/
├── README.md                              # how to add a schema, how to regenerate
├── codegen.sh                             # runs all three codegen scripts
├── codegen/
│   ├── ts.sh                              # uses npx json-schema-to-typescript
│   ├── go.sh                              # uses go run github.com/atombender/go-jsonschema
│   └── python.sh                          # uses self-contained venv at .venv/
├── schemas/
│   ├── envelope.json                      # WS envelope (kind/type/id/payload/error)
│   ├── session-token.json                 # JWT claims (EdDSA-signed broker token)
│   ├── fs/
│   │   ├── read.json                      # FsReadRequest / FsReadResponse (real)
│   │   ├── write.json                     # stub
│   │   ├── list.json                      # stub
│   │   └── watch-event.json               # stub
│   ├── pty/
│   │   ├── open.json                      # PtyOpenRequest / PtyOpenResponse (real)
│   │   ├── input.json                     # PtyInput (real)
│   │   ├── output-event.json              # PtyOutputEvent (real)
│   │   └── resize.json                    # stub
│   └── workspace/
│       └── info.json                      # WorkspaceInfo (real)
└── clients/
    ├── ts/
    │   ├── README.md
    │   └── package.json                   # @rommel/proto, pnpm workspace dep
    ├── go/
    │   ├── README.md
    │   └── go.mod                         # github.com/rommel-ade/rommel/proto/clients/go
    └── python/
        ├── README.md
        └── pyproject.toml                 # rommel-proto, hatchling build
```

### Files modified

- `.gitignore` — added `proto/codegen/.venv/` so the Python codegen venv isn't tracked.

### Files deleted

- `proto/schemas/funnel/.gitkeep`, `proto/schemas/git/.gitkeep` — placeholders confused datamodel-code-generator (it warns on non-JSON files in input dirs). The directories will materialize when their first real schema lands; their existence is documented in `proto/README.md`.

---

## Decisions made

### Format: JSON Schema (draft 2020-12), not Protobuf
`techstack.md` left this open. Picked JSON Schema for v1:
- Daemon traffic is JSON-over-WebSocket (`primitives.md` cross-cutting Q1) — no binary framing layer to bolt on.
- Browser devtools render the wire format directly. No `.proto` decoder needed to read a frame in the Network panel — this is the killer feature for hot-path debugging.
- Mature codegen on all three sides (proven by this phase).
- If profiling later demands Protobuf, the schemas port over field-for-field.

### Schema shape: `$defs` with named subschemas + `oneOf`, not anonymous `oneOf`
Initial drafts used anonymous `oneOf` — both `json-schema-to-typescript` and `go-jsonschema` produced ugly anonymous types. Restructuring to `$defs.FsReadRequest` + `$defs.FsReadResponse` + a `oneOf: [$ref, $ref]` at the root produced clean, importable type names across all three languages. This is now the convention for any RPC-shaped schema.

### Codegen tool selection (with version pins)
| Lang | Tool | Why |
|---|---|---|
| TS | `json-schema-to-typescript@^15` (via `npx --yes`) | Mature, single-file or directory mode, produces idiomatic TS with JSDoc from `description:` fields. Pinned to v15 major. |
| Go | `github.com/atombender/go-jsonschema@v0.18.0` (via `go run`) | Generates `UnmarshalJSON` validation hooks for free; supports `--capitalization` for ID/URL/JTI initialisms. Pinned to v0.18.0. (The `omissis/go-jsonschema` fork was tried first; older versions still declare the upstream module path so `go install` rejects them — landing on the upstream is simpler.) |
| Python | `datamodel-code-generator==0.31.2` (via self-contained venv) | Pydantic v2 output, supports `--disable-timestamp` for idempotency. |

All three are version-pinned: reproducible CI is the whole point of this phase.

### Python venv strategy: hermetic, bootstrapped on first run
The `python.sh` script creates `proto/codegen/.venv/` on first run, installs `datamodel-code-generator` into it, marks the install with `.installed-<version>`, and reuses on subsequent runs. Bumping the version invalidates the marker and triggers a clean reinstall. **No global Python deps required** — `make proto` works from a clean clone with just system Python.

### Go module path uses `rommel-ade/rommel` placeholder
The `go.mod` declares `module github.com/rommel-ade/rommel/proto/clients/go`. Real GitHub org should replace `rommel-ade` once the repo is pushed; consumers (`sandbox-daemon/`) will need their imports updated in lockstep. Calling this out explicitly so it doesn't get lost.

### Generated source is gitignored; only metadata is committed
Per the plan: `proto/clients/*/{src,gen}/` are gitignored. Committed are `package.json` (TS), `go.mod` (Go), `pyproject.toml` (Python), and per-client READMEs. CI's `proto.yml` workflow re-runs codegen and fails if the result diverges from `schemas/` — this is what catches the "someone hand-edited the generated code" footgun.

### `oneOf` wrapper at the schema root
For RPC-shaped schemas (`fs/read.json`, `pty/open.json`), the root has both a `$defs` block (the named types) and a `oneOf: [$ref, $ref]` (the discriminated union). The `oneOf` produces a useful TS union type (`FsRead = FsReadRequest | FsReadResponse`); the `$defs` gives Go and Python their named structs/classes. This double-encoding is intentional and ergonomic.

---

## Verification

### Idempotency (the load-bearing criterion)
```sh
make proto              # first run: bootstraps Python venv, fetches Go module, ~30s
cp -r proto/clients .snap
make proto              # second run: ~3s
diff -r .snap proto/clients   # → empty
```
Confirmed: zero diff across all three clients on second run.

### Output structure
- **TS:** 11 schema files → 11 `.ts` modules + an auto-generated `index.ts` re-exporting everything. Discriminated unions land cleanly: `export type FsRead = FsReadRequest | FsReadResponse;`.
- **Go:** Single `gen/proto.go` (package `protogen`) with one `type` per `$defs` entry plus `UnmarshalJSON` validation methods. Builds clean: `go build ./gen/...` from `proto/clients/go/` exits 0.
- **Python:** Per-domain submodules (`gen/fs/read.py`, `gen/pty/open.py`, etc.) with Pydantic v2 BaseModel classes. `--disable-timestamp` keeps headers diff-stable.

### Tooling presence (for the record)
The codegen scripts assume `node` (with `npx`), `go`, and `python3` are on PATH. All three are pinned in `.tool-versions`. CI workflows install them explicitly.

---

## Cross-cutting: session token contract is now committed

Per the scaffolding plan's cross-cutting note: the **session token shape** was a prerequisite for sections 2 (daemon) and 4 (backend) to integrate. With `proto/schemas/session-token.json` in place, that contract is now live:

- **Algorithm:** EdDSA (Ed25519) — documented in the schema's `description`.
- **Claims:** `iss`, `sub`, `aud`, `wid`, `scope`, `exp`, `iat`, `jti` — all required, all enumerated in the schema.
- **Scope vocabulary:** `fs:r`, `fs:rw`, `pty:rw`, `git:r`, `git:rw`, `funnel:r`, `funnel:rw`, `policy:r` — answers `primitives.md` cross-cutting Q5 (capability scoping) directly in the type system.
- **Key handoff:** backend signs (private key from Fly secret), daemon verifies (public key baked into VM image at deploy time).

The phase 4 backend and phase 2 daemon will both consume `SessionTokenClaims` types generated from this single schema — there is no second source for these fields anywhere.

---

## What's next

Per `docs/executing/scaffolding-plan.md` §2: **`sandbox-daemon/`** — Go binary that:
- accepts a WebSocket at `/ws?token=...`
- validates the token against `SessionTokenClaims` (EdDSA pubkey via env)
- handles `ping → pong` and a real `fs.read` to prove the proto loop works end-to-end
- stubs everything else from `primitives.md` §1 with `not_implemented`

Daemon will import `github.com/rommel-ade/rommel/proto/clients/go/gen` (package `protogen`) — likely via a `replace` directive in its own `go.mod` until a `go.work` file lands at the repo root.
