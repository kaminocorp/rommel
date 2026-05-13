"""init: users, workspaces, RLS, app_user role

Revision ID: 0001_init
Revises:
Create Date: 2026-05-13

Creates the two v1 tables and the RLS scaffolding that scopes every query
to the authenticated user. The FastAPI request-scoped DB session sets
`SET LOCAL rommel.user_id = '<sub>'` after authenticating
(`api/deps.py::get_db_for_user`) — every query inside that transaction
becomes RLS-bound automatically.

Two-role split (risk 4.2 of the Phase-4 plan):
  - Migrations run as the privileged role (`rommel` in dev) — owns tables.
  - The app connects as `app_user` — RLS-bound. Postgres exempts table owners
    from RLS by default, so using one role for both would silently bypass.
"""

from __future__ import annotations

from typing import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import UUID

revision: str = "0001_init"
down_revision: str | None = None
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # --- tables -----------------------------------------------------------
    op.create_table(
        "users",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("supabase_sub", sa.String(), nullable=False),
        sa.Column("email", sa.String(), nullable=True),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.UniqueConstraint("supabase_sub", name="uq_users_supabase_sub"),
    )

    op.create_table(
        "workspaces",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("user_id", UUID(as_uuid=True), nullable=False),
        sa.Column("name", sa.String(), nullable=False),
        sa.Column("fly_machine_id", sa.String(), nullable=True),
        sa.Column(
            "status",
            sa.String(),
            nullable=False,
            server_default=sa.text("'provisioning'"),
        ),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.Column(
            "updated_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.ForeignKeyConstraint(
            ["user_id"],
            ["users.id"],
            name="fk_workspaces_user_id_users",
            ondelete="CASCADE",
        ),
    )
    op.create_index("ix_workspaces_user_id", "workspaces", ["user_id"])

    # --- app_user role + grants -------------------------------------------
    # `IF NOT EXISTS` so re-runs against a partially-migrated DB are idempotent.
    op.execute(
        """
        DO $$
        BEGIN
            IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'app_user') THEN
                CREATE ROLE app_user LOGIN PASSWORD 'app_pw';
            END IF;
        END
        $$;
        """
    )
    op.execute("GRANT USAGE ON SCHEMA public TO app_user;")
    op.execute(
        "GRANT SELECT, INSERT, UPDATE, DELETE ON users, workspaces TO app_user;"
    )
    # If the schema is fresh and the app boots before any future migration,
    # sequences may not exist; the GRANT is safe to issue against a wildcard
    # but only the public schema's existing sequences are affected here.
    op.execute(
        "GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO app_user;"
    )

    # --- row-level security ------------------------------------------------
    op.execute("ALTER TABLE users    ENABLE ROW LEVEL SECURITY;")
    op.execute("ALTER TABLE users    FORCE  ROW LEVEL SECURITY;")
    op.execute("ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;")
    op.execute("ALTER TABLE workspaces FORCE  ROW LEVEL SECURITY;")
    # FORCE makes RLS apply to the table owner too — defense in depth in case
    # someone connects as the privileged role from a misconfigured client.

    # users: a row is visible only to its own session.
    op.execute(
        """
        CREATE POLICY users_self_select ON users
            FOR SELECT
            USING (supabase_sub = current_setting('rommel.user_id', true));
        """
    )
    op.execute(
        """
        CREATE POLICY users_self_modify ON users
            FOR ALL
            USING (supabase_sub = current_setting('rommel.user_id', true))
            WITH CHECK (supabase_sub = current_setting('rommel.user_id', true));
        """
    )

    # workspaces: scoped via the owning user.
    op.execute(
        """
        CREATE POLICY workspaces_owner_select ON workspaces
            FOR SELECT
            USING (
                user_id IN (
                    SELECT id FROM users
                    WHERE supabase_sub = current_setting('rommel.user_id', true)
                )
            );
        """
    )
    op.execute(
        """
        CREATE POLICY workspaces_owner_modify ON workspaces
            FOR ALL
            USING (
                user_id IN (
                    SELECT id FROM users
                    WHERE supabase_sub = current_setting('rommel.user_id', true)
                )
            )
            WITH CHECK (
                user_id IN (
                    SELECT id FROM users
                    WHERE supabase_sub = current_setting('rommel.user_id', true)
                )
            );
        """
    )


def downgrade() -> None:
    op.execute("DROP POLICY IF EXISTS workspaces_owner_modify ON workspaces;")
    op.execute("DROP POLICY IF EXISTS workspaces_owner_select ON workspaces;")
    op.execute("DROP POLICY IF EXISTS users_self_modify ON users;")
    op.execute("DROP POLICY IF EXISTS users_self_select ON users;")
    op.execute("ALTER TABLE workspaces DISABLE ROW LEVEL SECURITY;")
    op.execute("ALTER TABLE users      DISABLE ROW LEVEL SECURITY;")
    op.drop_index("ix_workspaces_user_id", table_name="workspaces")
    op.drop_table("workspaces")
    op.drop_table("users")
    op.execute("REVOKE ALL ON SCHEMA public FROM app_user;")
    op.execute("DROP ROLE IF EXISTS app_user;")
