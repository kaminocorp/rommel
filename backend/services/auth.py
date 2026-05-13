"""Inbound-auth seam: Supabase JWT → typed UserClaims.

The whole auth coupling to Supabase lives in this module. Swapping to Clerk
/ Auth.js / a homegrown IdP means rewriting `validate_jwt` and nothing else.

Notes:
  - Supabase issues RS256-signed JWTs with `kid` in the header pointing into
    its rotating JWKS. Cache the JWKS for ~10 minutes (risk doc §3 step 3).
  - The Supabase payload carries `app_metadata`, `user_metadata`, `role`,
    etc. — we deliberately don't re-use `proto/clients/python/gen` models for
    this (risk 4.4): those are strict (`additionalProperties: false`) for our
    own wire, and would reject the extra claims.
"""

from __future__ import annotations

import time
from typing import TYPE_CHECKING, Any

import httpx
import jwt
from cachetools import TTLCache
from pydantic import BaseModel

if TYPE_CHECKING:
    from api.config import Settings


class UserClaims(BaseModel):
    sub: str
    email: str | None = None


_JWKS_CACHE: TTLCache[str, dict[str, Any]] = TTLCache(maxsize=4, ttl=600)
_JWKS_LOCK_KEY = "__inflight__"  # cheap single-flight marker


async def _fetch_jwks(url: str) -> dict[str, Any]:
    cached = _JWKS_CACHE.get(url)
    if cached is not None:
        return cached
    async with httpx.AsyncClient(timeout=5.0) as client:
        resp = await client.get(url)
        resp.raise_for_status()
        jwks = resp.json()
    _JWKS_CACHE[url] = jwks
    return jwks


def _key_for_kid(jwks: dict[str, Any], kid: str) -> Any:
    for k in jwks.get("keys", []):
        if k.get("kid") == kid:
            return jwt.algorithms.RSAAlgorithm.from_jwk(k)
    raise ValueError(f"no JWKS key matches kid={kid!r}")


async def validate_jwt(token: str, *, settings: "Settings") -> UserClaims:
    """Verify `token` against Supabase JWKS and return its claims.

    On any failure (network, signature, claim mismatch, expiry) raises an
    exception — the route handler in `api.deps.get_current_user` translates
    that into a 401.
    """
    header = jwt.get_unverified_header(token)
    kid = header.get("kid")
    if not kid:
        raise ValueError("token header missing 'kid'")

    jwks = await _fetch_jwks(settings.supabase_jwks_url)
    try:
        key = _key_for_kid(jwks, kid)
    except ValueError:
        # Key may have rotated since we cached the JWKS. Invalidate and retry
        # exactly once before giving up — keeps us out of the "every request
        # re-fetches JWKS for 10 minutes after rotation" failure mode.
        _JWKS_CACHE.pop(settings.supabase_jwks_url, None)
        jwks = await _fetch_jwks(settings.supabase_jwks_url)
        key = _key_for_kid(jwks, kid)

    claims = jwt.decode(
        token,
        key,
        algorithms=["RS256"],
        audience=settings.supabase_jwt_audience,
        # No leeway. Risk 4.5: we keep the symmetry with the daemon (which also
        # enforces strict `exp`) by not allowing clock skew here either.
        options={"require": ["exp", "iat", "sub"]},
    )
    # Trust-but-narrow: only surface the two claims this app needs.
    return UserClaims(sub=str(claims["sub"]), email=claims.get("email"))


# Test-only helper. `tests/conftest.py` patches the auth seam by overriding
# `get_current_user` directly, but having a clean way to mint and validate a
# locally-keyed RS256 token keeps the unit test for this module hermetic.
def _now() -> int:
    return int(time.time())
