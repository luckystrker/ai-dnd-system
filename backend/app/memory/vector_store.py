import math
import uuid
from hashlib import sha256
from typing import Protocol

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.memory.visibility import is_visible_to
from app.models.events import CampaignEvent


class EmbeddingProvider(Protocol):
    async def embed(self, text: str) -> list[float]: ...


class DeterministicEmbeddingProvider:
    """Offline fallback used until an external embedding provider is configured."""

    def __init__(self, dimensions: int = 1536) -> None:
        self.dimensions = dimensions

    async def embed(self, text: str) -> list[float]:
        values: list[float] = []
        counter = 0
        while len(values) < self.dimensions:
            digest = sha256(f"{counter}:{text}".encode()).digest()
            values.extend((byte / 127.5) - 1.0 for byte in digest)
            counter += 1
        values = values[: self.dimensions]
        length = math.sqrt(sum(value * value for value in values)) or 1.0
        return [value / length for value in values]


def event_search_text(event: CampaignEvent) -> str:
    return f"{event.type} {' '.join(event.tags)} {event.payload}"


class CampaignMemory:
    def __init__(self, provider: EmbeddingProvider | None = None) -> None:
        self.provider = provider or DeterministicEmbeddingProvider()

    async def index_event(self, event: CampaignEvent) -> list[float]:
        embedding = await self.provider.embed(event_search_text(event))
        if len(embedding) != 1536:
            raise ValueError("Campaign event embeddings must have 1536 dimensions")
        event.embedding = embedding
        return embedding

    async def recall(
        self,
        session: AsyncSession,
        room_id: uuid.UUID | str,
        query: str,
        *,
        limit: int = 5,
        tags: list[str] | None = None,
        observer_id: str | None = None,
        is_dm: bool = True,
        audience: str = "party",
        observer_region: str | None = None,
    ) -> list[CampaignEvent]:
        if limit < 1:
            return []
        vector = await self.provider.embed(query)
        statement = select(CampaignEvent).where(
            CampaignEvent.room_id == uuid.UUID(str(room_id)),
            CampaignEvent.embedding.is_not(None),
        )
        if tags:
            for tag in tags:
                statement = statement.where(CampaignEvent.tags.contains([tag]))
        statement = statement.order_by(
            CampaignEvent.embedding.cosine_distance(vector)
        ).limit(min(limit * 4, 100))
        result = await session.execute(statement)
        return [
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
        ][:limit]
