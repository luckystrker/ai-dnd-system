from contextvars import ContextVar

from langchain_core.tools import tool

from app.agents.npc_agent import run_npc_agent
from app.db import async_session_factory
from app.services.npc_lifecycle import NPCLifecycleManager

_room_id: ContextVar[str | None] = ContextVar("npc_room_id", default=None)


def set_npc_room(room_id: str) -> None:
    _room_id.set(room_id)


@tool
async def consult_npc(npc_id: str, context: str) -> str:
    """Consult an important NPC using only the NPC's profile and observable memories."""
    if not _room_id.get():
        return "NPC consultation is unavailable because no room is active."
    try:
        async with async_session_factory() as session:
            manager = NPCLifecycleManager(session)
            npc_context = await manager.spawn(npc_id)
            response = await run_npc_agent(npc_context, context)
            await manager.record_interaction(
                npc_id,
                player_context=context,
                npc_response=response,
            )
        return f"{npc_context.profile.name}: {response}"
    except Exception as error:  # noqa: BLE001
        return f"NPC consultation failed: {error}"
