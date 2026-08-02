from datetime import UTC, datetime
from typing import Any

from pydantic import BaseModel, Field


class WSMessage(BaseModel):
    type: str
    payload: dict[str, Any] = Field(default_factory=dict)


class Event(BaseModel):
    type: str
    payload: dict[str, Any] = Field(default_factory=dict)
    timestamp: datetime = Field(default_factory=lambda: datetime.now(UTC))
    sender_id: str | None = None
