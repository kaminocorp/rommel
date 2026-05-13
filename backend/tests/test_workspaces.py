"""Workspace route + lifecycle smoke tests (no real Postgres / no real Fly).

DB-backed assertions are in tests that depend on `require_postgres`. These
tests instead exercise the Pydantic shapes + orchestrator stub mode, which
covers the wire contract the frontend depends on.
"""

from __future__ import annotations

import pytest

from services.fly_orchestrator import FlyOrchestrator


@pytest.mark.asyncio
async def test_fly_orchestrator_stub_mode_when_no_token(test_settings):
    # Dev-mode: empty `fly_api_token` → every call returns deterministic stubs.
    assert test_settings.fly_api_token == ""
    orch = FlyOrchestrator.from_settings(test_settings)
    mid = await orch.create_machine(wid="ws-abc", region=None)
    assert mid.startswith("stub-")
    await orch.start_machine(mid)
    await orch.stop_machine(mid)
    await orch.destroy_machine(mid)


@pytest.mark.asyncio
async def test_policy_endpoint_returns_empty_bundle(client):
    resp = await client.get("/policy")
    assert resp.status_code == 200
    assert resp.json() == {"version": 0, "rules": []}
