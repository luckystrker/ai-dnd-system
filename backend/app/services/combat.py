import uuid
from typing import Any

from sqlalchemy.ext.asyncio import AsyncSession

from app.engine.combat import CombatAction, Combatant, CombatState, CombatStateMachine
from app.memory.visibility import Visibility
from app.services.event_store import append_event
from app.services.game_state import get_state, update_state


async def start_combat(
    session: AsyncSession,
    room_id: uuid.UUID | str,
    combatants: list[Combatant],
) -> CombatState:
    machine = CombatStateMachine.start(combatants)
    state = machine.state
    await update_state(
        session,
        room_id,
        {"combat": state.model_dump()},
        event_type="combat_started",
        tags=("combat",),
        visibility=Visibility.PARTY,
    )
    return state


async def resolve_combat_action(
    session: AsyncSession,
    room_id: uuid.UUID | str,
    action: CombatAction,
) -> dict[str, Any]:
    game_state = await get_state(session, room_id)
    combat_data = game_state.get("combat")
    if not combat_data:
        raise ValueError("No active combat in this room")
    machine = CombatStateMachine.from_state(combat_data)
    result = machine.resolve_action(action)
    await update_state(
        session,
        room_id,
        {"combat": machine.state.model_dump()},
        event_type="combat_action",
        tags=("combat", action.action_type),
        visibility=Visibility.PARTY,
    )
    if machine.state.status in {"victory", "defeat"}:
        await append_event(
            session,
            room_id,
            "combat_ended",
            {"status": machine.state.status},
            visibility=Visibility.PARTY,
            tags=("combat", "combat_ended"),
        )
    return {
        "result": result.model_dump() if result else None,
        "state": machine.state.model_dump(),
    }
