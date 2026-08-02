from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.db import get_session
from app.schemas.room import (
    AssetResponse,
    BoardUpdateRequest,
    CharacterCreateRequest,
    CharacterResponse,
    CharacterUpdateRequest,
    CreateRoomRequest,
    CreateRoomResponse,
    JoinRoomRequest,
    JoinRoomResponse,
    RoomSnapshotResponse,
    RoomStateResponse,
)
from app.services import character_manager, room_manager
from app.services.asset_store import asset_as_dict, list_assets, prepare_preset_assets
from app.services.board import update_board
from app.services.game_state import get_state
from app.services.system_service import get_system

router = APIRouter(prefix="/room", tags=["rooms"])


@router.post("/create", response_model=CreateRoomResponse)
async def create_room(
    request: CreateRoomRequest,
    session: AsyncSession = Depends(get_session),
) -> CreateRoomResponse:
    try:
        get_system(request.system)
    except ValueError as error:
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail=str(error)
        ) from error
    room, player = await room_manager.create_room(
        session,
        request.display_name,
        config={"system": request.system},
    )
    return CreateRoomResponse(
        room_id=str(room.id),
        code=room.code,
        player_token=player.token,
    )


@router.post("/join", response_model=JoinRoomResponse)
async def join_room(
    request: JoinRoomRequest,
    session: AsyncSession = Depends(get_session),
) -> JoinRoomResponse:
    try:
        room, player = await room_manager.join_room(
            session,
            request.code,
            request.display_name,
            request.player_token,
        )
    except ValueError as error:
        detail = str(error)
        error_status = (
            status.HTTP_404_NOT_FOUND
            if "not found" in detail
            else status.HTTP_409_CONFLICT
        )
        raise HTTPException(status_code=error_status, detail=detail) from error
    return JoinRoomResponse(room_id=str(room.id), player_token=player.token)


@router.get("/{code}", response_model=RoomStateResponse)
async def get_room(
    code: str,
    session: AsyncSession = Depends(get_session),
) -> RoomStateResponse:
    state = await room_manager.get_room_state(session, code)
    if state is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Room not found"
        )
    return RoomStateResponse.model_validate(state)


@router.get("/{code}/snapshot", response_model=RoomSnapshotResponse)
async def get_room_snapshot(
    code: str,
    session: AsyncSession = Depends(get_session),
) -> RoomSnapshotResponse:
    state = await room_manager.get_room_state(session, code)
    room = await character_manager.find_room(session, code)
    if state is None or room is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Room not found"
        )
    characters = await character_manager.list_characters(session, room.id)
    return RoomSnapshotResponse(
        **state,
        characters=[
            character_manager.character_as_dict(character) for character in characters
        ],
        game_state=await get_state(session, room.id),
    )


@router.post("/{code}/characters", response_model=CharacterResponse)
async def create_character(
    code: str,
    request: CharacterCreateRequest,
    session: AsyncSession = Depends(get_session),
) -> CharacterResponse:
    room = await character_manager.find_room(session, code)
    if room is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Room not found"
        )
    try:
        character = await character_manager.create_character(
            session,
            room,
            name=request.name,
            stats=request.stats,
            inventory=request.inventory,
            player_token=request.player_token,
        )
    except ValueError as error:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST, detail=str(error)
        ) from error
    return CharacterResponse.model_validate(
        character_manager.character_as_dict(character)
    )


@router.patch("/{code}/characters/{character_id}", response_model=CharacterResponse)
async def update_character(
    code: str,
    character_id: str,
    request: CharacterUpdateRequest,
    session: AsyncSession = Depends(get_session),
) -> CharacterResponse:
    room = await character_manager.find_room(session, code)
    if room is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Room not found"
        )
    try:
        character = await character_manager.update_character(
            session,
            room,
            character_id,
            name=request.name,
            stats=request.stats,
            inventory=request.inventory,
        )
    except ValueError as error:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail=str(error)
        ) from error
    return CharacterResponse.model_validate(
        character_manager.character_as_dict(character)
    )


@router.patch("/{code}/board")
async def patch_board(
    code: str,
    request: BoardUpdateRequest,
    session: AsyncSession = Depends(get_session),
) -> dict:
    room = await character_manager.find_room(session, code)
    if room is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Room not found"
        )
    board_state = await update_board(
        session,
        room.id,
        map_url=request.map_url,
        tokens={key: token.model_dump() for key, token in request.tokens.items()}
        if request.tokens is not None
        else None,
    )
    return board_state.get("board", {})


@router.get("/{code}/assets", response_model=list[AssetResponse])
async def get_assets(
    code: str,
    session: AsyncSession = Depends(get_session),
) -> list[AssetResponse]:
    room = await character_manager.find_room(session, code)
    if room is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Room not found"
        )
    assets = await list_assets(session, room.id)
    return [AssetResponse.model_validate(asset_as_dict(asset)) for asset in assets]


@router.post("/{code}/assets/prepare", response_model=list[AssetResponse])
async def prepare_assets(
    code: str,
    session: AsyncSession = Depends(get_session),
) -> list[AssetResponse]:
    room = await character_manager.find_room(session, code)
    if room is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Room not found"
        )
    assets = await prepare_preset_assets(session, room)
    return [AssetResponse.model_validate(asset_as_dict(asset)) for asset in assets]
