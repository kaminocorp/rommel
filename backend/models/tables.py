"""SQLAlchemy 2.0 Core metadata — single source of truth for table shapes.

Alembic autogenerates migrations from this metadata; the repositories build
queries against these `Table` objects. No ORM session, no relationship loading
— pure Core (per Decision 0.1).
"""

from __future__ import annotations

import uuid

from sqlalchemy import (
    Column,
    DateTime,
    ForeignKey,
    MetaData,
    String,
    Table,
    func,
)
from sqlalchemy.dialects.postgresql import UUID

# Naming convention so Alembic-generated constraint names are stable across
# environments. Without this, autogenerate emits Postgres-default names that
# differ from what the migration file declares, producing endless drift diffs.
NAMING_CONVENTION = {
    "ix": "ix_%(column_0_label)s",
    "uq": "uq_%(table_name)s_%(column_0_name)s",
    "ck": "ck_%(table_name)s_%(constraint_name)s",
    "fk": "fk_%(table_name)s_%(column_0_name)s_%(referred_table_name)s",
    "pk": "pk_%(table_name)s",
}

metadata = MetaData(naming_convention=NAMING_CONVENTION)

users = Table(
    "users",
    metadata,
    Column("id", UUID(as_uuid=True), primary_key=True, default=uuid.uuid4),
    Column(
        "supabase_sub",
        String,
        nullable=False,
        unique=True,
        comment="Supabase JWT `sub` claim — the durable user id from the IdP.",
    ),
    Column("email", String, nullable=True),
    Column("created_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
)

workspaces = Table(
    "workspaces",
    metadata,
    Column("id", UUID(as_uuid=True), primary_key=True, default=uuid.uuid4),
    Column(
        "user_id",
        UUID(as_uuid=True),
        ForeignKey("users.id", ondelete="CASCADE"),
        nullable=False,
        index=True,
    ),
    Column("name", String, nullable=False),
    Column(
        "fly_machine_id",
        String,
        nullable=True,
        comment="Fly Machines API id; null until create_machine succeeds.",
    ),
    Column(
        "status",
        String,
        nullable=False,
        server_default="provisioning",
        comment="One of: provisioning, running, stopped, error.",
    ),
    Column("created_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    Column(
        "updated_at",
        DateTime(timezone=True),
        nullable=False,
        server_default=func.now(),
        onupdate=func.now(),
    ),
)
