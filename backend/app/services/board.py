import uuid
from typing import Any

from sqlalchemy.ext.asyncio import AsyncSession

from app.memory.visibility import Visibility
from app.services.game_state import update_state


async def update_board(
    session: AsyncSession,
    room_id: uuid.UUID | str,
    *,
    map_url: str | None = None,
    tokens: dict[str, dict[str, Any]] | None = None,
) -> dict[str, Any]:
    board: dict[str, Any] = {}
    if map_url is not None:
        board["map_url"] = map_url
    if tokens is not None:
        board["tokens"] = tokens
    return await update_state(
        session,
        room_id,
        {"board": board},
        event_type="board_updated",
        tags=("board",),
        visibility=Visibility.PARTY,
    )
