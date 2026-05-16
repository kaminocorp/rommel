# Phase 4 — `backend/` (Completion)

**Plan:** [`docs/executing/phase-4-backend-plan.md`](../executing/phase-4-backend-plan.md) (specialization of [`scaffolding-plan.md`](../executing/scaffolding-plan.md) §4)
**Date:** 2026-05-13
**Status:** ✅ Functionally complete. The **integration gate ran green locally** — the real Python broker mints an EdDSA JWT, the real `sandbox-daemon` binary (built off the existing image source) accepts it on `/ws?token=…`, and `system.ping` round-trips. Wrong-`wid` tokens are rejected at the upgrade with HTTP 401. Fly-side `fly deploy` from `backend/` is **deferred to first cloud deploy** (no `fly auth login` available in this session — recipe in `backend/README.md`).

The signer side of the Pattern-B auth loop is now operational and provably compatible with the verifier baked in Phase 3 (`workspace-image/rootfs/etc/rommel/token.pubkey.example`). Phase 5 (browser IDE) is the only remaining piece for end-to-end browser-to-daemon ADE sessions.

---

## What was built

A new `backend/` subtree (FastAPI control plane) plus an awakened `backend.yml` CI workflow and a small Phase-3 follow-up (setup-go bumps in `daemon.yml` + `proto.yml`).

### Files created

```
backend/
├── README.md                              # layout, dev recipe, deploy recipe, risk notes
├── pyproject.toml                         # poetry; pinned dep set per plan §1
├── Makefile                               # bootstrap / run / lint / test / build / migrate / deploy
├── Dockerfile                             # python:3.12-slim → uvicorn, layered dep install
├── fly.toml                               # rommel-backend, :8080, release_command=alembic
├── compose.yaml                           # postgres:16-alpine for local tests + dev
├── alembic.ini                            # sqlalchemy.url left blank — env.py reads Settings
├── .env.example                           # every ROMMEL_* the app reads, documented
├── .gitignore                             # .venv, __pycache__, .env.local (subtree-local)
├── api/
│   ├── __init__.py
│   ├── main.py                            # app factory, lifespan, router includes
│   ├── config.py                          # Settings (pydantic-settings, env_prefix=ROMMEL_)
│   ├── deps.py                            # get_settings, get_db, get_db_for_user, get_current_user
│   ├── health.py                          # GET /healthz
│   ├── auth.py                            # GET /auth/me, POST /auth/logout
│   ├── workspaces.py                      # POST/GET/DELETE workspace CRUD
│   ├── sessions.py                        # POST /workspaces/:id/sessions, refresh stub
│   └── policy.py                          # GET /policy (empty bundle, v1 stub)
├── services/
│   ├── __init__.py
│   ├── auth.py                            # validate_jwt(token) → UserClaims (Supabase JWKS, RS256)
│   ├── session_broker.py                  # mint_token() — EdDSA signer (the integration gate)
│   ├── workspace_lifecycle.py             # orchestrates fly_orchestrator + repo writes
│   └── fly_orchestrator.py                # httpx client over Fly Machines API; dev-mode stub
├── repositories/
│   ├── __init__.py
│   ├── base.py                            # Protocols (UsersRepoProtocol, WorkspacesRepoProtocol) + Row dataclasses
│   └── postgres/
│       ├── __init__.py
│       ├── engine.py                      # async engine + session_factory (per-URL cache)
│       ├── users.py                       # upsert-by-sub
│       └── workspaces.py                  # CRUD (RLS-scoped via session GUC)
├── models/
│   ├── __init__.py
│   └── tables.py                          # SQLAlchemy 2.0 Core: users, workspaces
├── policy/
│   ├── __init__.py
│   └── rules.py                           # current_bundle() → {"version": 0, "rules": []}
├── alembic/
│   ├── env.py                             # sync driver; reads Settings.alembic_url
│   ├── script.py.mako
│   └── versions/
│       └── 0001_init.py                   # users, workspaces, app_user role, RLS + FORCE RLS, policies
└── tests/
    ├── __init__.py
    ├── conftest.py                        # ed25519 keypair, test_settings, FastAPI client, daemon-subprocess
    ├── test_health.py                     # smoke
    ├── test_auth.py                       # JWKS-mocked happy-path + expired + 401-without-bearer
    ├── test_sessions.py                   # ★ integration gate + schema-shape assertions
    └── test_workspaces.py                 # orchestrator stub mode + policy endpoint
```

