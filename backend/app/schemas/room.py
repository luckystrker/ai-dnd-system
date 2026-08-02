from typing import Any

from pydantic import BaseModel, ConfigDict, Field, field_validator


class CreateRoomRequest(BaseModel):
    display_name: str = Field(min_length=1, max_length=50)
    system: str = Field(default="dnd5e", min_length=1, max_length=40)

    @field_validator("display_name")
    @classmethod
    def validate_display_name(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("display_name must not be blank")
        return value

    @field_validator("system")
    @classmethod
    def normalize_system(cls, value: str) -> str:
        value = value.strip().lower()
        if not value:
            raise ValueError("system must not be blank")
        return value


class JoinRoomRequest(BaseModel):
    code: str = Field(min_length=6, max_length=6)
    display_name: str = Field(min_length=1, max_length=50)
    player_token: str | None = None

    @field_validator("code")
    @classmethod
    def normalize_code(cls, value: str) -> str:
        return value.strip().upper()

    @field_validator("display_name")
    @classmethod
    def validate_display_name(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("display_name must not be blank")
        return value


class CreateRoomResponse(BaseModel):
    room_id: str
    code: str
    player_token: str


class JoinRoomResponse(BaseModel):
    room_id: str
    player_token: str


class PlayerInfo(BaseModel):
    id: str
    display_name: str


class RoomStateResponse(BaseModel):
    room_id: str
    code: str
    status: str
    players: list[PlayerInfo]


class CharacterCreateRequest(BaseModel):
    name: str = Field(min_length=1, max_length=100)
    stats: dict[str, Any] = Field(default_factory=dict)
    inventory: list[Any] = Field(default_factory=list)
    player_token: str | None = None

    @field_validator("name")
    @classmethod
    def validate_name(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("name must not be blank")
        return value


class CharacterUpdateRequest(BaseModel):
    name: str | None = Field(default=None, min_length=1, max_length=100)
    stats: dict[str, Any] | None = None
    inventory: list[Any] | None = None


class CharacterResponse(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: str
    room_id: str
    player_id: str
    name: str
    stats: dict[str, Any]
    inventory: list[Any]


class BoardToken(BaseModel):
    x: float = Field(ge=0, le=1)
    y: float = Field(ge=0, le=1)
    label: str = Field(default="", max_length=100)
    kind: str = Field(default="character", max_length=30)


class BoardUpdateRequest(BaseModel):
    map_url: str | None = Field(default=None, max_length=2000)
    tokens: dict[str, BoardToken] | None = None


class RoomSnapshotResponse(RoomStateResponse):
    characters: list[CharacterResponse]
    game_state: dict[str, Any]


class AssetResponse(BaseModel):
    id: str
    room_id: str
    kind: str
    name: str
    uri: str
    metadata: dict[str, Any]
