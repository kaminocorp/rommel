"""Policy bundle — v1 stub.

`GET /policy` returns `{"version": 0, "rules": []}` so clients can begin
polling and rendering an empty state. Real rule evaluation lands when there's
a rule to enforce (per §6 of the Phase-4 plan).
"""

from __future__ import annotations


def current_bundle() -> dict[str, object]:
    return {"version": 0, "rules": []}