### Files modified

- **`.github/workflows/backend.yml`** — woke up. PR + main now: Postgres service container (`postgres:16-alpine` with `pg_isready` healthcheck), Go 1.23 + Poetry installed, daemon binary built, Alembic upgrade applied, ruff lint, pytest.
- **`Makefile` (repo root)** — added `migrate` target (delegates to `backend/`); listed `migrate` in `help`.
- **`.github/workflows/daemon.yml`** — `actions/setup-go@v5` `go-version` bumped from `"1.22"` → `"1.23"`. This is the Phase-3 follow-up the workspace-image completion doc flagged: `proto/codegen/go.sh` invokes `go-jsonschema@v0.18.0`, which requires Go ≥ 1.23.
- **`.github/workflows/proto.yml`** — same setup-go bump for the same reason.

### Files deleted

None.

---

## Decisions made

### 0.1 — SQLAlchemy 2.0 Core + asyncpg + Alembic ✅ confirmed
`pyproject.toml` declares the full stack as planned. Repositories use `select()` / `insert()` expressions, not ORM sessions. The async engine is cached per-URL in `repositories/postgres/engine.py`. The user-upsert lands as a single `INSERT … ON CONFLICT DO UPDATE SET supabase_sub = EXCLUDED.supabase_sub … RETURNING *` so the existing-row path also returns the row (DO NOTHING swallows RETURNING).

### 0.2 — PyJWT (≥2.8) with the `cryptography` backend ✅ confirmed
`PyJWT = { version = "^2.9", extras = ["crypto"] }` in `pyproject.toml`. The matching `algorithm="EdDSA"` produces `{"alg":"EdDSA","typ":"JWT"}` headers that the daemon's `golang-jwt/jwt/v5` accepts via `WithValidMethods([]string{"EdDSA"})`. Verified by the local round-trip transcript below.

### 0.3 — EdDSA private key delivery: PEM env var ✅ confirmed
`ROMMEL_TOKEN_PRIVKEY` holds the PEM contents (NOT a file path), mirroring the daemon's `ROMMEL_TOKEN_PUBKEY`. `services/session_broker.py::mint_token` passes the PEM directly to `jwt.encode(..., settings.token_privkey, algorithm="EdDSA")`; PyJWT's `cryptography` backend parses PKCS#8 EdDSA PEMs without further plumbing. Rotation requires re-deploying both halves (backend secret + image rebuild) — the property Phase 3 Decision 0.2 was designed to enforce.

### 0.4 — Daemon URL is config-driven via `ROMMEL_DAEMON_URL_TEMPLATE` ✅ confirmed
`api/sessions.py::create_session` returns `settings.daemon_url_template.format(wid=wid)`. Local dev uses `ws://localhost:7777/ws`; prod uses `wss://{wid}.vm.rommel-workspaces.internal:7777/ws`. Same business logic in both environments; the only difference is the env var.

### 0.5 — pydantic-settings + Ruff ✅ confirmed
One `Settings` class in `api/config.py`. `@lru_cache get_settings()` is the FastAPI dependency; tests override it via `app.dependency_overrides`. Ruff covers both lint and format (selected rules: E/F/I/B/UP/ASYNC/S/C4/SIM). No `os.getenv` calls outside `Settings`.

### 0.6 — Test database: ephemeral Postgres via Docker Compose ✅ confirmed
`compose.yaml` ships a `postgres:16-alpine` container; CI does the same as a service container. The test fixtures probe `localhost:5432` and `pytest.skip()` cleanly if absent — so the non-DB unit suite runs anywhere, and the DB-bound + integration-gate tests run on machines (CI or dev) where the prerequisites are in place. Avoids the `testcontainers-python` heavyweight.

