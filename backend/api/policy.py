"""Policy routes — v1 stub.

`GET /policy` returns an empty bundle so clients can begin polling. `PUT
/policy` is deferred per §6 of the Phase-4 plan; the policy *pipeline* lands
when there's a rule to enforce.
"""

from __future__ import annotations

from fastapi import APIRouter

from policy.rules import current_bundle

router = APIRouter(tags=["policy"])


@router.get("")
async def get_policy() -> dict[str, object]:
    return current_bundle()
