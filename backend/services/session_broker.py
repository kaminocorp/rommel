"""Session-token broker — the Phase-4 integration gate.

Mints EdDSA (Ed25519) JWTs whose claim shape matches
`proto/schemas/session-token.json`. The daemon's
`sandbox-daemon/internal/auth/token.go::Verify` is the matching verifier:

    iss == "rommel-backend"          (const, also in the proto schema)
    aud == "rommel-daemon"           (const, also in the proto schema)
    alg == "EdDSA"                   (enforced via golang-jwt's WithValidMethods)
    wid == this daemon's WID         (binds the token to one workspace)
    exp > now                        (strict; no leeway either side — risk 4.5)
    jti present (UUIDv4)             (reserved for replay protection)

Failure modes the daemon rejects (each has a test in `sandbox-daemon/internal/ws/server_test.go`):
    - missing/bad signature
    - alg != EdDSA (alg=none / RS256 confusion)
    - wid mismatch
    - exp in past
    - scope element not in the enum
"""

from __future__ import annotations

import uuid
from datetime import UTC, datetime, timedelta
from typing import TYPE_CHECKING

import jwt

if TYPE_CHECKING:
    from api.config import Settings

ISSUER = "rommel-backend"
AUDIENCE = "rommel-daemon"
ALG = "EdDSA"


def mint_token(
    *,
    user_id: str,
    wid: str,
    scopes: list[str],
    settings: "Settings",
) -> tuple[str, datetime]:
    """Sign a session token. Returns `(jwt_string, expires_at)`.

    Risk 4.5: `iat` and `exp` are derived from a *single* `now()` call so the
    delta is exact. Computing them from two separate `datetime.now()` calls
    can produce a sub-second drift that, combined with the daemon's strict
    `exp` check, has previously caused tokens to be born already-expired.
    """
    now = datetime.now(tz=UTC)
    exp = now + timedelta(seconds=settings.token_ttl_seconds)

    claims = {
        "iss": ISSUER,
        "sub": user_id,
        "aud": AUDIENCE,
        "wid": wid,
        "scope": scopes,
        "iat": int(now.timestamp()),
        "exp": int(exp.timestamp()),
        "jti": str(uuid.uuid4()),
    }

    # PyJWT loads the PEM via `cryptography` (the `[crypto]` extra). The
    # daemon's golang-jwt uses `ParseEdPrivateKeyFromPEM` on the matching
    # pubkey — the two libraries produce byte-identical signatures over the
    # same canonicalized payload.
    token = jwt.encode(claims, settings.token_privkey, algorithm=ALG)
    return token, exp