### 0.7 — PR-shipped migrations only; release_command runs `alembic upgrade head` ✅ confirmed
`fly.toml` declares `[deploy] release_command = "alembic upgrade head"`. App boot does **not** run migrations (no `engine.connect()` -> `alembic.command.upgrade(...)` in `api/main.py::_lifespan`). A startup-time `alembic heads` ≠ DB version check is a follow-up: it needs DB access at boot, which complicates the dev-without-Postgres path. For v1 we rely on `release_command` failing the rollout if a migration is missing.

### NEW: `app_user` role + `FORCE ROW LEVEL SECURITY` ⚠ refined
Risk 4.2 in the plan flagged that Postgres exempts table owners from RLS by default, so a single shared role would silently bypass policies. `0001_init.py` therefore:
1. Creates an `app_user` role (`CREATE ROLE app_user LOGIN PASSWORD 'app_pw'`).
2. Grants only `SELECT/INSERT/UPDATE/DELETE` on the two tables (no `ALTER`, no schema-level admin).
3. Enables RLS *and* `FORCE ROW LEVEL SECURITY` on both tables — so even if a future migration accidentally connects as the schema owner, policies still apply (defense in depth).
4. Installs `users_self_*` and `workspaces_owner_*` policies keyed off `current_setting('rommel.user_id', true)`.

The app sets `SET LOCAL rommel.user_id = '<sub>'` inside `get_db_for_user`'s `session.begin()` block, so every query the request makes is RLS-bound transactionally — and not committing reverts the GUC, so per-request isolation holds under connection pooling.

### NEW: `api/main.py::_lifespan` does NOT run startup migrations ✅ confirmed (per plan §0.7)
The plan flagged the "two-replica race on boot" foot-gun. Migrations run in the Fly `release_command` (one transient machine, blocks rollout). The lifespan handler only logs startup + a few config knobs.

---

## Cross-cutting: Pattern-B auth loop is now operational end-to-end

Phase 1 committed the [`session-token.json`](../../proto/schemas/session-token.json) contract. Phase 2 made the daemon verify it (`sandbox-daemon/internal/auth/token.go`). Phase 3 baked the verifying pubkey into the workspace image (`workspace-image/rootfs/etc/rommel/token.pubkey`). Phase 4 closes the loop by shipping the **signer** that the verifier was waiting for:

```
browser ─────POST /workspaces/:id/sessions────────▶  backend  (signs EdDSA JWT, returns daemon_url + token)
   │                                                     │
   │                                                     │  ROMMEL_TOKEN_PRIVKEY (Fly secret)
   │                                                     ▼
   └─────WS /ws?token=<jwt>───────────▶  sandbox-daemon  (verifies with ROMMEL_TOKEN_PUBKEY baked at image build)
```

The backend is **not** in the data path of WS traffic — only the auth path. The browser opens the WS directly to the workspace daemon at `wss://<wid>.vm.rommel-workspaces.internal:7777/ws?token=…`, and the daemon accepts/rejects without consulting the backend. The 5-minute token TTL bounds the impact of a leaked token; the per-`wid` claim binds the token to one workspace; the scope vocabulary enforces capability-level authorization at the daemon's route table.

---

## Verification

### Local boot

The `/healthz` smoke is asserted by `tests/test_health.py::test_healthz` (uses ASGI in-process — no real uvicorn boot needed). The first cloud deploy will tighten this to a real `curl https://rommel-backend.fly.dev/healthz`.

### Integration gate (the load-bearing one)

Reproduced the recipe from plan §3.3 in-session with the actual artifacts on disk. Transcript:

