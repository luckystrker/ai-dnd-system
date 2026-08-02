"""Add mutable game state and append-only campaign events.

Revision ID: 0002_game_state_and_events
Revises: 0001_initial_schema
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from pgvector.sqlalchemy import Vector
from sqlalchemy.dialects import postgresql

revision: str = "0002_game_state_and_events"
down_revision: str | None = "0001_initial_schema"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.execute("CREATE EXTENSION IF NOT EXISTS vector")
    op.create_table(
        "game_state",
        sa.Column("room_id", postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column(
            "state",
            postgresql.JSONB(astext_type=sa.Text()),
            server_default=sa.text("'{}'::jsonb"),
            nullable=False,
        ),
        sa.Column(
            "updated_at",
            sa.DateTime(timezone=True),
            server_default=sa.text("now()"),
            nullable=False,
        ),
        sa.ForeignKeyConstraint(["room_id"], ["rooms.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("room_id"),
    )
    op.create_table(
        "events",
        sa.Column(
            "id",
            postgresql.UUID(as_uuid=True),
            server_default=sa.text("gen_random_uuid()"),
            nullable=False,
        ),
        sa.Column("room_id", postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column(
            "timestamp",
            sa.DateTime(timezone=True),
            server_default=sa.text("now()"),
            nullable=False,
        ),
        sa.Column("type", sa.String(length=50), nullable=False),
        sa.Column(
            "payload",
            postgresql.JSONB(astext_type=sa.Text()),
            server_default=sa.text("'{}'::jsonb"),
            nullable=False,
        ),
        sa.Column(
            "visibility", sa.String(length=20), server_default="public", nullable=False
        ),
        sa.Column("visibility_target", sa.String(length=100), nullable=True),
        sa.Column(
            "tags",
            postgresql.ARRAY(sa.String(length=100)),
            server_default=sa.text("ARRAY[]::varchar[]"),
            nullable=False,
        ),
        sa.Column("embedding", Vector(1536), nullable=True),
        sa.ForeignKeyConstraint(["room_id"], ["rooms.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_events_room_timestamp", "events", ["room_id", "timestamp"])
    op.create_index("ix_events_room_type", "events", ["room_id", "type"])


def downgrade() -> None:
    op.drop_index("ix_events_room_type", table_name="events")
    op.drop_index("ix_events_room_timestamp", table_name="events")
    op.drop_table("events")
    op.drop_table("game_state")
