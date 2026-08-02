"""Add NPC profiles and lifecycle state.

Revision ID: 0003_npc_profiles_and_states
Revises: 0002_game_state_and_events
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects import postgresql

revision: str = "0003_npc_profiles_and_states"
down_revision: str | None = "0002_game_state_and_events"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "npc_profiles",
        sa.Column(
            "id",
            postgresql.UUID(as_uuid=True),
            server_default=sa.text("gen_random_uuid()"),
            nullable=False,
        ),
        sa.Column("room_id", postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("name", sa.String(length=100), nullable=False),
        sa.Column(
            "personality", sa.String(length=2000), server_default="", nullable=False
        ),
        sa.Column(
            "goals",
            postgresql.JSONB(astext_type=sa.Text()),
            server_default=sa.text("'[]'::jsonb"),
            nullable=False,
        ),
        sa.Column(
            "memory",
            postgresql.JSONB(astext_type=sa.Text()),
            server_default=sa.text("'{}'::jsonb"),
            nullable=False,
        ),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            server_default=sa.text("now()"),
            nullable=False,
        ),
        sa.ForeignKeyConstraint(["room_id"], ["rooms.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_npc_profiles_room_id", "npc_profiles", ["room_id"])
    op.create_table(
        "npc_states",
        sa.Column("npc_id", postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column(
            "location", sa.String(length=200), server_default="unknown", nullable=False
        ),
        sa.Column(
            "status", sa.String(length=20), server_default="sleeping", nullable=False
        ),
        sa.Column(
            "state",
            postgresql.JSONB(astext_type=sa.Text()),
            server_default=sa.text("'{}'::jsonb"),
            nullable=False,
        ),
        sa.Column("last_active_at", sa.DateTime(timezone=True), nullable=True),
        sa.ForeignKeyConstraint(["npc_id"], ["npc_profiles.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("npc_id"),
    )


def downgrade() -> None:
    op.drop_table("npc_states")
    op.drop_index("ix_npc_profiles_room_id", table_name="npc_profiles")
    op.drop_table("npc_profiles")
