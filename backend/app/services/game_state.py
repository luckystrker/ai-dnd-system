import uuid
from copy import deepcopy
from typing import Any

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.memory.visibility import Visibility
from app.models.game_state import GameState
from app.services.event_store import append_event


def deep_merge(base: dict[str, Any], patch: dict[str, Any]) -> dict[str, Any]:
    """Merge nested mappings while replacing scalar and list values."""
    result = deepcopy(base)
    for key, value in patch.items():
        if isinstance(value, dict) and isinstance(result.get(key), dict):
            result[key] = deep_merge(result[key], value)
        else:
            result[key] = deepcopy(value)
    return result


async def get_state(session: AsyncSession, room_id: uuid.UUID | str) -> dict[str, Any]:
    result = await session.execute(
        select(GameState).where(GameState.room_id == uuid.UUID(str(room_id)))
    )
    state = result.scalar_one_or_none()
    return deepcopy(state.state) if state else {}


async def update_state(
    session: AsyncSession,
    room_id: uuid.UUID | str,
    patch: dict[str, Any],
    *,
    event_type: str = "state_updated",
    tags: tuple[str, ...] = (),
    visibility: Visibility | str = Visibility.PARTY,
) -> dict[str, Any]:
    room_uuid = uuid.UUID(str(room_id))
    result = await session.execute(
        select(GameState).where(GameState.room_id == room_uuid).with_for_update()
    )
    state = result.scalar_one_or_none()
    if state is None:
        state = GameState(room_id=room_uuid, state={})
        session.add(state)
        await session.flush()

    state.state = deep_merge(state.state, patch)
    new_state = deepcopy(state.state)
    await append_event(
        session,
        room_uuid,
        event_type,
        {"patch": patch, "state": new_state},
        visibility=visibility,
        tags=tags,
    )
    return new_state
