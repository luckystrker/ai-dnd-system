from contextvars import ContextVar

from langchain_core.tools import tool

from app.db import async_session_factory
from app.engine.combat import CombatAction
from app.services.combat import resolve_combat_action

_room_id: ContextVar[str | None] = ContextVar("combat_room_id", default=None)


def set_combat_room(room_id: str) -> None:
    _room_id.set(room_id)


@tool
async def combat_action(
    actor_id: str,
    target_id: str,
    action_type: str = "attack",
    attack_bonus: int = 0,
    damage_sides: int = 6,
    damage_count: int = 1,
    damage_bonus: int = 0,
) -> str:
    """Resolve one deterministic combat action in the current room."""
    room_id = _room_id.get()
    if not room_id:
        return "Combat action is unavailable because no room is active."
    try:
        action = CombatAction(
            actor_id=actor_id,
            target_id=target_id,
            action_type=action_type,
            attack_bonus=attack_bonus,
            damage_sides=damage_sides,
            damage_count=damage_count,
            damage_bonus=damage_bonus,
        )
        async with async_session_factory() as session:
            result = await resolve_combat_action(session, room_id, action)
        return str(result)
    except Exception as error:  # noqa: BLE001
        return f"Combat action failed: {error}"
