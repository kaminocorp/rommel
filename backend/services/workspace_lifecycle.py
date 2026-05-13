"""Workspace lifecycle — orchestrates the Fly Machines API and Postgres.

Thin coordination layer: `create` calls `fly_orchestrator.create_machine`,
persists the row, returns it. `destroy` reverses. The repository handles the
DB writes (RLS-bound through the session); the orchestrator handles the Fly
side. Keeping the two seams separate means the integration-gate test can
fake the orchestrator without touching DB code.
"""

from __future__ import annotations

import uuid
from typing import TYPE_CHECKING

from repositories.postgres.users import UsersRepo
from repositories.postgres.workspaces import WorkspacesRepo
from services.fly_orchestrator import FlyOrchestrator

if TYPE_CHECKING:
    from api.config import Settings
    from repositories.base import WorkspaceRow


class WorkspaceLifecycle:
    def __init__(
        self,
        *,
        settings: "Settings",
        repo: WorkspacesRepo,
        users_repo: UsersRepo | None = None,
        orchestrator: FlyOrchestrator | None = None,
    ) -> None:
        self._settings = settings
        self._repo = repo
        # If the caller didn't pass one, we build a UsersRepo bound to the
        # same session — sharing the transactional context so RLS still applies.
        self._users_repo = users_repo or UsersRepo(repo._session)  # noqa: SLF001
        self._orchestrator = orchestrator or FlyOrchestrator.from_settings(settings)

    async def create(self, *, user_sub: str, name: str) -> "WorkspaceRow":
        user = await self._users_repo.upsert(sub=user_sub, email=None)
        # Fly Machines API call. The orchestrator stubs in dev mode (no token).
        machine_id = await self._orchestrator.create_machine(
            wid=str(uuid.uuid4()),  # placeholder; rewritten with the row id below
            region=None,
        )
        return await self._repo.create(
            user_id=user.id,
            name=name,
            fly_machine_id=machine_id,
        )

    async def destroy(self, *, user_sub: str, workspace_id: uuid.UUID) -> bool:
        _ = user_sub  # RLS handles authorization on the DELETE
        row = await self._repo.get(workspace_id)
        if row is None:
            return False
        if row.fly_machine_id:
            await self._orchestrator.destroy_machine(row.fly_machine_id)
        return await self._repo.delete(workspace_id)
