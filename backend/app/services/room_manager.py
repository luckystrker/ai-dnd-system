import secrets
import string

from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.character import RoomPlayer
from app.models.player import Player
from app.models.room import Room

CODE_ALPHABET = "".join(
    character
    for character in string.ascii_uppercase + string.digits
    if character not in "0O1I"
)
MAX_ROOM_PLAYERS = 6


def generate_room_code(length: int = 6) -> str:
    if length <= 0:
        raise ValueError("Room code length must be positive")
    return "".join(secrets.choice(CODE_ALPHABET) for _ in range(length))


def generate_token() -> str:
    return secrets.token_hex(32)


async def create_room(
    session: AsyncSession,
    display_name: str,
    *,
    config: dict | None = None,
) -> tuple[Room, Player]:
    room = Room(code=generate_room_code(), config=config or {})
    player = Player(token=generate_token(), display_name=display_name.strip())
    session.add_all([room, player])
    await session.flush()
    session.add(RoomPlayer(room_id=room.id, player_id=player.id))
    await session.commit()
    await session.refresh(room)
    await session.refresh(player)
    return room, player


async def join_room(
    session: AsyncSession,
    code: str,
    display_name: str,
    player_token: str | None = None,
) -> tuple[Room, Player]:
    normalized_code = code.strip().upper()
    result = await session.execute(select(Room).where(Room.code == normalized_code))
    room = result.scalar_one_or_none()
    if room is None:
        raise ValueError(f"Room with code {normalized_code} not found")
    if room.status != "active":
        raise ValueError("Room is not active")

    player: Player | None = None
    if player_token:
        result = await session.execute(
            select(Player).where(Player.token == player_token)
        )
        player = result.scalar_one_or_none()

    if player is None:
        player = Player(token=generate_token(), display_name=display_name.strip())
        session.add(player)
        await session.flush()
    else:
        player.display_name = display_name.strip()

    existing = await session.execute(
        select(RoomPlayer).where(
            RoomPlayer.room_id == room.id,
            RoomPlayer.player_id == player.id,
        )
    )
    if existing.scalar_one_or_none() is None:
        player_count = await session.scalar(
            select(func.count())
            .select_from(RoomPlayer)
            .where(RoomPlayer.room_id == room.id)
        )
        if (player_count or 0) >= MAX_ROOM_PLAYERS:
            raise ValueError("Room is full")
        session.add(RoomPlayer(room_id=room.id, player_id=player.id))

    await session.commit()
    await session.refresh(room)
    await session.refresh(player)
    return room, player


async def get_room_state(session: AsyncSession, code: str) -> dict | None:
    normalized_code = code.strip().upper()
    result = await session.execute(select(Room).where(Room.code == normalized_code))
    room = result.scalar_one_or_none()
    if room is None:
        return None

    players_result = await session.execute(
        select(Player)
        .join(RoomPlayer, RoomPlayer.player_id == Player.id)
        .where(RoomPlayer.room_id == room.id)
        .order_by(Player.created_at, Player.id)
    )
    players = players_result.scalars().all()
    return {
        "room_id": str(room.id),
        "code": room.code,
        "status": room.status,
        "players": [
            {"id": str(player.id), "display_name": player.display_name}
            for player in players
        ],
    }
