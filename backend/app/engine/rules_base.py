from abc import ABC, abstractmethod
from typing import Any

from pydantic import BaseModel


class DiceResult(BaseModel):
    rolls: list[int]
    total: int


class CheckResult(BaseModel):
    roll: int
    modifier: int
    total: int
    difficulty: int
    success: bool
    margin: int


class GameSystem(ABC):
    @abstractmethod
    def roll_dice(self, sides: int, count: int = 1) -> DiceResult:
        raise NotImplementedError

    @abstractmethod
    def skill_check(
        self,
        stats: dict,
        skill: str,
        difficulty: int,
        advantage: bool | None = None,
    ) -> CheckResult:
        raise NotImplementedError

    def roll_check(
        self,
        stats: dict,
        skill: str,
        difficulty: int,
        advantage: bool | None = None,
    ) -> CheckResult:
        return self.skill_check(stats, skill, difficulty, advantage)

    @abstractmethod
    def resolve_combat(
        self, state: dict[str, Any], action: dict[str, Any]
    ) -> dict[str, Any]:
        raise NotImplementedError

    @abstractmethod
    def get_character_template(self) -> dict[str, Any]:
        raise NotImplementedError

    @abstractmethod
    def get_narrative_tone(self) -> str:
        raise NotImplementedError
