# Phase 2 — `sandbox-daemon/` (Completion)

**Plan:** [`docs/executing/scaffolding-plan.md`](../executing/scaffolding-plan.md) §2
**Date:** 2026-05-12
**Status:** ✅ Complete. WS upgrade at `/ws?token=…` validates EdDSA-signed broker tokens against `protogen.SessionTokenClaims`, `system.ping → pong` round-trips, and `fs.read` is implemented end-to-end with a sandboxed workspace root. All other primitives from `primitives.md` §1 return a `not_implemented` error envelope so the surface area is visible.

---

## What was built

A Go binary (`sandbox-daemon`) that imports the generated proto Go client (`protogen`) and exposes the daemon primitives over a single WebSocket endpoint. The binary statically compiles to ~7 MB and lands at `dist/sandbox-daemon`; the same source produces an image via the Phase 3 `workspace-image/` consumer.

### Files created

```
sandbox-daemon/
├── README.md
├── Makefile                          # bootstrap / lint / test / build / run-local
├── Dockerfile                        # multi-stage; static binary into debian:stable-slim
├── .golangci.yml
├── go.mod  / go.sum                  # replace ../proto/clients/go (until go.work)
├── cmd/daemon/main.go                # config, routes, http.Server, graceful shutdown
└── internal/
    ├── config/
    │   ├── config.go                 # env parsing (ROMMEL_{PORT,WORKSPACE_ROOT,WID,TOKEN_PUBKEY})
    │   └── config_test.go
    ├── auth/
    │   └── token.go                  # EdDSA verify, iss/aud/wid/exp checks, scope helper
    ├── ws/
    │   ├── envelope.go               # Frame type with json.RawMessage payload; error codes
    │   ├── server.go                 # gorilla upgrade, per-conn loop, scope-gated dispatch
    │   └── server_test.go            # full WS roundtrip test suite (13 cases)
    ├── fs/
    │   └── handler.go                # fs.read real; fs.write/list/watch stubbed
    ├── pty/
    │   └── handler.go                # all stubs in v1
    └── workspace/
        └── info.go                   # workspace.info from config
```

### Files modified

- **`.github/workflows/daemon.yml`** — added a `Regenerate Go proto client` step that runs `bash proto/codegen/go.sh` between `setup-go` and `vet`. The gen file is gitignored, so without this CI couldn't compile the daemon.

### Files deleted

None.

---

## Decisions made

### Module path: `github.com/rommel-ade/rommel/sandbox-daemon`
Mirrors the placeholder org used by `proto/clients/go` (`rommel-ade`). When the real GitHub org lands, both modules' paths flip in lockstep — calling this out so the rename stays a single search-and-replace.

### Proto Go client imported via `replace ../proto/clients/go`
Per the changelog 0.1.1 "Next" callout: until a top-level `go.work` lands, `sandbox-daemon/go.mod` carries a `replace` directive pointing at the sibling proto module. Works in CI because the entire repo is checked out side-by-side. The plan to swap to `go.work` later is unchanged.

### Wire format: local `Frame` type wraps `protogen.Envelope`
The generated `protogen.Envelope` has `Payload interface{}` (faithful to the JSON Schema). For dispatch we want raw bytes so handlers can unmarshal into their own typed shape without a re-marshal round-trip. Defined `ws.Frame` with `Payload json.RawMessage` — same wire shape (encodes to/from the same JSON), but a clean seam between the codec and the router.

### Envelope `type` values use dotted form, including for ping
The envelope schema's `type` pattern is `^[a-z][a-z0-9]*\.[a-z][a-z0-9_-]*$`. The plan's example "`ping`" would fail that pattern — so the daemon uses **`system.ping`** instead, reserving a `system.*` domain for daemon-level lifecycle verbs (future `system.version`, `system.health`, etc.). Cost: zero. Benefit: schema-faithful from day one.

### JWT validation: `WithValidMethods([]string{"EdDSA"})`
Required to avoid the classic `alg=none` and HS-vs-RS confusion attacks where a token's header dictates the verifier. `golang-jwt/jwt/v5` does *not* enforce a method allow-list by default; this is the one knob that's load-bearing for security.

### Claims validation: parse, then JSON-roundtrip into `protogen.SessionTokenClaims`
Two-stage: (1) `jwt.Parse` enforces signature, `iss`, `aud`, `exp`. (2) `json.Marshal(parsed.Claims)` → `json.Unmarshal(&protogen.SessionTokenClaims)` runs the generated `UnmarshalJSON` hooks (required fields, scope-enum membership). One schema, two checkpoints — no duplicated validation rules.

### `fs.read` path sandbox: Clean + relative-to-root prefix check (no `EvalSymlinks`)
Confirmed with the user. Reject absolute paths outright; else `filepath.Join(root, req.path)`, `filepath.Clean`, then `filepath.Rel(rootClean, clean)` and verify the result doesn't start with `..`. Symlink-following is *not* resolved at this layer — fast, predictable, sufficient for v1 scaffolding. The known footgun (a symlink to `/etc/passwd` inside the workspace) gets a `filepath.EvalSymlinks` pass when the daemon is no longer a scaffolding stub.

