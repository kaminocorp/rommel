"""Application settings, sourced from environment variables.

One Settings class, one `@lru_cache get_settings()` FastAPI dependency, no
`os.getenv` calls anywhere else in the codebase. `.env.local` is supported for
local dev; Fly secrets supplant it in production.

Env prefix is `ROMMEL_` — same convention as the daemon (`sandbox-daemon/internal/config/config.go`).
"""

from __future__ import annotations

from functools import lru_cache

from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

# Strip the SQLAlchemy `+asyncpg` driver tag for Alembic, which runs sync.
# Risk 4.1: Alembic hangs on async drivers; the migrate URL is sync by design.
_ASYNCPG_TAG = "+asyncpg"


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="ROMMEL_",
        env_file=(".env", ".env.local"),
        env_file_encoding="utf-8",
        extra="ignore",
    )

    # --- database -----------------------------------------------------------
    database_url: str = Field(
        description="Async URL the FastAPI app uses (postgresql+asyncpg://...). "
        "The connecting role MUST be RLS-bound (e.g. `app_user`); see risk 4.2."
    )
    database_migrate_url: str | None = Field(
        default=None,
        description="Sync URL Alembic uses. Defaults to database_url with the "
        "asyncpg tag stripped — but the connecting role should be the schema "
        "owner (privileged), not `app_user`, so set this explicitly in prod.",
    )

    # --- session-token signer ----------------------------------------------
    token_privkey: str = Field(
        description="Ed25519 PEM contents (NOT a file path). Mirrors the "
        "daemon's ROMMEL_TOKEN_PUBKEY convention so rotation is symmetric."
    )
    token_ttl_seconds: int = Field(
        default=300,
        ge=30,
        le=3600,
        description="Session-token expiry. Daemon enforces `exp` strictly; "
        "no clock-skew leeway (risk 4.5).",
    )

    # --- daemon reachability ----------------------------------------------
    daemon_url_template: str = Field(
        description="WS URL template with a `{wid}` placeholder. Prod: "
        "wss://{wid}.vm.rommel-workspaces.internal:7777/ws — dev: "
        "ws://localhost:7777/ws."
    )

    # --- inbound auth (Supabase) -------------------------------------------
    supabase_jwks_url: str = Field(
        description="Supabase project's JWKS endpoint. Used by the auth seam "
        "to validate inbound user JWTs (RS256, kid-matched)."
    )
    supabase_jwt_audience: str = Field(
        default="authenticated",
        description="Supabase issues tokens with aud=authenticated.",
    )

    # --- Fly Machines orchestrator ----------------------------------------
    fly_api_token: str = Field(
        default="",
        description="Fly Machines API token. Empty in dev — the orchestrator "
        "stubs in that mode.",
    )
    fly_workspaces_app: str = Field(
        default="rommel-workspaces",
        description="Fly app name workspace machines are spawned under.",
    )
    workspace_image: str = Field(
        default="registry.fly.io/rommel-workspaces:latest",
        description="Workspace image ref the orchestrator pins per machine.",
    )

    # --- session capability defaults --------------------------------------
    default_scopes: list[str] = Field(
        default_factory=lambda: ["fs:rw", "pty:rw", "git:rw", "funnel:rw", "policy:r"],
        description="Scope vocabulary baked into freshly-minted session tokens. "
        "Daemon enforces per-primitive — this is just the v1 default bundle.",
    )

    # --- CORS ---------------------------------------------------------------
    cors_origins: list[str] = Field(
        default_factory=lambda: ["*"],
        description="Allowed CORS origins for the API. Default is '*' for dev; "
        "production must set this to the exact frontend origin(s) (e.g. "
        "'https://rommel.dev'). Comma-separated values are accepted from the "
        "environment via ROMMEL_CORS_ORIGINS.",
    )

    @field_validator("default_scopes", "cors_origins", mode="before")
    @classmethod
    def _split_csv(cls, v: object) -> object:
        # Allow `ROMMEL_DEFAULT_SCOPES=fs:rw,pty:rw` or
        # `ROMMEL_CORS_ORIGINS=https://a,https://b` (env-friendly) in addition
        # to a real list (e.g. from a model_copy override in tests).
        if isinstance(v, str):
            return [s.strip() for s in v.split(",") if s.strip()]
        return v

    @property
    def alembic_url(self) -> str:
        """Sync URL Alembic should use, derived from database_migrate_url or
        database_url. The `+asyncpg` tag is stripped because Alembic's runner
        is synchronous (risk 4.1)."""
        url = self.database_migrate_url or self.database_url
        return url.replace(_ASYNCPG_TAG, "")


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    """FastAPI dependency. Cached so the env is parsed once per process; tests
    override it via `app.dependency_overrides[get_settings] = ...`."""
    return Settings()  # type: ignore[call-arg]
