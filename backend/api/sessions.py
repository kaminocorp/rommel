"""Session-broker routes — the Phase-4 integration gate.

`POST /workspaces/:id/sessions` mints an EdDSA JWT whose claims match
`proto/schemas/session-token.json`. The browser then opens a WS directly to
the workspace daemon at `daemon_url?token=...` — the backend is **not** in
the data path of WS traffic, only the auth path.

`POST /sessions/:id/refresh` is reserved in the schema but unimplemented in
v1 (risk 4.6). Long-lived WS sessions just re-call `POST /sessions` and
re-open the WS.
"""

from __future__ import annotations

import uuid
from datetime import datetime

from fastapi import APIRouter, HTTPException, status
from pydantic import BaseModel

from .deps import RLSDBDep, SettingsDep, UserClaimsDep
from repositories.postgres.workspaces import WorkspacesRepo
from services.session_broker import mint_token

router = APIRouter(tags=["sessions"])


class SessionOut(BaseModel):
    daemon_url: str
    token: str
    expires_at: datetime


@router.post(
    "/workspaces/{workspace_id}/sessions",
    response_model=SessionOut,
    status_code=status.HTTP_201_CREATED,
)
async def create_session(
    workspace_id: uuid.UUID,
    user: UserClaimsDep,
    db: RLSDBDep,
    settings: SettingsDep,
) -> SessionOut:
    """Mint a short-lived EdDSA token for `workspace_id`.

    Authorization model: RLS already scopes the workspace lookup to rows the
    current user owns (`SET LOCAL rommel.user_id` in `get_db_for_user`). If
    the row is missing, the user either doesn't own it or it doesn't exist —
    either way, 404.
    """
    workspace = await WorkspacesRepo(db).get(workspace_id)
    if workspace is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="workspace not found")

    # `wid` claim is the human-readable workspace id the daemon was launched
    # with. v1 uses the UUID string; future versions may want a shorter label.
    wid = str(workspace.id)
    token, expires_at = mint_token(
        user_id=user.sub,
        wid=wid,
        scopes=settings.default_scopes,
        settings=settings,
    )
    return SessionOut(
        daemon_url=settings.daemon_url_template.format(wid=wid),
        token=token,
        expires_at=expires_at,
    )


@router.post(
    "/sessions/{session_id}/refresh",
    status_code=status.HTTP_501_NOT_IMPLEMENTED,
)
async def refresh_session(session_id: uuid.UUID) -> dict[str, str]:
    # Risk 4.6: deferred to Phase-N. Frontend re-calls POST /sessions.
    _ = session_id
    raise HTTPException(
        status_code=status.HTTP_501_NOT_IMPLEMENTED,
        detail="session refresh is reserved in the schema but unimplemented in v1",
    )