### Routes as data, not a `switch` statement
`cmd/daemon/main.go::buildRoutes` exposes the full primitive map in one screen, with required scopes alongside. Adding a new primitive is one map entry — no dispatcher edit. Audit-friendly: `grep "fsRw," cmd/daemon/main.go` enumerates every write-gated primitive.

### Stubs return `code: "not_implemented"`, not 404 or silent ack
Every primitive listed in `primitives.md` §1 is wired to a route — `fs.write`, `fs.list`, `fs.watch`, `pty.open`, `pty.input`, `pty.resize`. Trying to use them returns an error envelope with `code: "not_implemented"`. Two benefits: (1) the wire surface is *discoverable* by clients (they don't have to ask the team what's wired up), (2) when the real impl lands, only the handler swaps — no route registration needed.

### Daemon Makefile treats the proto gen file as a Make prerequisite
`PROTOGEN := ../proto/clients/go/gen/proto.go` is listed as a dependency of `bootstrap`, `lint`, `test`, `build`. If missing, the rule runs `bash ../proto/codegen/go.sh` automatically. Result: a fresh contributor running `cd sandbox-daemon && make test` gets a working green build without first running `make proto` at the repo root.

---

## Verification

### Build
```sh
cd sandbox-daemon
make build              # → dist/sandbox-daemon (Mach-O / arm64 locally; static ELF in Docker)
```

### Tests
```sh
make test
# ok    .../internal/config   (3 tests)
# ok    .../internal/ws       (13 tests)
```

Test cases (`internal/ws/server_test.go`):
- `/healthz` returns 200
- Upgrade rejected without token (401)
- Upgrade rejected on wrong signature (401)
- Upgrade rejected on wrong `wid` (401)
- Upgrade rejected on expired token (401)
- `system.ping → response` round-trips
- Unknown primitive returns `unknown_type` error envelope
- `fs.read` happy path (utf-8) — contents, size, encoding all match
- `fs.read` base64 encoding round-trips binary content
- `fs.read` rejects absolute path (`/etc/passwd`) with `fs.invalid_path`
- `fs.read` rejects `..` escape with `fs.invalid_path`
- `fs.read` of nonexistent file returns `fs.not_found`
- `fs.write` stub returns `not_implemented`
- `fs.read` with token carrying only `pty:rw` returns `forbidden`
- Malformed envelope (`{"kind":"request"}` missing `type`) returns `bad_request`

### Cold-bootstrap from fresh clone
```sh
rm -rf proto/clients/go/gen          # simulate fresh checkout (gen is gitignored)
cd sandbox-daemon && make test       # → Make regenerates the gen file, then tests pass
```

### Lint
```sh
make lint                            # → go vet ./... (clean); golangci-lint optional
```

### CI workflow wakes up
`.github/workflows/daemon.yml` gates on `sandbox-daemon/go.mod`. With this phase, the file now exists, so the workflow will trigger on push/PR. The new "Regenerate Go proto client" step runs `proto/codegen/go.sh` before vet/test/build so CI has the gen file the daemon imports.

---

## Cross-cutting

### Session token contract is now exercised end-to-end
Phase 1 committed `proto/schemas/session-token.json`. Phase 2 actually validates against the generated `protogen.SessionTokenClaims` — `iss`/`aud` consts, `wid` match, `scope` enum membership, `exp` enforcement. The contract is no longer aspirational; the backend (Phase 4) can mint tokens against this exact verifier behaviour.

### Capability scoping lands in code
`SessionTokenClaimsScopeElem` values (`fs:r`, `fs:rw`, `pty:rw`, …) flow from the schema → generated Go consts → route definitions in `main.go::buildRoutes`. Adding a new scope is: add to schema enum → regenerate → reference in route. The `forbidden` test confirms the gate fires.

---

## What's next

Per `docs/executing/scaffolding-plan.md` §3: **`workspace-image/`** — the Fly Machine VM image that bakes this daemon binary plus baseline tooling (`git`, `curl`, `ca-certificates`, `tini`). The `Dockerfile` already in `sandbox-daemon/` is a single-binary build target; `workspace-image/` will be the deployable artifact (Fly app `rommel-workspaces`, image consumed by the Machines API).

Optional cleanup, deferred to a follow-up:
- **`go.work` at the repo root** would remove the `replace` directive in `sandbox-daemon/go.mod` and let multi-Go-module IDE tooling resolve imports without manual prodding. Cheap, but not blocking.
- **`golangci-lint` CI step**. Local lint conditionally runs it; CI today does only `go vet`. Add to `daemon.yml` once the linter config has had a few weeks to settle.
- **Replace-attack protection on `jti`**. Schema reserves the field; daemon doesn't track it. Wire up an LRU once the threat model demands it.
