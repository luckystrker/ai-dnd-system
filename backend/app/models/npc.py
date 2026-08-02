import uuid
from datetime import datetime
from typing import Any

from sqlalchemy import DateTime, ForeignKey, String, func
from sqlalchemy.dialects.postgresql import JSONB, UUID
from sqlalchemy.orm import Mapped, mapped_column

from app.models.room import Base


class NPCProfile(Base):
    __tablename__ = "npc_profiles"

    id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True),
        primary_key=True,
        default=uuid.uuid4,
    )
    room_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True),
        ForeignKey("rooms.id", ondelete="CASCADE"),
        nullable=False,
    )
    name: Mapped[str] = mapped_column(String(100), nullable=False)
    personality: Mapped[str] = mapped_column(String(2000), default="", nullable=False)
    goals: Mapped[list[Any]] = mapped_column(JSONB, default=list, nullable=False)
    memory: Mapped[dict[str, Any]] = mapped_column(JSONB, default=dict, nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        server_default=func.now(),
        nullable=False,
    )


class NPCState(Base):
    __tablename__ = "npc_states"

    npc_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True),
        ForeignKey("npc_profiles.id", ondelete="CASCADE"),
        primary_key=True,
    )
    location: Mapped[str] = mapped_column(
        String(200), default="unknown", nullable=False
    )
    status: Mapped[str] = mapped_column(String(20), default="sleeping", nullable=False)
    state: Mapped[dict[str, Any]] = mapped_column(JSONB, default=dict, nullable=False)
    last_active_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
