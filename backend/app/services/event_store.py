import uuid
from collections.abc import Iterable
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import desc, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.memory.visibility import Visibility, is_visible_to
from app.models.events import CampaignEvent
from app.models.room import Room
from app.schemas.ws_messages import Event
from app.services.event_bus import event_bus


async def append_event(
    session: AsyncSession,
    room_id: uuid.UUID | str,
    event_type: str,
    payload: dict[str, Any] | None = None,
    *,
    visibility: Visibility | str = Visibility.PUBLIC,
    visibility_target: str | None = None,
    tags: Iterable[str] = (),
    publish: bool = True,
    embedding: list[float] | None = None,
) -> CampaignEvent:
    """Persist an event and optionally broadcast its public envelope."""
    event_payload = payload or {}
    event_tags = list(tags)
    if embedding is None:
        from app.memory.vector_store import DeterministicEmbeddingProvider

        embedding = await DeterministicEmbeddingProvider().embed(
            f"{event_type} {' '.join(event_tags)} {event_payload}"
        )
    event = CampaignEvent(
        room_id=uuid.UUID(str(room_id)),
        type=event_type,
        payload=event_payload,
        visibility=Visibility(visibility).value,
        visibility_target=visibility_target,
        tags=event_tags,
        embedding=embedding,
    )
    session.add(event)
    await session.commit()
    await session.refresh(event)

    if publish:
        room_result = await session.execute(
            select(Room.code).where(Room.id == event.room_id)
        )
        broadcast_room_id = room_result.scalar_one_or_none() or str(room_id)
        await event_bus.publish(
            broadcast_room_id,
            Event(
                type=event_type,
                payload={
                    **event.payload,
                    "event_id": str(event.id),
                    "visibility": event.visibility,
                    "tags": event.tags,
                },
                timestamp=event.timestamp or datetime.now(UTC),
            ),
        )
    return event


async def recent_events(
    session: AsyncSession,
    room_id: uuid.UUID | str,
    *,
    limit: int = 20,
    observer_id: str | None = None,
    is_dm: bool = False,
    audience: str = "party",
    observer_region: str | None = None,
) -> list[CampaignEvent]:
    if limit < 1:
        return []
    result = await session.execute(
        select(CampaignEvent)
        .where(CampaignEvent.room_id == uuid.UUID(str(room_id)))
        .order_by(desc(CampaignEvent.timestamp))
        .limit(min(max(limit * 4, limit), 200))
    )
    events = [
        event
        for event in result.scalars().all()
        if is_visible_to(
            event.visibility,
            observer_id=observer_id,
            visibility_target=event.visibility_target,
            is_dm=is_dm,
            audience=audience,
            observer_region=observer_region,
            tags=event.tags,
        )
    ]
    return list(reversed(events[:limit]))


def event_as_dict(event: CampaignEvent) -> dict[str, Any]:
    return {
        "id": str(event.id),
        "room_id": str(event.room_id),
        "timestamp": event.timestamp.isoformat() if event.timestamp else None,
        "type": event.type,
        "payload": event.payload,
        "visibility": event.visibility,
        "visibility_target": event.visibility_target,
        "tags": event.tags,
    }
