from contextvars import ContextVar

from langchain_core.tools import tool

from app.engine.systems.dnd5e import DnD5eSystem

_system = DnD5eSystem()
_current_stats: ContextVar[dict | None] = ContextVar("current_character_stats", default=None)


def set_character_stats(stats: dict) -> None:
    _current_stats.set(dict(stats))


@tool
def skill_check(character_name: str, skill: str, difficulty: int) -> str:
    """Perform a skill check with d20 plus the character modifier against a difficulty."""
    result = _system.skill_check(
        stats=_current_stats.get() or {},
        skill=skill,
        difficulty=difficulty,
    )
    status = "SUCCESS" if result.success else "FAILURE"
    return (
        f"{character_name} - {skill}: {result.roll} + {result.modifier} "
        f"= {result.total} vs DC {difficulty} -> {status}"
    )