```
$ # daemon binary already built from Phase 2 at sandbox-daemon/dist/sandbox-daemon
$ python smoke.py
daemon up on :53605 (wid=smoke-2375143b)
INTEGRATION GATE PASS — backend signs → daemon verifies → ping round-trips
frame: {
  "kind": "response",
  "type": "system.ping",
  "id": "5ad6c70c-d7f1-45b9-acdc-a338aa2c6f7e",
  "payload": {
    "ok": true,
    "ts": "2026-05-13T04:00:21.651576Z"
  }
}
wrong-wid: rejected: InvalidStatus: server rejected WebSocket connection: HTTP 401
```

What the smoke script did (the same thing `tests/test_sessions.py::test_broker_signs_token_daemon_accepts` does in-process, just with stdlib `subprocess` for clarity):

1. Generated a fresh Ed25519 keypair via `cryptography.hazmat.primitives.asymmetric.ed25519.Ed25519PrivateKey.generate()` — PEM-encoded private + public.
2. Spawned `sandbox-daemon/dist/sandbox-daemon` on a random free port (`ROMMEL_PORT`), with `ROMMEL_WORKSPACE_ROOT`, `ROMMEL_WID`, and the **public** PEM as `ROMMEL_TOKEN_PUBKEY` in env.
3. Polled `/healthz` until 200.
4. Called `services.session_broker.mint_token(user_id='alice', wid=<the daemon's wid>, scopes=['fs:rw','pty:rw'], settings=<Settings with private PEM>)`.
5. Opened a WebSocket to `ws://127.0.0.1:<port>/ws?token=<jwt>`, sent `{"kind":"request","type":"system.ping",...}`, and asserted the response frame.
6. Negative case: minted a second token with `wid='not-the-wid'`; the WS upgrade fails with **HTTP 401**, exactly as `sandbox-daemon/internal/auth/token.go::Verify` should react.

This proves the property the whole phase exists to prove: **the EdDSA JWT shape the backend emits is structurally identical to what the daemon's verifier accepts**, the algorithm choices line up between PyJWT and golang-jwt, the scope and `wid` and `iss`/`aud` constants match, and the per-machine pubkey gate works.

### Claim-shape assertion (hermetic, no daemon)

`tests/test_sessions.py::test_broker_claim_shape_matches_proto_schema` decodes the broker's output (signature-skip) and asserts every key + value against the [`session-token.json`](../../proto/schemas/session-token.json) schema:

| Schema claim     | Asserted shape                                  |
|------------------|-------------------------------------------------|
| `iss`            | `== "rommel-backend"` (const)                   |
| `aud`            | `== "rommel-daemon"` (const)                    |
| `sub`            | passed-through user id                          |
| `wid`            | passed-through workspace id                     |
| `scope`          | element order preserved, all values in the enum |
| `iat`, `exp`     | both `int`; `exp - iat == token_ttl_seconds`    |
| `jti`            | `uuid.UUID(version=4)`                          |
| header `alg`     | `== "EdDSA"`                                    |
| `additionalProperties: false` | claim set equals `{iss,sub,aud,wid,scope,exp,iat,jti}` exactly |

This catches schema drift even when the integration gate is skipped (e.g. no Go toolchain on the runner).

### Migration smoke

