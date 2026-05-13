# Phase 4 — `backend/` Implementation Plan

Companion to [`scaffolding-plan.md`](./scaffolding-plan.md) §4. Specializes that section into a step-by-step build order for the **`backend/`** subtree: the FastAPI control plane that owns auth, workspace lifecycle, and (most importantly for Pattern B) the session-token broker.

**Status going in:** Phases 0–3 are complete (see [`docs/changelog.md`](../changelog.md)). The proto schema for `SessionTokenClaims` is committed; the daemon verifier already enforces it; the workspace image bakes an EdDSA pubkey at a known SHA. Phase 4 produces the *signer* whose pubkey is already deployed — meaning the first end-to-end token round-trip becomes possible at the end of this phase.

**Definition of "done" for Phase 4:**

1. `make -C backend run` boots Uvicorn locally; `curl localhost:8080/healthz` returns `{"ok": true}`.
2. `make -C backend migrate` applies `0001_init.py` against a local Postgres, creating `users` and `workspaces` with RLS enabled.
3. `GET /auth/me` validates a real Supabase-issued JWT and returns its claims (Supabase JWKS, no shared secret stored).
4. **Integration gate:** `POST /workspaces/:id/sessions` mints an EdDSA-signed JWT whose claims match `proto/schemas/session-token.json`, and a locally running `sandbox-daemon` (with the matching pubkey) accepts it for a `system.ping` round-trip.
5. `fly deploy` from `backend/` puts a live URL up on `rommel-backend.fly.dev`; `.github/workflows/backend.yml` wakes up and goes green on PR.

---

## 0. Decisions to settle before any code lands

Phase 3's open decisions were structural (Dockerfile location, build context). Phase 4's are mostly library/policy calls — each one is reversible per-PR, but cheaper to confirm now than to retrofit after a model layer has shipped.

### 0.1 ORM/driver stack: SQLAlchemy 2.0 async (Core) + asyncpg + Alembic

Scaffolding-plan §4 lists "asyncpg or sqlalchemy" as open. **Recommendation: SQLAlchemy 2.0 Core (not the ORM session) with asyncpg as the driver, and Alembic for migrations.**

- *Core, not ORM*: queries are expressed as `select(workspaces).where(...)` Python expressions. Type-checked, no string SQL in the codebase, no lazy-loading/N+1 footguns. ORM features (identity map, relationship loading) are dead weight for a CRUD control plane.
- *asyncpg under it*: SQLAlchemy 2.0's async support is mature; asyncpg is the fastest Postgres driver in the Python ecosystem.
- *Alembic*: `alembic revision --autogenerate` reads SQLAlchemy metadata and emits migrations. The alternative — bare asyncpg with hand-rolled migrations — works fine but means writing CREATE TABLE statements forever and losing autogenerate's drift detection.

Trade-off considered: **bare asyncpg + raw SQL files + a `schema.sql` snapshot.** Cleaner for a team that hates ORMs entirely, and reasonable here since the row shapes are simple. Rejected because Alembic's migration history + autogenerate buys more than it costs at this scale, and SQLAlchemy 2.0 Core sidesteps the worst ORM ergonomics anyway.

### 0.2 JWT library: PyJWT (≥2.8) with the `cryptography` backend

Scaffolding-plan §4 lists `python-jose[cryptography]`. **Recommendation: PyJWT** instead.

- *Maintenance posture*: python-jose has had stretches of stalled maintenance; PyJWT is actively maintained by the Authlib org's adjacent community and tracks the JOSE spec changes.
- *EdDSA*: PyJWT 2.x supports Ed25519 via the `cryptography` package without extra plumbing — `jwt.encode(claims, priv_key, algorithm="EdDSA")` and the matching `jwt.decode(token, pub_key, algorithms=["EdDSA"])`. The daemon's Go side uses `golang-jwt/jwt/v5` with `WithValidMethods([]string{"EdDSA"})`; PyJWT's `algorithm="EdDSA"` produces a header (`{"alg":"EdDSA", "typ":"JWT"}`) that round-trips cleanly with that.
- *API surface*: smaller, easier to audit. python-jose ships `jwt`, `jws`, `jwk`, `jwe` modules; PyJWT is one module.

