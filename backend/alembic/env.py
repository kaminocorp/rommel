"""Alembic environment — sync driver only (risk 4.1).

The FastAPI app uses asyncpg; Alembic's runner is synchronous and hangs on
async drivers, so we derive a sync URL by stripping the `+asyncpg` tag.

Two key invariants enforced here:
  1. `target_metadata = metadata` — autogenerate diffs against `models.tables`.
  2. `url = Settings().alembic_url` — single source of truth; no `sqlalchemy.url`
     in alembic.ini.
"""

from __future__ import annotations

import sys
from logging.config import fileConfig
from pathlib import Path

from alembic import context
from sqlalchemy import engine_from_config, pool

# Make the backend package importable when alembic is invoked from the
# `backend/` directory (the `prepend_sys_path = .` in alembic.ini does this
# too, but being explicit guards against IDE-launched runs).
_BACKEND_ROOT = Path(__file__).resolve().parent.parent
if str(_BACKEND_ROOT) not in sys.path:
    sys.path.insert(0, str(_BACKEND_ROOT))

from api.config import Settings  # noqa: E402
from models.tables import metadata  # noqa: E402

config = context.config

if config.config_file_name is not None:
    fileConfig(config.config_file_name)

target_metadata = metadata


def _settings() -> Settings:
    return Settings()  # type: ignore[call-arg]


def run_migrations_offline() -> None:
    url = _settings().alembic_url
    context.configure(
        url=url,
        target_metadata=target_metadata,
        literal_binds=True,
        compare_type=True,
        dialect_opts={"paramstyle": "named"},
    )
    with context.begin_transaction():
        context.run_migrations()


def run_migrations_online() -> None:
    ini_section = config.get_section(config.config_ini_section) or {}
    ini_section["sqlalchemy.url"] = _settings().alembic_url

    connectable = engine_from_config(
        ini_section,
        prefix="sqlalchemy.",
        poolclass=pool.NullPool,
    )
    with connectable.connect() as connection:
        context.configure(
            connection=connection,
            target_metadata=target_metadata,
            compare_type=True,
        )
        with context.begin_transaction():
            context.run_migrations()


if context.is_offline_mode():
    run_migrations_offline()
else:
    run_migrations_online()
