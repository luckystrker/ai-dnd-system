import uuid
from typing import Any

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.character import Character, RoomPlayer
from app.models.player import Player
from app.models.room import Room


async def find_room(session: AsyncSession, code: str) -> Room | None:
    result = await session.execute(
        select(Room).where(Room.code == code.strip().upper())
    )
    return result.scalar_one_or_none()


async def _find_player(
    session: AsyncSession,
    room: Room,
    player_token: str | None,
) -> Player:
    statement = (
        select(Player)
        .join(RoomPlayer, RoomPlayer.player_id == Player.id)
        .where(RoomPlayer.room_id == room.id)
        .order_by(Player.created_at, Player.id)
    )
    if player_token:
        statement = statement.where(Player.token == player_token)
    result = await session.execute(statement)
    player = result.scalars().first()
    if player is None:
        raise ValueError("Player is not a member of this room")
    return player


async def list_characters(session: AsyncSession, room_id: uuid.UUID) -> list[Character]:
    result = await session.execute(
        select(Character)
        .where(Character.room_id == room_id)
        .order_by(Character.created_at)
    )
    return list(result.scalars().all())


async def create_character(
    session: AsyncSession,
    room: Room,
    *,
    name: str,
    stats: dict[str, Any],
    inventory: list[Any],
    player_token: str | None,
) -> Character:
    player = await _find_player(session, room, player_token)
    character = Character(
        room_id=room.id,
        player_id=player.id,
        name=name.strip(),
        stats=stats,
        inventory=inventory,
    )
    session.add(character)
    await session.commit()
    await session.refresh(character)
    return character


async def update_character(
    session: AsyncSession,
    room: Room,
    character_id: uuid.UUID | str,
    *,
    name: str | None = None,
    stats: dict[str, Any] | None = None,
    inventory: list[Any] | None = None,
) -> Character:
    result = await session.execute(
        select(Character).where(
            Character.id == uuid.UUID(str(character_id)),
            Character.room_id == room.id,
        )
    )
    character = result.scalar_one_or_none()
    if character is None:
        raise ValueError("Character not found")
    if name is not None:
        character.name = name.strip()
    if stats is not None:
        character.stats = stats
    if inventory is not None:
        character.inventory = inventory
    await session.commit()
    await session.refresh(character)
    return character


def character_as_dict(character: Character) -> dict[str, Any]:
    return {
        "id": str(character.id),
        "room_id": str(character.room_id),
        "player_id": str(character.player_id),
        "name": character.name,
        "stats": character.stats,
        "inventory": character.inventory,
    }