Trade-off considered: **Authlib's `jose.jwt`**. Comparable feature-set and arguably best-in-class. Rejected for v1 because Authlib's bigger surface (OAuth/OIDC providers) tempts scope creep — we'd start to use its OAuth bits and bind the auth seam tighter than `validate_jwt(token) → UserClaims` deserves.

### 0.3 EdDSA private key delivery: PEM-string env var, mirroring the daemon's verifier

`ROMMEL_TOKEN_PRIVKEY` holds the PEM contents (not a file path), mirroring `ROMMEL_TOKEN_PUBKEY` on the daemon side (`sandbox-daemon/internal/config/config.go:73` parses PEM contents directly). On Fly: `fly secrets set ROMMEL_TOKEN_PRIVKEY="$(cat priv.pem)" -a rommel-backend`. On local dev: `.env.local` (gitignored).

Rotation: backend rotates private key → new pubkey baked into next `workspace-image:<sha>` → tokens from the new key are accepted only on machines launched from the new image. This is the property Phase 3 Decision 0.2 was designed to enforce; the symmetry on the backend side closes the loop.

Trade-off considered: **mounted-file Fly secret** (`fly secrets set --file priv.pem`). Cleaner ops story (no PEM strings in env-var dumps), but adds a file-path indirection that doesn't match the daemon convention. Documented as the rotation-friendlier upgrade if env-var leaks ever become a concern.

### 0.4 Daemon reachability is config-driven (Fly internal in prod, localhost in dev)

`POST /workspaces/:id/sessions` returns `{daemon_url, token, expires_at}` — but the URL shape differs between environments:

- **Production:** `wss://<wid>.vm.rommel-workspaces.internal:7777/ws?token=…` — using Fly's per-machine `.internal` DNS. The backend resolves machine-id → wid via the database row populated when `fly_orchestrator.create_machine` succeeded.
- **Local dev:** `ws://localhost:7777/ws?token=…` — developer runs `cd sandbox-daemon && make run-local`. The backend doesn't need to know about Fly at all; one env var (`ROMMEL_DAEMON_URL_TEMPLATE`) interpolates `{wid}` and produces the URL string.

This keeps the local Pattern-B loop runnable without a Fly account and without modifying business logic between environments.

### 0.5 Settings: pydantic-settings; lint/format: Ruff

- `pydantic-settings>=2.0` for all env-driven config. One `Settings` class, one `@lru_cache get_settings()` FastAPI dependency, no `os.getenv` calls anywhere else in the codebase. `.env` / `.env.local` files supported for local dev; Fly secrets supplant them in production.
- **Ruff** for lint *and* format (replaces black + flake8 + isort with one tool). Trivial choice but worth pinning so two contributors don't end up running different formatters.

### 0.6 Test database: ephemeral Postgres via Docker Compose, not SQLite/Supabase-shadow

The first migration enables RLS. SQLite can't run RLS at all; Supabase shadow databases need network and per-developer credentials. **Use a local Postgres container** (`compose.yaml` ships one), with `pytest-asyncio` and a `pytest` fixture that runs migrations on a fresh schema per test session.

Trade-off considered: **`testcontainers-python`** (auto-manages the container lifetime in pytest). Reasonable, but adds a heavyweight dep for one container; a hand-rolled compose service is good enough. Documented as the upgrade if test ergonomics start to hurt.

### 0.7 Alembic policy: PR-shipped migrations only; `alembic check` in CI; never autogen on boot

Every PR that changes `models.py` must include a migration in the same PR (CI gate). `alembic upgrade head` runs only in CI / deploy steps, **never at FastAPI startup** — runtime migrations are the classic foot-gun where two replicas race the same migration on boot. The FastAPI app `fail`s if `alembic heads` ≠ DB version at startup, so a missing migration is loud, not silent.

---

## 1. Files to create

