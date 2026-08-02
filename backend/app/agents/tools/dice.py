from langchain_core.tools import tool

from app.engine.systems.dnd5e import DnD5eSystem

_system = DnD5eSystem()


@tool
def roll_dice(sides: int, count: int = 1) -> str:
    """Roll dice. Specify sides and count, for example roll_dice(sides=20)."""
    result = _system.roll_dice(sides=sides, count=count)
    return f"Rolled {count}d{sides}: {result.rolls} = {result.total}"
