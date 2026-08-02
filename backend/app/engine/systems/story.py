from typing import Any

from app.engine.combat import CombatAction, CombatStateMachine
from app.engine.rules_base import CheckResult, DiceResult, GameSystem
from app.engine.systems.dnd5e import DnD5eSystem


class StorySystem(GameSystem):
    """Minimal non-D&D placeholder proving that systems are replaceable."""

    def __init__(self) -> None:
        self._dice = DnD5eSystem()

    def roll_dice(self, sides: int, count: int = 1) -> DiceResult:
        return self._dice.roll_dice(sides, count)

    def skill_check(
        self,
        stats: dict,
        skill: str,
        difficulty: int,
        advantage: bool | None = None,
    ) -> CheckResult:
        return self._dice.skill_check(stats, skill, difficulty, advantage)

    def resolve_combat(
        self, state: dict[str, Any], action: dict[str, Any]
    ) -> dict[str, Any]:
        machine = CombatStateMachine.from_state(state)
        result = machine.resolve_action(CombatAction.model_validate(action))
        return {
            "result": result.model_dump() if result else None,
            "state": machine.state.model_dump(),
        }

    def get_character_template(self) -> dict[str, Any]:
        return {"name": "", "stats": {}, "inventory": []}

    def get_narrative_tone(self) -> str:
        return "system-neutral collaborative storytelling with flexible mechanics"