```
backend/
├── README.md
├── pyproject.toml                    # poetry; fastapi, uvicorn, pydantic, pydantic-settings,
│                                     #   sqlalchemy[asyncio], asyncpg, alembic, pyjwt[crypto],
│                                     #   httpx, structlog
├── poetry.lock
├── fly.toml                          # app: rommel-backend, public service on :8080
├── Dockerfile                        # python:3.12-slim + uvicorn
├── compose.yaml                      # local Postgres for tests + dev
├── alembic.ini
├── Makefile                          # bootstrap / run / test / lint / migrate / migrate-new / deploy
├── .env.example
├── .gitignore                        # .env.local, .venv/, __pycache__, etc.
├── api/
│   ├── __init__.py
│   ├── main.py                       # FastAPI app factory, lifespan, router includes
│   ├── deps.py                       # get_settings, get_db, get_current_user
│   ├── health.py                     # GET /healthz
│   ├── auth.py                       # GET /auth/me, POST /auth/logout
│   ├── workspaces.py                 # CRUD + start/stop
│   ├── sessions.py                   # POST /workspaces/:id/sessions, POST /sessions/:id/refresh
│   └── policy.py                     # GET /policy stub
├── services/
│   ├── __init__.py
│   ├── auth.py                       # validate_jwt(token) → UserClaims (Supabase JWKS)
│   ├── session_broker.py             # mint_token(user_id, wid, scopes) — EdDSA sign
│   ├── workspace_lifecycle.py        # orchestrates fly_orchestrator + repository writes
│   └── fly_orchestrator.py           # thin httpx client over Fly Machines API
├── repositories/
│   ├── __init__.py
│   ├── base.py                       # typing.Protocol for WorkspaceRepo, UserRepo
│   └── postgres/
│       ├── __init__.py
│       ├── engine.py                 # async engine + session factory
│       ├── workspaces.py
│       └── users.py
├── models/
│   ├── __init__.py
│   └── tables.py                     # SQLAlchemy metadata: users, workspaces
├── policy/
│   ├── __init__.py
│   └── rules.py                      # stub: returns {"version": 0, "rules": []}
├── alembic/
│   ├── env.py                        # reads DATABASE_URL from Settings; uses sync driver
│   ├── script.py.mako
│   └── versions/
│       └── 0001_init.py              # users, workspaces, RLS-enabled, with policies
└── tests/
    ├── conftest.py                   # async test client, postgres fixture, migrated schema
    ├── test_health.py
    ├── test_auth.py
    ├── test_sessions.py              # ← the integration gate (mints, decodes back, asserts shape)
    └── test_workspaces.py
```

---

## 2. Step-by-step implementation

### Step 1 — Subtree skeleton + Settings (PR-1)

`pyproject.toml` with the locked dep set; `Settings` class with `model_config = SettingsConfigDict(env_prefix="ROMMEL_", env_file=".env.local")`; FastAPI app factory with `/healthz`; `Makefile` (`run`, `lint`, `test`, `bootstrap`); `Dockerfile` (python:3.12-slim, `uvicorn api.main:app --host 0.0.0.0 --port 8080`); `compose.yaml` with `postgres:16` for tests.

Settings keys (initial set, document the lot in `.env.example`):

