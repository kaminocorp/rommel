"""Phase-4 integration gate.

Two tests:
  - `test_broker_signs_token_daemon_accepts` — the load-bearing gate. Spawns
    the real sandbox-daemon binary, mints a token via the broker, opens a WS
    to the daemon, sends `system.ping`, expects the matching response.
  - `test_broker_claim_shape_matches_proto_schema` — pure-Python check that
    the claim bag the broker produces is structurally identical to the
    `proto/schemas/session-token.json` schema (no daemon dependency).

The integration gate is the property the whole phase exists to prove. If
both tests pass, Phase 4 is functionally complete.
"""

from __future__ import annotations

import json
import uuid

import jwt
import pytest
import websockets
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from services.session_broker import mint_token


@pytest.mark.asyncio
async def test_broker_signs_token_daemon_accepts(daemon_subprocess, test_settings):
    """Backend signs → daemon verifies → ping round-trips. The gate."""
    wid = daemon_subprocess["wid"]
    ws_url = daemon_subprocess["ws_url"]

    token, _exp = mint_token(
        user_id="integration-test-user",
        wid=wid,
        scopes=["fs:rw", "pty:rw"],
        settings=test_settings,
    )

    async with websockets.connect(f"{ws_url}?token={token}") as ws:
        request_id = str(uuid.uuid4())
        await ws.send(
            json.dumps(
                {
                    "kind": "request",
                    "type": "system.ping",
                    "id": request_id,
                    "payload": {},
                }
            )
        )
        raw = await ws.recv()

    frame = json.loads(raw)
    assert frame["kind"] == "response", frame
    assert frame["type"] == "system.ping"
    assert frame["id"] == request_id
    assert frame["payload"]["ok"] is True
    assert "ts" in frame["payload"]


@pytest.mark.asyncio
async def test_daemon_rejects_token_with_wrong_wid(daemon_subprocess, test_settings):
    """Sanity-check on the reverse: a token minted for a different wid is
    rejected at the WS upgrade. Mirrors `TestVerify_RejectsWrongWid` on the
    daemon side."""
    ws_url = daemon_subprocess["ws_url"]
    bad_token, _ = mint_token(
        user_id="integration-test-user",
        wid="not-the-daemons-wid",
        scopes=["fs:rw"],
        settings=test_settings,
    )

    # The daemon writes 401 on the upgrade; websockets raises InvalidStatusCode.
    with pytest.raises(Exception) as ei:  # noqa: PT011 — library raises a few different shapes
        async with websockets.connect(f"{ws_url}?token={bad_token}"):
            pass
    msg = str(ei.value)
    assert "401" in msg or "invalid" in msg.lower(), f"unexpected error: {msg!r}"


def test_broker_claim_shape_matches_proto_schema(test_settings):
    """The claim bag the broker emits must match `session-token.json`. This
    test is hermetic — no daemon, no HTTP — so it catches schema drift even
    when the integration gate is skipped (e.g. no Go toolchain)."""
    token, exp = mint_token(
        user_id="abc-123",
        wid="my-workspace",
        scopes=["fs:rw", "pty:rw", "git:r"],
        settings=test_settings,
    )

    # Decode without verifying signature; we're inspecting the payload shape.
    claims = jwt.decode(token, options={"verify_signature": False})

    assert set(claims.keys()) == {"iss", "sub", "aud", "wid", "scope", "exp", "iat", "jti"}
    assert claims["iss"] == "rommel-backend"
    assert claims["aud"] == "rommel-daemon"
    assert claims["sub"] == "abc-123"
    assert claims["wid"] == "my-workspace"
    assert claims["scope"] == ["fs:rw", "pty:rw", "git:r"]
    # `iat` and `exp` are unix-seconds ints from the same `now()` (risk 4.5).
    assert isinstance(claims["iat"], int)
    assert isinstance(claims["exp"], int)
    assert claims["exp"] - claims["iat"] == test_settings.token_ttl_seconds
    assert claims["exp"] == int(exp.timestamp())
    # `jti` is UUIDv4 per the broker.
    uuid.UUID(claims["jti"], version=4)

    # `alg` header must be EdDSA (risk 4.3 + daemon's WithValidMethods gate).
    header = jwt.get_unverified_header(token)
    assert header["alg"] == "EdDSA"


def test_broker_signature_verifies_with_public_key(ed25519_keypair, test_settings):
    """Round-trip: the daemon will load the matching PEM via
    `jwt.ParseEdPublicKeyFromPEM` — we replicate that with PyJWT to prove the
    signature itself is well-formed independently of the daemon."""
    _, pub_pem = ed25519_keypair
    pub = serialization.load_pem_public_key(pub_pem.encode("utf-8"))

    token, _ = mint_token(
        user_id="abc-123",
        wid="my-workspace",
        scopes=["fs:rw"],
        settings=test_settings,
    )
    claims = jwt.decode(token, pub, algorithms=["EdDSA"], audience="rommel-daemon")
    assert claims["iss"] == "rommel-backend"


def test_broker_uses_single_now_for_iat_and_exp(test_settings):
    """Risk 4.5 codified as a test: `iat` and `exp` are derived from one
    `datetime.now()` call, so `exp - iat == token_ttl_seconds` exactly."""
    token, _ = mint_token(
        user_id="abc-123",
        wid="my-workspace",
        scopes=["fs:r"],
        settings=test_settings,
    )
    claims = jwt.decode(token, options={"verify_signature": False})
    assert claims["exp"] - claims["iat"] == test_settings.token_ttl_seconds


def test_broker_emits_ed25519_priv_check(ed25519_keypair):
    """Smoke check that the keypair fixture actually produces an Ed25519 PEM
    parseable by `cryptography`."""
    priv_pem, _ = ed25519_keypair
    key = serialization.load_pem_private_key(priv_pem.encode("utf-8"), password=None)
    assert isinstance(key, Ed25519PrivateKey)
