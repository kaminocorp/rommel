"""Workspaces repository. RLS scopes reads/writes to the caller's rows."""

from __future__ import annotations

import uuid

from sqlalchemy import delete as sa_delete
from sqlalchemy import insert, select
from sqlalchemy.ext.asyncio import AsyncSession

from models.tables import workspaces
from repositories.base import WorkspaceRow


class WorkspacesRepo:
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def get(self, workspace_id: uuid.UUID) -> WorkspaceRow | None:
        stmt = select(workspaces).where(workspaces.c.id == workspace_id)
        result = await self._session.execute(stmt)
        row = result.mappings().first()
        return _row(row) if row else None

    async def list(self) -> list[WorkspaceRow]:
        stmt = select(workspaces).order_by(workspaces.c.created_at.desc())
        result = await self._session.execute(stmt)
        return [_row(r) for r in result.mappings().all()]

    async def create(
        self,
        *,
        user_id: uuid.UUID,
        name: str,
        fly_machine_id: str | None,
    ) -> WorkspaceRow:
        stmt = (
            insert(workspaces)
            .values(
                id=uuid.uuid4(),
                user_id=user_id,
                name=name,
                fly_machine_id=fly_machine_id,
                status="provisioning",
            )
            .returning(workspaces)
        )
        result = await self._session.execute(stmt)
        row = result.mappings().one()
        return _row(row)

    async def delete(self, workspace_id: uuid.UUID) -> bool:
        stmt = sa_delete(workspaces).where(workspaces.c.id == workspace_id)
        result = await self._session.execute(stmt)
        return (result.rowcount or 0) > 0


def _row(m: object) -> WorkspaceRow:
    return WorkspaceRow(
        id=m["id"],  # type: ignore[index]
        user_id=m["user_id"],  # type: ignore[index]
        name=m["name"],  # type: ignore[index]
        fly_machine_id=m["fly_machine_id"],  # type: ignore[index]
        status=m["status"],  # type: ignore[index]
        created_at=m["created_at"],  # type: ignore[index]
        updated_at=m["updated_at"],  # type: ignore[index]
    )
