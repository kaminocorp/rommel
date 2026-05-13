"""Auth seam tests.

The `get_current_user` dep is overridden in conftest to return a fake
UserClaims; this test confirms the route plumbing returns the claims shape.
A separate unit test (`test_validate_jwt_rejects_*`) exercises the underlying
`services.auth.validate_jwt` directly against a hand-rolled RS256 token plus
a hand-rolled JWKS, so we can prove the JWKS path works without hitting
Supabase.
"""

from __future__ import annotations

import json
import time
from typing import Any

import jwt
import pytest
from cachetools import TTLCache
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa


@pytest.mark.asyncio
async def test_get_auth_me_returns_user_claims(client):
    resp = await client.get("/auth/me")
    assert resp.status_code == 200
    body = resp.json()
    assert body["sub"] == "test-user-sub"
    assert body["email"] == "test@example.com"


@pytest.mark.asyncio
async def test_get_auth_me_without_bearer_is_401():
    # Bypass the overridden app — build a fresh one without the auth override
    # so the real `get_current_user` runs.
    from api.main import create_app
    from httpx import ASGITransport, AsyncClient

    application = create_app()
    transport = ASGITransport(app=application)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        resp = await ac.get("/auth/me")
    assert resp.status_code == 401


@pytest.mark.asyncio
async def test_validate_jwt_happy_path(monkeypatch, test_settings):
    """Hermetic happy-path for the auth seam: generate an RSA keypair, build a
    one-key JWKS, monkey-patch the TTLCache, sign a token, validate."""
    from services import auth as auth_mod

    priv = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    pub_pem = priv.public_key().public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )

    kid = "test-kid"
    jwks = {
        "keys": [
            json.loads(jwt.algorithms.RSAAlgorithm.to_jwk(priv.public_key()))
            | {"kid": kid, "alg": "RS256", "use": "sig"}
        ]
    }
    # Inject the JWKS directly into the cache so no HTTP call is made.
    fake_cache: TTLCache[str, dict[str, Any]] = TTLCache(maxsize=1, ttl=600)
    fake_cache[test_settings.supabase_jwks_url] = jwks
    monkeypatch.setattr(auth_mod, "_JWKS_CACHE", fake_cache)

    priv_pem = priv.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    now = int(time.time())
    token = jwt.encode(
        {
            "sub": "abc-123",
            "email": "u@example.com",
            "aud": test_settings.supabase_jwt_audience,
            "iat": now,
            "exp": now + 60,
        },
        priv_pem,
        algorithm="RS256",
        headers={"kid": kid},
    )

    claims = await auth_mod.validate_jwt(token, settings=test_settings)
    assert claims.sub == "abc-123"
    assert claims.email == "u@example.com"


@pytest.mark.asyncio
async def test_validate_jwt_rejects_expired(monkeypatch, test_settings):
    from services import auth as auth_mod

    priv = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    kid = "test-kid"
    jwks = {
        "keys": [
            json.loads(jwt.algorithms.RSAAlgorithm.to_jwk(priv.public_key()))
            | {"kid": kid, "alg": "RS256", "use": "sig"}
        ]
    }
    fake_cache: TTLCache[str, dict[str, Any]] = TTLCache(maxsize=1, ttl=600)
    fake_cache[test_settings.supabase_jwks_url] = jwks
    monkeypatch.setattr(auth_mod, "_JWKS_CACHE", fake_cache)

    priv_pem = priv.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    now = int(time.time())
    token = jwt.encode(
        {
            "sub": "abc-123",
            "aud": test_settings.supabase_jwt_audience,
            "iat": now - 120,
            "exp": now - 60,
        },
        priv_pem,
        algorithm="RS256",
        headers={"kid": kid},
    )

    with pytest.raises(jwt.ExpiredSignatureError):
        await auth_mod.validate_jwt(token, settings=test_settings)
