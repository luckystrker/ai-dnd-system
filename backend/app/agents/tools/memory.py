from contextvars import ContextVar

from langchain_core.tools import tool

from app.db import async_session_factory
from app.memory.vector_store import CampaignMemory
from app.services.event_store import event_as_dict

_room_id: ContextVar[str | None] = ContextVar("memory_room_id", default=None)


def set_memory_room(room_id: str) -> None:
    _room_id.set(room_id)


@tool
async def recall(query: str, tags: str = "") -> str:
    """Recall relevant facts and past events from the current campaign."""
    room_id = _room_id.get()
    if not room_id:
        return "Campaign memory is unavailable because no room is active."

    tag_list = [tag.strip() for tag in tags.split(",") if tag.strip()]
    try:
        async with async_session_factory() as session:
            events = await CampaignMemory().recall(
                session,
                room_id,
                query,
                tags=tag_list or None,
                limit=5,
                is_dm=True,
            )
        if not events:
            return "No relevant campaign memories were found."
        return "\n".join(
            f"- {event.type}: {event_as_dict(event)['payload']}" for event in events
        )
    except Exception as error:  # noqa: BLE001
        return f"Campaign memory lookup failed: {error}"
