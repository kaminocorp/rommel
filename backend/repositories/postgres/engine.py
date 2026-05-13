"""Async engine + session factory.

One engine per (process, URL). The cache key is the URL so tests can spin up
ephemeral engines against a `postgresql+asyncpg://test/...` without polluting
the prod engine.
"""

from __future__ import annotations

from functools import lru_cache

from sqlalchemy.ext.asyncio import (
    AsyncEngine,
    AsyncSession,
    async_sessionmaker,
    create_async_engine,
)


@lru_cache(maxsize=4)
def get_engine(database_url: str) -> AsyncEngine:
    return create_async_engine(
        database_url,
        echo=False,
        pool_pre_ping=True,
        # Each request's `SET LOCAL rommel.user_id` is transactional, so a
        # modest pool is fine. Fly machines are single-instance for v1.
        pool_size=5,
        max_overflow=5,
    )


@lru_cache(maxsize=4)
def get_session_factory(database_url: str) -> async_sessionmaker[AsyncSession]:
    return async_sessionmaker(
        bind=get_engine(database_url),
        class_=AsyncSession,
        expire_on_commit=False,
        # Autobegin/autoflush turned off — repositories drive transactions
        # explicitly. Keeps `SET LOCAL` lifetime obvious in the request handler.
        autoflush=False,
        autocommit=False,
    )
