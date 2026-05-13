"""Workspace CRUD routes.

v1 surface:
  POST   /workspaces           create (calls fly_orchestrator + persists row)
  GET    /workspaces           list user's workspaces
  GET    /workspaces/:id       fetch one
  DELETE /workspaces/:id       destroy (stops the Fly machine, removes row)

The session-broker route (`POST /workspaces/:id/sessions`) lives in
`api.sessions` so it can be grouped with `POST /sessions/:id/refresh` later.
"""

from __future__ import annotations

import uuid
from datetime import datetime

from fastapi import APIRouter, HTTPException, status
from pydantic import BaseModel, Field

from .deps import RLSDBDep, SettingsDep, UserClaimsDep
from repositories.postgres.workspaces import WorkspaceRow, WorkspacesRepo
from services.workspace_lifecycle import WorkspaceLifecycle

router = APIRouter(tags=["workspaces"])


class WorkspaceCreate(BaseModel):
    name: str = Field(min_length=1, max_length=255)


class WorkspaceOut(BaseModel):
    id: uuid.UUID
    name: str
    status: str
    fly_machine_id: str | None
    created_at: datetime
    updated_at: datetime

    @classmethod
    def from_row(cls, row: WorkspaceRow) -> "WorkspaceOut":
        return cls(
            id=row.id,
            name=row.name,
            status=row.status,
            fly_machine_id=row.fly_machine_id,
            created_at=row.created_at,
            updated_at=row.updated_at,
        )


@router.post("", response_model=WorkspaceOut, status_code=status.HTTP_201_CREATED)
async def create_workspace(
    body: WorkspaceCreate,
    user: UserClaimsDep,
    db: RLSDBDep,
    settings: SettingsDep,
) -> WorkspaceOut:
    lifecycle = WorkspaceLifecycle(settings=settings, repo=WorkspacesRepo(db))
    row = await lifecycle.create(user_sub=user.sub, name=body.name)
    return WorkspaceOut.from_row(row)


@router.get("", response_model=list[WorkspaceOut])
async def list_workspaces(user: UserClaimsDep, db: RLSDBDep) -> list[WorkspaceOut]:
    # RLS already scopes the SELECT to this user's rows; no extra WHERE needed.
    _ = user
    rows = await WorkspacesRepo(db).list()
    return [WorkspaceOut.from_row(r) for r in rows]


@router.get("/{workspace_id}", response_model=WorkspaceOut)
async def get_workspace(workspace_id: uuid.UUID, user: UserClaimsDep, db: RLSDBDep) -> WorkspaceOut:
    _ = user
    row = await WorkspacesRepo(db).get(workspace_id)
    if row is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="workspace not found")
    return WorkspaceOut.from_row(row)


@router.delete("/{workspace_id}", status_code=status.HTTP_204_NO_CONTENT)
async def delete_workspace(
    workspace_id: uuid.UUID,
    user: UserClaimsDep,
    db: RLSDBDep,
    settings: SettingsDep,
) -> None:
    lifecycle = WorkspaceLifecycle(settings=settings, repo=WorkspacesRepo(db))
    deleted = await lifecycle.destroy(user_sub=user.sub, workspace_id=workspace_id)
    if not deleted:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="workspace not found")
    return None
