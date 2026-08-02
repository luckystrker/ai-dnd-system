from unittest.mock import AsyncMock

import pytest

from app.schemas.ws_messages import Event
from app.services.event_bus import EventBus


@pytest.fixture
def bus():
    return EventBus()


@pytest.mark.asyncio
async def test_publish_to_subscriber(bus):
    websocket = AsyncMock()
    bus.subscribe("room1", websocket)
    await bus.publish("room1", Event(type="test", payload={"msg": "hello"}))
    websocket.send_text.assert_called_once()
    assert '"test"' in websocket.send_text.call_args[0][0]


@pytest.mark.asyncio
async def test_publish_to_multiple(bus):
    websocket_one, websocket_two = AsyncMock(), AsyncMock()
    bus.subscribe("room1", websocket_one)
    bus.subscribe("room1", websocket_two)
    await bus.publish("room1", Event(type="test"))
    websocket_one.send_text.assert_called_once()
    websocket_two.send_text.assert_called_once()


@pytest.mark.asyncio
async def test_unsubscribe(bus):
    websocket = AsyncMock()
    bus.subscribe("room1", websocket)
    bus.unsubscribe("room1", websocket)
    await bus.publish("room1", Event(type="test"))
    websocket.send_text.assert_not_called()


@pytest.mark.asyncio
async def test_publish_empty_room(bus):
    await bus.publish("nope", Event(type="test"))