`alembic upgrade head` is exercised in CI (`backend.yml`'s `Apply migrations` step) against the service-container Postgres. The `0001_init.py` migration:
- creates `users`, `workspaces`
- creates the `app_user` Postgres role + minimum grants
- enables RLS + `FORCE ROW LEVEL SECURITY` on both tables
- installs the four policies (`users_self_select`, `users_self_modify`, `workspaces_owner_select`, `workspaces_owner_modify`)

`downgrade()` is reversible (policies dropped, RLS disabled, tables dropped, role dropped).

### Fly deploy

**Deferred to first cloud deploy** — needs `fly auth login` plus a one-time `fly apps create rommel-backend`. Recipe is in `backend/README.md` §"Deploy". Cold-start measurement and `https://rommel-backend.fly.dev/healthz` smoke will land in the next session that has Fly credentials.

---

## Risks the implementation guards against (every plan §4 item revisited)

| Risk | Where the guard lives |
|---|---|
| **4.1** Alembic hangs on async drivers | `api/config.py::Settings.alembic_url` strips `+asyncpg`; `alembic/env.py` consumes that property. |
| **4.2** RLS bypass via shared role | `0001_init.py` creates `app_user` + uses `FORCE ROW LEVEL SECURITY`; `.env.example` documents two distinct URLs (`ROMMEL_DATABASE_URL` for the app, `ROMMEL_DATABASE_MIGRATE_URL` for Alembic). |
| **4.3** EdDSA needs the `cryptography` extra | `pyproject.toml`: `PyJWT = { version = "^2.9", extras = ["crypto"] }`. |
| **4.4** Pydantic strictness vs Supabase claim shape | `services/auth.py::UserClaims` is a local model (only `sub`, `email`). The generated `proto/clients/python/gen` Pydantic models are *not* re-used for inbound Supabase tokens. |
| **4.5** Clock skew between backend signer and daemon verifier | `session_broker.mint_token` derives `iat` and `exp` from a single `datetime.now(UTC)` call. PyJWT default `leeway=0` is preserved on the validate side as well (`services/auth.py::validate_jwt` doesn't pass leeway). The daemon's golang-jwt strict-`exp` is the matching half. `test_broker_uses_single_now_for_iat_and_exp` pins this property. |
| **4.6** No refresh endpoint in v1 | `api/sessions.py::refresh_session` returns 501 with a clear message. The 5-minute TTL is short enough that the frontend can re-call `POST /workspaces/:id/sessions` cheaply. |
| **4.7** Fly `.internal` DNS depends on machine metadata.label | `services/fly_orchestrator.py::create_machine` sets `metadata.label = wid`. Documented inline + in `backend/README.md`. |

---

## Out of scope (explicitly deferred — matches plan §6)

- **Real agent dispatch** (`POST /workspaces/:id/agents`).
- **Repos API** (`GET /repos`, `POST /repos/import`) — needs GitHub OAuth.
- **Real policy enforcement** — `GET /policy` returns the empty bundle.
- **Quotas / rate limits / billing.**
- **Multi-tenancy beyond the RLS baseline.**
- **Replay protection on `jti`** — broker emits a fresh UUIDv4 per token; daemon doesn't track them yet.
- **WebSocket-aware health checks** on the workspace daemons.
- **Startup-time Alembic-head ≠ DB version check** — relies on `release_command` to gate rollouts in v1.

---

## Next

Per [`scaffolding-plan.md`](../executing/scaffolding-plan.md) §5: **`frontend/`** — the browser IDE. Newly unblocked by Phase 4:

- The browser now has a real `POST /workspaces/:id/sessions` to call.
- The `{daemon_url, token, expires_at}` response shape is fixed; the frontend just opens a WS to `daemon_url?token=…` and starts speaking the envelope protocol.
- The signer/verifier loop is wire-realistic in both directions — no more stub auth.

A few small follow-ups it's worth carrying into Phase 5 (or a tiny PR before it):

1. **Fly deploy smoke** — first `fly deploy` from `backend/`, plus the live `/healthz` curl, plus updating the Fly secret keys (private signing key + Supabase JWKS URL).
2. **Token-refresh endpoint** — once the frontend's hot WS sessions start to outlast 5 minutes, implement `POST /sessions/:id/refresh` for real. Threat model decides whether `jti` tracking comes with it.
3. **Workspace status transitions** — `provisioning → running → stopped`. The schema column is in place; the Fly orchestrator just needs to push transitions back into the DB after `start_machine`/`stop_machine` succeed.

The cross-cutting property earned: **a deployed `workspace-image:<sha>` and a deployed `rommel-backend` are now provably tied** — the backend's signing key matches the pubkey baked at image-build time. Rotation requires both halves to be re-deployed; that's the security primitive Phase 3 Decision 0.2 was designed to enforce, and Phase 4's symmetric `ROMMEL_TOKEN_PRIVKEY` env-var convention closes the loop.
