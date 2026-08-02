from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_openai import ChatOpenAI

from app.agents.prompts.dm_system import DM_SYSTEM_PROMPT
from app.config import settings
from app.services.npc_lifecycle import NPCContext

_npc_llm: Any | None = None


def _get_npc_llm() -> Any:
    global _npc_llm
    if _npc_llm is None:
        _npc_llm = ChatOpenAI(
            base_url=settings.llm_base_url,
            api_key=settings.llm_api_key,
            model=settings.llm_model,
            streaming=False,
        )
    return _npc_llm


async def run_npc_agent(npc_context: NPCContext, interaction: str) -> str:
    system_prompt = (
        f"{DM_SYSTEM_PROMPT}\n\nYou are speaking as the NPC only. "
        "Do not reveal private memories or hidden game information.\n\n"
        f"{npc_context.render()}"
    )
    response = await _get_npc_llm().ainvoke(
        [
            SystemMessage(content=system_prompt),
            HumanMessage(content=interaction),
        ]
    )
    content = response.content
    if isinstance(content, str):
        return content
    return str(content)
