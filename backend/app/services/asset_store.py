import uuid
from typing import Any

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.campaign.presets.dnd5e import DND5E_STARTER_PRESET
from app.models.asset import Asset
from app.models.room import Room


async def list_assets(session: AsyncSession, room_id: uuid.UUID) -> list[Asset]:
    result = await session.execute(
        select(Asset)
        .where(Asset.room_id == room_id)
        .order_by(Asset.created_at, Asset.id)
    )
    return list(result.scalars().all())


async def create_asset(
    session: AsyncSession,
    room: Room,
    *,
    kind: str,
    name: str,
    uri: str,
    metadata: dict[str, Any] | None = None,
) -> Asset:
    asset = Asset(
        room_id=room.id,
        kind=kind,
        name=name,
        uri=uri,
        asset_metadata=metadata or {},
    )
    session.add(asset)
    await session.commit()
    await session.refresh(asset)
    return asset


async def prepare_preset_assets(
    session: AsyncSession,
    room: Room,
    *,
    preset: dict[str, Any] = DND5E_STARTER_PRESET,
) -> list[Asset]:
    existing = await list_assets(session, room.id)
    existing_names = {asset.name for asset in existing}
    created: list[Asset] = []
    for location in preset.get("locations", []):
        location_id = str(location.get("id", "location"))
        name = str(location.get("name", location_id))
        if name in existing_names:
            continue
        created.append(
            await create_asset(
                session,
                room,
                kind="map",
                name=name,
                uri=f"asset://room/{room.id}/maps/{location_id}",
                metadata={
                    "location_id": location_id,
                    "description": location.get("description", ""),
                    "generated": False,
                    "provider": None,
                },
            )
        )
    return existing + created


def asset_as_dict(asset: Asset) -> dict[str, Any]:
    return {
        "id": str(asset.id),
        "room_id": str(asset.room_id),
        "kind": asset.kind,
        "name": asset.name,
        "uri": asset.uri,
        "metadata": asset.asset_metadata,
    }
