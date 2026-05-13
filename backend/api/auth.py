"""Inbound-auth routes.

Just two endpoints:
  GET  /auth/me       — echo the validated UserClaims (smoke-tests the seam)
  POST /auth/logout   — no-op in v1 (Supabase manages refresh tokens
                        client-side); kept for symmetry with the schema.
"""

from __future__ import annotations

from fastapi import APIRouter, status

from .deps import UserClaimsDep
from services.auth import UserClaims

router = APIRouter(tags=["auth"])


@router.get("/me", response_model=UserClaims)
async def me(user: UserClaimsDep) -> UserClaims:
    return user


@router.post("/logout", status_code=status.HTTP_204_NO_CONTENT)
async def logout(_user: UserClaimsDep) -> None:
    # No server-side session to invalidate in v1 — Supabase tokens are
    # self-contained JWTs with their own `exp`. This endpoint exists so the
    # frontend has a canonical "sign-out" callsite for future bookkeeping
    # (audit log, refresh-token revocation, etc.).
    return None
