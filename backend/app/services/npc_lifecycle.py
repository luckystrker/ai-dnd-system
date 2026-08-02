import json
import uuid
from copy import deepcopy
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.memory.visibility import Visibility
from app.models.events import CampaignEvent
from app.models.npc import NPCProfile, NPCState
from app.services.event_store import append_event, recent_events
from app.services.game_state import deep_merge


@dataclass(frozen=True)
class NPCContext:
    profile: NPCProfile
    state: NPCState
    visible_events: list[CampaignEvent]

    def render(self, max_chars: int = 9000) -> str:
        events = [
            {
                "type": event.type,
                "payload": event.payload,
                "timestamp": event.timestamp.isoformat() if event.timestamp else None,
            }
            for event in self.visible_events
        ]
        text = "\n\n".join(
            [
                f"NPC: {self.profile.name}",
                f"Personality: {self.profile.personality}",
                "Goals: "
                + json.dumps(self.profile.goals, ensure_ascii=True, default=str),
                "Private memory: "
                + json.dumps(self.profile.memory, ensure_ascii=True, default=str),
                f"Location: {self.state.location}; status: {self.state.status}",
                "Observable events: "
                + json.dumps(events, ensure_ascii=True, default=str),
            ]
        )
        return text[:max_chars]


def _profile_snapshot(profile: NPCProfile) -> NPCProfile:
    return NPCProfile(
        id=profile.id,
        room_id=profile.room_id,
        name=profile.name,
        personality=profile.personality,
        goals=deepcopy(profile.goals),
        memory=deepcopy(profile.memory),
        created_at=profile.created_at,
    )


def _state_snapshot(state: NPCState) -> NPCState:
    return NPCState(
        npc_id=state.npc_id,
        location=state.location,
        status=state.status,
        state=deepcopy(state.state),
        last_active_at=state.last_active_at,
    )


class NPCLifecycleManager:
    def __init__(self, session: AsyncSession) -> None:
        self.session = session

    async def _profile(self, npc_id: uuid.UUID | str) -> NPCProfile:
        result = await self.session.execute(
            select(NPCProfile).where(NPCProfile.id == uuid.UUID(str(npc_id)))
        )
        profile = result.scalar_one_or_none()
        if profile is None:
            raise ValueError(f"NPC {npc_id} not found")
        return profile

    async def spawn(
        self,
        npc_id: uuid.UUID | str,
        *,
        location: str | None = None,
    ) -> NPCContext:
        profile = await self._profile(npc_id)
        result = await self.session.execute(
            select(NPCState).where(NPCState.npc_id == profile.id)
        )
        state = result.scalar_one_or_none()
        if state is None:
            state = NPCState(npc_id=profile.id, location=location or "unknown")
            self.session.add(state)
        elif location:
            state.location = location
        state.status = "active"
        state.last_active_at = datetime.now(UTC)
        await self.session.commit()
        await self.session.refresh(state)
        return await self.context(profile, state)

    async def context(self, profile: NPCProfile, state: NPCState) -> NPCContext:
        events = await recent_events(
            self.session,
            profile.room_id,
            limit=30,
            observer_id=str(profile.id),
            audience="npc",
            observer_region=state.location,
        )
        return NPCContext(
            profile=_profile_snapshot(profile),
            state=_state_snapshot(state),
            visible_events=events,
        )

    async def serialize_memory(
        self,
        npc_id: uuid.UUID | str,
        memory_patch: dict[str, Any],
        *,
        despawn: bool = True,
    ) -> NPCContext:
        profile = await self._profile(npc_id)
        result = await self.session.execute(
            select(NPCState).where(NPCState.npc_id == profile.id)
        )
        state = result.scalar_one_or_none()
        if state is None:
            state = NPCState(npc_id=profile.id)
            self.session.add(state)
        profile.memory = deep_merge(profile.memory, memory_patch)
        if despawn:
            state.status = "sleeping"
        await self.session.commit()
        await self.session.refresh(profile)
        await self.session.refresh(state)
        return await self.context(profile, state)

    async def despawn(self, npc_id: uuid.UUID | str) -> NPCContext:
        profile = await self._profile(npc_id)
        result = await self.session.execute(
            select(NPCState).where(NPCState.npc_id == profile.id)
        )
        state = result.scalar_one_or_none()
        if state is None:
            state = NPCState(npc_id=profile.id)
            self.session.add(state)
        state.status = "sleeping"
        await self.session.commit()
        await self.session.refresh(state)
        return await self.context(profile, state)

    async def record_interaction(
        self,
        npc_id: uuid.UUID | str,
        *,
        player_context: str,
        npc_response: str,
    ) -> NPCContext:
        context = await self.serialize_memory(
            npc_id,
            {
                "last_interaction": player_context,
                "last_response": npc_response,
            },
            despawn=False,
        )
        await append_event(
            self.session,
            context.profile.room_id,
            "npc_interaction",
            {
                "npc_id": str(context.profile.id),
                "npc_name": context.profile.name,
                "player_context": player_context,
                "response": npc_response,
            },
            visibility=Visibility.PARTY,
            tags=("npc", context.profile.name),
        )
        return context


async def spawn_npc(
    session: AsyncSession,
    npc_id: uuid.UUID | str,
    *,
    location: str | None = None,
) -> NPCContext:
    return await NPCLifecycleManager(session).spawn(npc_id, location=location)


async def despawn_npc(session: AsyncSession, npc_id: uuid.UUID | str) -> NPCContext:
    return await NPCLifecycleManager(session).despawn(npc_id)
