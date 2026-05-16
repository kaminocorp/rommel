# rommel-backend

The Rommel ADE control plane: FastAPI app that owns auth, workspace lifecycle,
and (the load-bearing piece) the EdDSA session-token broker the workspace
daemon validates.

| | |
|---|---|
| **Fly app** | `rommel-backend` |
| **Image** | built from `backend/Dockerfile` |
| **Public URL** | `https://rommel-backend.fly.dev` |
| **Database** | Postgres (Supabase in prod, `compose.yaml` locally) |
| **Signed-by** | EdDSA private key in Fly secret `ROMMEL_TOKEN_PRIVKEY` |
| **Verified-by** | EdDSA pubkey baked into `workspace-image` at `/etc/rommel/token.pubkey` |

The signer/verifier pair is the entire Pattern-B auth loop: the backend mints
a short-lived JWT, the browser opens a WS straight to the workspace daemon at
`wss://{wid}.workspaces.rommel.dev/ws?token=...` (Phase 0 Flycast public mapping)
or the internal `.internal` form in dev, and the daemon
either accepts or rejects without consulting the backend.

## Layout

```
api/                    FastAPI routes + dependency wiring
  config.py             Settings (pydantic-settings, env_prefix=ROMMEL_)
  deps.py               get_db / get_db_for_user / get_current_user
  main.py               app factory; routers; lifespan
  health.py             GET /healthz
  auth.py               GET /auth/me, POST /auth/logout
  workspaces.py         /workspaces CRUD
  sessions.py           POST /workspaces/:id/sessions, POST /sessions/:id/refresh (501)
  policy.py             GET /policy (stub)
services/               Business logic
  auth.py               validate_jwt(token, settings) → UserClaims (Supabase JWKS)
  session_broker.py     mint_token(user_id, wid, scopes, settings) — EdDSA signer
  workspace_lifecycle.py  orchestrates fly_orchestrator + repository writes
  fly_orchestrator.py   thin httpx client over the Fly Machines API
repositories/postgres/  SQLAlchemy 2.0 Core; one repo per table
models/tables.py        Single source of truth for table shapes
alembic/                Schema migrations (sync driver — see risk 4.1)
policy/rules.py         Returns the empty bundle for v1
tests/                  pytest-asyncio + ASGI test client; daemon-subprocess fixture
```

## Local dev

```sh
# 1. Postgres (RLS needs real Postgres; SQLite can't)
docker compose up -d postgres

# 2. Python deps
make bootstrap                          # poetry install

# 3. Generate a dev keypair and point both halves at it
openssl genpkey -algorithm ed25519 -out /tmp/dev.pem
openssl pkey    -in /tmp/dev.pem -pubout -out /tmp/dev.pub
export ROMMEL_TOKEN_PRIVKEY="$(cat /tmp/dev.pem)"

# 4. Migrate the schema
make migrate

# 5. Boot uvicorn
make run                                # → http://localhost:8080
curl -fsS localhost:8080/healthz        # → {"ok": true}
```

For the full end-to-end loop (backend signs, daemon verifies, ping
round-trips) see `tests/test_sessions.py::test_broker_signs_token_daemon_accepts`
— or the recipe in `docs/executing/phase-4-backend-plan.md` §3.3.

## Tests

```sh
# Smoke + unit: no external deps, runs in seconds.
make test

# Integration gate: builds the daemon binary, spawns it, opens a WS, pings.
# Skipped automatically if the Go toolchain isn't available.
make test -- tests/test_sessions.py::test_broker_signs_token_daemon_accepts

# DB-bound tests skip if Postgres isn't reachable on localhost:5432.
```

## Deploy

```sh
fly auth login
fly apps create rommel-backend                    # one-time
fly secrets set \
  ROMMEL_TOKEN_PRIVKEY="$(cat priv.pem)" \
  ROMMEL_DATABASE_URL="$SUPABASE_URL" \
  ROMMEL_DATABASE_MIGRATE_URL="$SUPABASE_MIGRATE_URL" \
  ROMMEL_SUPABASE_JWKS_URL="$SUPABASE_JWKS" \
  ROMMEL_DAEMON_URL_TEMPLATE="wss://{wid}.workspaces.rommel.dev/ws" \   # Phase 0 Flycast shape
  ROMMEL_FLY_API_TOKEN="$FLY_API_TOKEN"

fly deploy                                        # release_command runs `alembic upgrade head`
curl -fsS https://rommel-backend.fly.dev/healthz
```

**Key rotation** (intentionally re-deploy-coupled, per Decision 0.3): generate
a new keypair → `fly secrets set ROMMEL_TOKEN_PRIVKEY=...` on the backend →
rebuild & redeploy `workspace-image` with the new pubkey baked in. Until both
sides are deployed, tokens signed by the new key are accepted only on
machines launched from the new image. This is the property Phase 3
Decision 0.2 was designed to enforce.

## Risks the code intentionally guards against

- **Alembic + async drivers** (4.1) — `alembic/env.py` strips `+asyncpg` to a
  sync URL. Async drivers hang Alembic's sync runner.
- **RLS bypass via shared role** (4.2) — `ROMMEL_DATABASE_URL` uses `app_user`
  (RLS-bound); `ROMMEL_DATABASE_MIGRATE_URL` uses the privileged owner.
  `FORCE ROW LEVEL SECURITY` is enabled in `0001_init.py` as defense in depth.
- **EdDSA crypto extra** (4.3) — `pyproject.toml` declares `PyJWT[crypto]`.
- **Pydantic strictness vs Supabase claims** (4.4) — `services/auth.py` uses
  its own `UserClaims`, *not* the generated proto Pydantic model (which is
  `additionalProperties: false`).
- **Clock skew between backend signer and daemon verifier** (4.5) —
  `session_broker.mint_token` derives `iat` and `exp` from a single `now()`.
- **Token TTL with no refresh endpoint in v1** (4.6) — frontend re-calls
  `POST /workspaces/:id/sessions` and re-opens the WS.
- **Fly `.internal` DNS label** (4.7) — `fly_orchestrator.create_machine`
  sets `metadata.label = wid` so `<wid>.vm.<app>.internal` resolves.
