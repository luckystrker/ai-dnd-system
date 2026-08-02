import json
import uuid
from dataclasses import dataclass
from typing import Any

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.memory.vector_store import CampaignMemory
from app.models.room import Room
from app.services.event_store import event_as_dict, recent_events
from app.services.game_state import get_state


@dataclass(frozen=True)
class DMContext:
    state: dict[str, Any]
    recent: list[dict[str, Any]]
    recalled: list[dict[str, Any]]
    system_name: str = "dnd5e"

    def render(self, max_chars: int = 12000) -> str:
        sections = [
            f"## Selected Game System\n{self.system_name}",
            "## Current Game State\n"
            + json.dumps(self.state, ensure_ascii=True, default=str),
            "## Recent Events\n"
            + json.dumps(self.recent, ensure_ascii=True, default=str),
            "## Recalled Campaign Memory\n"
            + json.dumps(self.recalled, ensure_ascii=True, default=str),
        ]
        text = "\n\n".join(sections)
        return text[:max_chars]


async def build_dm_context(
    session: AsyncSession,
    room_id: uuid.UUID | str,
    query: str,
    *,
    recent_limit: int = 20,
    recall_limit: int = 5,
) -> DMContext:
    room_result = await session.execute(
        select(Room).where(Room.id == uuid.UUID(str(room_id)))
    )
    room = room_result.scalar_one_or_none()
    system_name = "dnd5e"
    if room and isinstance(room.config, dict):
        configured_system = room.config.get("system")
        if isinstance(configured_system, str) and configured_system.strip():
            system_name = configured_system.strip().lower()
    events = await recent_events(session, room_id, limit=recent_limit, is_dm=True)
    memory = CampaignMemory()
    recalled = await memory.recall(
        session, room_id, query, limit=recall_limit, is_dm=True
    )
    return DMContext(
        state=await get_state(session, room_id),
        recent=[event_as_dict(event) for event in events],
        recalled=[event_as_dict(event) for event in recalled],
        system_name=system_name,
    )
