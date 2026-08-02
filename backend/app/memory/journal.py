import uuid

from sqlalchemy.ext.asyncio import AsyncSession

from app.memory.visibility import Visibility
from app.services.event_store import append_event


async def save_session_journal(
    session: AsyncSession,
    room_id: uuid.UUID | str,
    summary: str,
    *,
    tags: tuple[str, ...] = ("journal",),
) -> None:
    await append_event(
        session,
        room_id,
        "journal",
        {"summary": summary},
        visibility=Visibility.PARTY,
        tags=tags,
    )
