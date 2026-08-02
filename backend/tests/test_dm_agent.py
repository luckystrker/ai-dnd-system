from unittest.mock import AsyncMock

import pytest
from langchain_core.messages import AIMessage, AIMessageChunk, HumanMessage

from app.agents import dm_agent
from app.schemas.ws_messages import Event
from app.services.event_bus import EventBus


class FakeModel:
    async def ainvoke(self, _messages):
        return AIMessage(content="The torches flicker as the door opens.")


class FakeStreamGraph:
    async def astream_events(self, _input, version):
        assert version == "v2"
        yield {
            "event": "on_chat_model_stream",
            "data": {"chunk": AIMessageChunk(content="A hidden stairway appears.")},
        }
        yield {
            "event": "on_tool_end",
            "name": "roll_dice",
            "data": {"output": "Rolled 1d20: [18] = 18"},
        }


def test_graph_invokes_model():
    import asyncio

    graph = dm_agent._build_graph(FakeModel())
    result = asyncio.run(
        graph.ainvoke({"messages": [HumanMessage(content="look around")]})
    )
    assert result["messages"][-1].content == "The torches flicker as the door opens."


@pytest.mark.asyncio
async def test_run_dm_agent_publishes_stream_and_tool_events(monkeypatch):
    bus = EventBus()
    websocket = AsyncMock()
    bus.subscribe("room1", websocket)
    monkeypatch.setattr(dm_agent, "event_bus", bus)
    monkeypatch.setattr(dm_agent, "_build_graph", lambda: FakeStreamGraph())

    await dm_agent.run_dm_agent("room1", "search the room")

    messages = [call.args[0] for call in websocket.send_text.call_args_list]
    assert any('"dm_token"' in message for message in messages)
    assert any('"dice_roll"' in message for message in messages)
    assert any('"dm_complete"' in message for message in messages)


def test_event_model_has_fresh_timestamp():
    first = Event(type="one")
    second = Event(type="two")
    assert first.timestamp <= second.timestamp
