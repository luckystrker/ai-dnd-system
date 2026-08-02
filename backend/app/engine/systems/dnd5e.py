import random
from typing import Any

from app.engine.combat import CombatAction, CombatStateMachine
from app.engine.rules_base import CheckResult, DiceResult, GameSystem

SKILL_ABILITY_MAP: dict[str, str] = {
    "athletics": "str",
    "acrobatics": "dex",
    "sleight_of_hand": "dex",
    "stealth": "dex",
    "arcana": "int",
    "history": "int",
    "investigation": "int",
    "nature": "int",
    "religion": "int",
    "animal_handling": "wis",
    "insight": "wis",
    "medicine": "wis",
    "perception": "wis",
    "survival": "wis",
    "deception": "cha",
    "intimidation": "cha",
    "performance": "cha",
    "persuasion": "cha",
}


class DnD5eSystem(GameSystem):
    def roll_dice(self, sides: int, count: int = 1) -> DiceResult:
        if sides < 1:
            raise ValueError("Dice must have at least one side")
        if count < 1:
            raise ValueError("Dice count must be at least one")
        if count > 100:
            raise ValueError("Dice count must not exceed 100")
        rolls = [random.randint(1, sides) for _ in range(count)]
        return DiceResult(rolls=rolls, total=sum(rolls))

    def skill_check(
        self,
        stats: dict,
        skill: str,
        difficulty: int,
        advantage: bool | None = None,
    ) -> CheckResult:
        ability = SKILL_ABILITY_MAP.get(skill.strip().lower(), "str")
        raw_score = stats.get(ability, 10)
        try:
            score = int(raw_score)
        except (TypeError, ValueError):
            score = 10
        modifier = (score - 10) // 2

        first_roll = random.randint(1, 20)
        if advantage is True:
            roll = max(first_roll, random.randint(1, 20))
        elif advantage is False:
            roll = min(first_roll, random.randint(1, 20))
        else:
            roll = first_roll

        total = roll + modifier
        return CheckResult(
            roll=roll,
            modifier=modifier,
            total=total,
            difficulty=difficulty,
            success=total >= difficulty,
            margin=total - difficulty,
        )

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
        return {
            "name": "",
            "class": "adventurer",
            "stats": {
                "str": 10,
                "dex": 10,
                "con": 10,
                "int": 10,
                "wis": 10,
                "cha": 10,
                "hp": 10,
                "max_hp": 10,
                "ac": 10,
            },
            "inventory": [],
        }

    def get_narrative_tone(self) -> str:
        return "high fantasy adventure with dungeons, dragons, heroic stakes, and casual rules"
