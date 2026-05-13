"""Users repository — upsert-by-sub is the only write path.

New users land via the auth seam: the first `GET /auth/me` for a fresh
Supabase `sub` creates the row. RLS scopes every read.
"""

from __future__ import annotations

import uuid

from sqlalchemy import select
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession

from models.tables import users
from repositories.base import UserRow


class UsersRepo:
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def get_by_sub(self, sub: str) -> UserRow | None:
        stmt = select(users).where(users.c.supabase_sub == sub)
        result = await self._session.execute(stmt)
        row = result.mappings().first()
        return _row(row) if row else None

    async def upsert(self, sub: str, email: str | None) -> UserRow:
        # ON CONFLICT (supabase_sub) DO UPDATE SET supabase_sub = EXCLUDED.supabase_sub
        # — a no-op update whose only purpose is to make Postgres return the
        # existing row via RETURNING (DO NOTHING swallows RETURNING for the
        # conflict path). Keeps the row id stable across re-authentications;
        # email is left alone (we never overwrite a previously-stored value).
        stmt = pg_insert(users).values(
            id=uuid.uuid4(),
            supabase_sub=sub,
            email=email,
        )
        stmt = stmt.on_conflict_do_update(
            index_elements=[users.c.supabase_sub],
            set_={"supabase_sub": stmt.excluded.supabase_sub},
        ).returning(users)
        result = await self._session.execute(stmt)
        row = result.mappings().one()
        return _row(row)


def _row(m: object) -> UserRow:
    # m is a RowMapping; the attribute access form lines up with column names.
    return UserRow(
        id=m["id"],  # type: ignore[index]
        supabase_sub=m["supabase_sub"],  # type: ignore[index]
        email=m["email"],  # type: ignore[index]
        created_at=m["created_at"],  # type: ignore[index]
    )