| Key | Required | Notes |
|---|---|---|
| `ROMMEL_DATABASE_URL` | yes | `postgresql+asyncpg://…` for app; sync `postgresql://…` derived for Alembic |
| `ROMMEL_TOKEN_PRIVKEY` | yes | Ed25519 PEM contents (matches daemon's `ROMMEL_TOKEN_PUBKEY` convention) |
| `ROMMEL_DAEMON_URL_TEMPLATE` | yes | e.g. `wss://{wid}.vm.rommel-workspaces.internal:7777/ws` (prod) or `ws://localhost:7777/ws` (dev) |
| `ROMMEL_TOKEN_TTL_SECONDS` | no (default 300) | session-token expiry |
| `ROMMEL_SUPABASE_JWKS_URL` | yes | Supabase project's JWKS endpoint |
| `ROMMEL_FLY_API_TOKEN` | prod-only | Fly Machines API auth |

### Step 2 — Models + Alembic 0001 (PR-2)

`models/tables.py` defines `users` (`id uuid primary key`, `supabase_sub text unique`, `created_at`) and `workspaces` (`id uuid primary key`, `user_id uuid references users(id)`, `name text`, `fly_machine_id text`, `status text`, `created_at`, `updated_at`).

`alembic/env.py` uses the **sync** SQLAlchemy driver (`postgresql://…`) — async drivers cause Alembic to hang on migrations because Alembic's own runner is sync. Derive the sync URL from `ROMMEL_DATABASE_URL` by stripping `+asyncpg`.

`0001_init.py` does five things in order: create tables, create `app_user` Postgres role, `GRANT SELECT, INSERT, UPDATE, DELETE` to it, `ENABLE ROW LEVEL SECURITY` on both tables, install `policies` (`workspaces` rows visible only when `current_setting('rommel.user_id')` matches `user_id::text`). The FastAPI request-scoped DB session sets `SET LOCAL rommel.user_id = '<sub>'` after authenticating — every query in that scope is RLS-bound automatically.

### Step 3 — Auth seam: `validate_jwt(token) → UserClaims` (PR-3)

`services/auth.py`:

```python
async def validate_jwt(token: str) -> UserClaims:
    jwks = await _fetch_jwks_cached()
    header = jwt.get_unverified_header(token)
    key = next(k for k in jwks["keys"] if k["kid"] == header["kid"])
    pub = jwt.algorithms.RSAAlgorithm.from_jwk(key)
    claims = jwt.decode(token, pub, algorithms=["RS256"], audience="authenticated")
    return UserClaims(sub=claims["sub"], email=claims.get("email"))
```

`api/deps.py::get_current_user` calls it; `GET /auth/me` returns the claims. One function = the auth seam. A future swap to Clerk/Auth.js means rewriting this function and nothing else.

JWKS caching: `cachetools.TTLCache` for ~10 minutes. Supabase rotates keys infrequently; a fixed TTL is fine.

### Step 4 — Session broker (PR-4) — the integration gate

`services/session_broker.py`:

```python
def mint_token(*, user_id: str, wid: str, scopes: list[str], settings: Settings) -> tuple[str, datetime]:
    now = datetime.now(tz=UTC)
    exp = now + timedelta(seconds=settings.token_ttl_seconds)
    claims = {
        "iss": "rommel-backend",
        "sub": user_id,
        "aud": "rommel-daemon",
        "wid": wid,
        "scope": scopes,
        "iat": int(now.timestamp()),
        "exp": int(exp.timestamp()),
        "jti": str(uuid.uuid4()),
    }
    token = jwt.encode(claims, settings.token_privkey, algorithm="EdDSA")
    return token, exp
```

`api/sessions.py::create_session` calls it after authorizing the user against the workspace, then returns `{daemon_url: settings.daemon_url_template.format(wid=wid), token, expires_at}`.

**The Phase-4 integration gate runs here:** `tests/test_sessions.py` boots the actual `sandbox-daemon` binary as a subprocess (with the matching pubkey in env), calls `POST /workspaces/<wid>/sessions`, opens a WS to the returned `daemon_url`, sends `system.ping`, expects `system.ping` response. The same loop the frontend will execute in Phase 5.

### Step 5 — Fly orchestrator skeleton (PR-5)

`services/fly_orchestrator.py` — thin httpx client over the Fly Machines API. Methods: `create_machine(image, wid, region) → machine_id`, `start_machine(machine_id)`, `stop_machine(machine_id)`, `destroy_machine(machine_id)`. Pass `ROMMEL_TOKEN_PUBKEY` is **not** done here — Phase 3 baked it into the image; `create_machine` only injects `ROMMEL_WID` per machine.

Workspace CRUD (`api/workspaces.py`) wires this in: `POST /workspaces` calls `create_machine`, persists the workspace row with the returned `fly_machine_id`. Real implementation can wait — for v1 scaffolding, returning a stub `{ok: true, id: "<uuid>"}` is enough to unblock Phase 5.

### Step 6 — Dockerfile + fly.toml + first deploy (PR-6)

`Dockerfile`: python:3.12-slim, COPY pyproject + poetry.lock, install with `poetry install --only main`, COPY source, run `uvicorn api.main:app --host 0.0.0.0 --port 8080`. Multi-stage isn't needed — Poetry's lockfile + slim base is already lean.

`fly.toml`: app `rommel-backend`, `internal_port = 8080`, single `[[services]]` block with `force_https = true`, `[[services.http_checks]]` against `/healthz`. Shared CPU 1x, 256MB RAM is plenty for a control plane.

Migrations run as a `[deploy]` release-command: `release_command = "alembic upgrade head"`. One-shot machine, blocks the rollout until clean.

### Step 7 — Wake up `backend.yml` CI

The workflow already exists from Phase 0 and gates on `backend/pyproject.toml`. Once Step 1 lands, it triggers. Add a Postgres service container to the workflow (`services:` block in the YAML) and run `pytest` against it. Path-filters already cover `backend/**`.

---

## 3. Verification recipe

### 3.1 Local boot

```sh
cd backend
docker compose up -d postgres
poetry install
make migrate                              # alembic upgrade head
make run                                  # uvicorn, :8080
curl -fsS localhost:8080/healthz          # → {"ok": true}
```

### 3.2 Auth flow

```sh
# Obtain a Supabase JWT via the project's auth flow (any browser sign-in
# against the dev Supabase project), copy the access_token. Then:
curl -fsS -H "Authorization: Bearer $JWT" localhost:8080/auth/me
# → {"sub": "<uuid>", "email": "you@example.com"}
```

### 3.3 The integration gate — backend signs, daemon accepts

```sh
# Terminal 1: daemon with the matching pubkey
openssl genpkey -algorithm ed25519 -out /tmp/dev.pem
openssl pkey -in /tmp/dev.pem -pubout -out /tmp/dev.pub
ROMMEL_TOKEN_PUBKEY="$(cat /tmp/dev.pub)" \
ROMMEL_WORKSPACE_ROOT="$PWD" \
ROMMEL_WID="dev-workspace" \
make -C sandbox-daemon run-local

# Terminal 2: backend with the matching private key
ROMMEL_TOKEN_PRIVKEY="$(cat /tmp/dev.pem)" \
ROMMEL_DAEMON_URL_TEMPLATE="ws://localhost:7777/ws" \
make -C backend run

# Terminal 3: full round-trip
JWT=$(curl -s -X POST -H "Authorization: Bearer $SUPABASE_JWT" \
       "localhost:8080/workspaces/dev-workspace/sessions" | jq -r '.token')
echo '{"kind":"request","type":"system.ping","id":"01"}' | \
  websocat "ws://localhost:7777/ws?token=$JWT"
# → {"kind":"response","type":"system.ping","id":"01","payload":{"ok":true,"ts":"…"}}
```

This is the load-bearing gate. If it works, Phase 4 has functionally landed.

### 3.4 Migration applies on a fresh DB

```sh
docker compose down -v && docker compose up -d postgres
make migrate                              # 0001 applies cleanly
psql "$ROMMEL_DATABASE_URL" -c '\d workspaces'   # → table exists, RLS enabled
```

### 3.5 Fly deploy

```sh
fly auth login
fly apps create rommel-backend            # one-time
fly secrets set ROMMEL_TOKEN_PRIVKEY="$(cat priv.pem)" \
                ROMMEL_DATABASE_URL="$SUPABASE_URL" \
                ROMMEL_SUPABASE_JWKS_URL="$SUPABASE_JWKS" \
                ROMMEL_DAEMON_URL_TEMPLATE="wss://{wid}.vm.rommel-workspaces.internal:7777/ws"
fly deploy
curl -fsS https://rommel-backend.fly.dev/healthz
```

---

## 4. Risks and gotchas

### 4.1 Alembic + async drivers

Alembic's runner is synchronous. Using `postgresql+asyncpg://…` as the migration URL hangs or errors. **Fix:** `alembic/env.py` strips the `+asyncpg` to derive a sync `postgresql://…` URL. Use `psycopg[binary]` (psycopg 3) under the sync driver — same dialect, sync API.

### 4.2 RLS local-dev: two Postgres roles

The Alembic migration runs as a privileged role (the migration creates RLS policies); the FastAPI app connects as `app_user`, which is RLS-bound. If both use the same role, RLS gets bypassed because Postgres exempts table owners by default. Two `DATABASE_URL`s — `ROMMEL_DATABASE_URL` for the app, `ROMMEL_DATABASE_MIGRATE_URL` for Alembic — keep them separate.

### 4.3 EdDSA in PyJWT requires the `cryptography` extra

`pip install pyjwt[crypto]`, not just `pyjwt`. Without it, `jwt.encode(..., algorithm="EdDSA")` raises `NotImplementedError`. The `pyproject.toml` declares the extra explicitly.

### 4.4 Pydantic v2 strict mode vs Supabase JWT claim shape

Supabase issues JWTs with extra claims (`app_metadata`, `user_metadata`, `role`, `aud="authenticated"`). The generated `proto/clients/python/gen` Pydantic models for *our own* tokens use `additionalProperties: false` (strict). Don't reuse those models for Supabase JWT parsing — define a separate `UserClaims` dataclass in `services/auth.py` for inbound Supabase tokens. Phase-1 schemas are for our wire, not third-party tokens.

### 4.5 JWT clock skew between backend and daemon

PyJWT defaults to no leeway; the daemon's golang-jwt also enforces strict `exp`. If backend and daemon machines drift by even a second, a 5-minute token can be born already-expired. **Fix:** PyJWT `jwt.encode` adds `iat` from a single `datetime.now(UTC)` call (don't compute `iat` and `exp` from separate `now()` calls); daemon validates `exp > now()` only (no `iat > now()` future-dating check). Document the convention in `services/session_broker.py`.

