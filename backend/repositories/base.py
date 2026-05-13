"""Repository protocols — the seam between API/service layers and Postgres.

Implementations live in `repositories.postgres`. Keeping a Protocol layer
lets tests swap in an in-memory fake without touching FastAPI internals; it
also makes the eventual move to a second store (e.g. Redis-backed session
cache) a per-Protocol question instead of a sprawling rewrite.
"""

from __future__ import annotations

import uuid
from dataclasses import dataclass
from datetime import datetime
from typing import Protocol


@dataclass(frozen=True)
class UserRow:
    id: uuid.UUID
    supabase_sub: str
    email: str | None
    created_at: datetime


@dataclass(frozen=True)
class WorkspaceRow:
    id: uuid.UUID
    user_id: uuid.UUID
    name: str
    fly_machine_id: str | None
    status: str
    created_at: datetime
    updated_at: datetime


class UsersRepoProtocol(Protocol):
    async def get_by_sub(self, sub: str) -> UserRow | None: ...
    async def upsert(self, sub: str, email: str | None) -> UserRow: ...


class WorkspacesRepoProtocol(Protocol):
    async def get(self, workspace_id: uuid.UUID) -> WorkspaceRow | None: ...
    async def list(self) -> list[WorkspaceRow]: ...
    async def create(
        self, *, user_id: uuid.UUID, name: str, fly_machine_id: str | None
    ) -> WorkspaceRow: ...
    async def delete(self, workspace_id: uuid.UUID) -> bool: ...
