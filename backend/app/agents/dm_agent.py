import uuid
from collections.abc import AsyncIterator
from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_openai import ChatOpenAI
from langgraph.graph import END, MessagesState, StateGraph
from langgraph.prebuilt import ToolNode

from app.agents.prompts.dm_system import DM_SYSTEM_PROMPT
from app.agents.tools.checks import set_character_stats, skill_check
from app.agents.tools.combat import combat_action, set_combat_room
from app.agents.tools.dice import roll_dice
from app.agents.tools.memory import recall, set_memory_room
from app.agents.tools.npc import consult_npc, set_npc_room
from app.config import settings
from app.schemas.ws_messages import Event
from app.services.event_bus import event_bus

tools = [roll_dice, skill_check, recall, consult_npc, combat_action]
llm: Any | None = None


def _get_llm() -> Any:
    global llm
    if llm is None:
        llm = ChatOpenAI(
            base_url=settings.llm_base_url,
            api_key=settings.llm_api_key,
            model=settings.llm_model,
            streaming=True,
        ).bind_tools(tools)
    return llm


def _build_graph(model: Any | None = None, system_prompt: str = DM_SYSTEM_PROMPT):
    model = _get_llm() if model is None else model

    async def agent_node(state: MessagesState):
        messages = [SystemMessage(content=system_prompt)] + state["messages"]
        response = await model.ainvoke(messages)
        return {"messages": [response]}

    def should_continue(state: MessagesState):
        last_message = state["messages"][-1]
        if getattr(last_message, "tool_calls", None):
            return "tools"
        return END

    graph = StateGraph(MessagesState)
    graph.add_node("agent", agent_node)
    graph.add_node("tools", ToolNode(tools))
    graph.set_entry_point("agent")
    graph.add_conditional_edges("agent", should_continue, {"tools": "tools", END: END})
    graph.add_edge("tools", "agent")
    return graph.compile()


def _content_to_text(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for item in content:
            if isinstance(item, str):
                parts.append(item)
            elif isinstance(item, dict) and isinstance(item.get("text"), str):
                parts.append(item["text"])
        return "".join(parts)
    return ""


def _tool_output_to_text(output: Any) -> str:
    if isinstance(output, str):
        return output
    if hasattr(output, "content"):
        return _content_to_text(output.content)
    return str(output)


async def _stream_graph(graph: Any, input_messages: dict) -> AsyncIterator[dict]:
    async for event in graph.astream_events(input_messages, version="v2"):
        yield event


async def run_dm_agent(
    room_id: str,
    player_message: str,
    character_stats: dict | None = None,
    context_text: str | None = None,
    database_room_id: str | None = None,
) -> str | None:
    """Run the DM graph and publish token/tool events to a room."""
    set_character_stats(character_stats or {})
    tool_room_id = database_room_id or room_id
    set_memory_room(tool_room_id)
    set_npc_room(tool_room_id)
    set_combat_room(tool_room_id)
    message_id = str(uuid.uuid4())

    try:
        system_prompt = DM_SYSTEM_PROMPT
        if context_text:
            system_prompt += f"\n\n## Current Room Context\n{context_text}"
        graph = (
            _build_graph()
            if not context_text
            else _build_graph(system_prompt=system_prompt)
        )
        input_messages = {"messages": [HumanMessage(content=player_message)]}
        full_text = ""
        async for event in _stream_graph(graph, input_messages):
            event_name = event.get("event")
            event_data = event.get("data") or {}

            if event_name == "on_chat_model_stream":
                chunk = event_data.get("chunk")
                token = _content_to_text(getattr(chunk, "content", ""))
                if token:
                    full_text += token
                    await event_bus.publish(
                        room_id,
                        Event(type="dm_token", payload={"token": token}),
                    )

            elif event_name == "on_tool_end":
                output = event_data.get("output", "")
                tool_name = event.get("name", "unknown")
                await event_bus.publish(
                    room_id,
                    Event(
                        type="dice_roll",
                        payload={
                            "tool": tool_name,
                            "result": _tool_output_to_text(output),
                        },
                    ),
                )

        await event_bus.publish(
            room_id,
            Event(
                type="dm_complete",
                payload={"message_id": message_id, "full_text": full_text},
            ),
        )
        return full_text
    except Exception as error:  # noqa: BLE001
        await event_bus.publish(
            room_id,
            Event(
                type="error",
                payload={"message": f"DM error: {error}"},
            ),
        )
        return None