### 4.6 Token TTL without a refresh endpoint in v1

The schema reserves `POST /sessions/:id/refresh` but Phase 4 doesn't implement it — long-lived WS sessions will hit token expiry mid-session. **Mitigation for v1:** TTL is 5 minutes; the frontend (Phase 5) just calls `POST /workspaces/:id/sessions` again and re-opens the WS. Real refresh is a Phase-N polish item.

### 4.7 Fly internal DNS string in the daemon URL template

`wss://<wid>.vm.rommel-workspaces.internal:7777/ws` works only because Fly's `.internal` DNS resolves machine-IDs *and* user-specified labels. The `<wid>` placeholder requires the backend to set the Fly machine's metadata label to the wid at `create_machine` time. Document this in `services/fly_orchestrator.py`. Wrong DNS shape is a silent failure (browser sees "connection refused"), not a loud one.

---

## 5. Sequencing (suggested)

A reasonable per-PR carve-up — each independently revertable, each leaves CI green:

1. **PR-1 — Skeleton + Settings + `/healthz`.** Boots locally, CI wakes up.
2. **PR-2 — SQLAlchemy + Alembic + `0001_init.py`.** `make migrate` works on a fresh local Postgres; CI's `alembic check` step lands here.
3. **PR-3 — Auth seam + `GET /auth/me`.** First test that needs a real Supabase JWT — `conftest.py` mints a test JWT signed by a per-test ephemeral keypair to bypass Supabase entirely in CI.
4. **PR-4 — Session broker + `POST /workspaces/:id/sessions`.** **The integration gate.** A pytest fixture spawns the daemon binary; the test signs a token and asserts the daemon accepts it.
5. **PR-5 — Fly orchestrator skeleton + workspace CRUD stubs.** No real Fly call yet — just the method signatures and the workspace DB rows. PR includes a smoke test that the orchestrator's interface matches what `services/workspace_lifecycle.py` expects.
6. **PR-6 — Dockerfile + `fly.toml` + first deploy.** Includes the live-URL `curl` in the PR description as the gate.

