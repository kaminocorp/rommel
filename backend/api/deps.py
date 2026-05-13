"""FastAPI dependency wiring.

Three injectable seams, each used per-request:
  - get_settings  — process-cached Settings (overridden in tests)
  - get_db        — async SQLAlchemy session (no RLS context bound)
  - get_db_for_user — same, but pinned to `rommel.user_id = <sub>` so the
                    RLS policies in `0001_init.py` apply
  - get_current_user — Bearer-token → UserClaims via the Supabase JWKS path
"""

from __future__ import annotations

from typing import Annotated, AsyncIterator

from fastapi import Depends, Header, HTTPException, status
from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncSession

from api.config import Settings, get_settings  # noqa: F401  re-export for tests
from repositories.postgres.engine import get_session_factory
from services.auth import UserClaims, validate_jwt

SettingsDep = Annotated[Settings, Depends(get_settings)]


async def get_current_user(
    settings: SettingsDep,
    authorization: Annotated[str | None, Header()] = None,
) -> UserClaims:
    """Validates the `Authorization: Bearer <jwt>` header against Supabase
    JWKS and returns the typed `UserClaims`. Raises 401 on any failure.
    """
    if not authorization or not authorization.lower().startswith("bearer "):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="missing bearer token",
            headers={"WWW-Authenticate": "Bearer"},
        )
    token = authorization.split(" ", 1)[1].strip()
    try:
        return await validate_jwt(token, settings=settings)
    except Exception as exc:  # noqa: BLE001  intentional broad catch at boundary
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail=f"invalid token: {exc}",
            headers={"WWW-Authenticate": "Bearer"},
        ) from exc


UserClaimsDep = Annotated[UserClaims, Depends(get_current_user)]


async def get_db(settings: SettingsDep) -> AsyncIterator[AsyncSession]:
    """Yields an AsyncSession without binding RLS context.

    Use this for routes that don't need RLS (e.g. /healthz, /auth/me's read
    of the user's own row keyed by `sub`). Routes that hit `workspaces` use
    `get_db_for_user` so RLS scopes the query to the caller's rows.
    """
    factory = get_session_factory(settings.database_url)
    async with factory() as session:
        yield session


async def get_db_for_user(
    settings: SettingsDep,
    user: UserClaimsDep,
) -> AsyncIterator[AsyncSession]:
    """Variant of `get_db` that binds RLS context to the current user.

    `SET LOCAL` is transaction-scoped; wrapping the whole request in a
    `session.begin()` block means every query the route makes inherits the
    pinned `rommel.user_id` — and a request that doesn't commit will revert
    the GUC, so per-request isolation is automatic even under connection
    pooling.
    """
    factory = get_session_factory(settings.database_url)
    async with factory() as session:
        async with session.begin():
            await session.execute(
                text("SET LOCAL rommel.user_id = :uid"),
                {"uid": user.sub},
            )
            yield session


DBDep = Annotated[AsyncSession, Depends(get_db)]
RLSDBDep = Annotated[AsyncSession, Depends(get_db_for_user)]
