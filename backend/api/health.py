"""GET /healthz — unauthenticated liveness probe.

Used by Fly's `[[services.http_checks]]` and by the test suite as the
canary that the app factory wired up correctly. Mirrors the daemon's
unauthenticated `/healthz` endpoint (`sandbox-daemon/cmd/daemon/main.go`).
"""

from __future__ import annotations

from fastapi import APIRouter

router = APIRouter(tags=["health"])


@router.get("/healthz")
async def healthz() -> dict[str, bool]:
    return {"ok": True}