PR-4 is the "Phase 4 functionally complete" milestone. PR-5 and PR-6 are about deployability; the contract that unblocks Phase 5 (frontend) lands in PR-4.

---

## 6. Out of scope (deferred to later phases)

- **Real agent dispatch.** `POST /workspaces/:id/agents` and friends are Layer-3/Hermes territory.
- **Repos API.** `GET /repos`, `POST /repos/import` — needs GitHub OAuth, a non-trivial chunk; defer to Phase 5+.
- **Real policy enforcement.** `GET /policy` returns an empty bundle; `PUT /policy` is unimplemented. The policy *pipeline* lands later when there's a rule to enforce.
- **Quotas / rate limits / billing.** Out of scope for scaffolding.
- **Multi-tenancy beyond the RLS baseline.** Org accounts, sharing, team RBAC — all later.
- **Replay protection on `jti`.** Schema reserves it; backend includes a fresh UUIDv4 per token; daemon doesn't track them yet. Wire up an LRU once the threat model demands it.
- **WebSocket-aware health checks.** The daemon already has `/healthz`; the backend doesn't yet probe its workspace daemons for liveness. Phase-N reliability work.

---

## 7. Completion doc target

When Phase 4 lands, write `docs/completions/phase-4-backend.md` mirroring the structure of `phase-3-workspace-image.md`:

- **What was built** — file tree + summary.
- **Decisions made** — every 0.X above, marked confirmed/revised.
- **Verification** — copy of §3 with the actual integration-gate transcript (the `websocat` round-trip showing the daemon accepting a token the backend just signed).
- **Cross-cutting** — note that the full Pattern B token-signer ↔ verifier loop is now operational; the frontend (Phase 5) is the only missing piece for end-to-end browser-to-daemon ADE sessions.
- **What's next** — `frontend/` per scaffolding-plan §5.

Update `docs/changelog.md` with the `0.1.4` entry pointing at the completion doc.
