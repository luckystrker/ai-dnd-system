from collections.abc import Callable

from app.engine.rules_base import GameSystem
from app.engine.systems.dnd5e import DnD5eSystem
from app.engine.systems.story import StorySystem


class GameSystemRegistry:
    def __init__(self) -> None:
        self._factories: dict[str, Callable[[], GameSystem]] = {}

    def register(self, name: str, factory: Callable[[], GameSystem]) -> None:
        normalized = name.strip().lower()
        if not normalized:
            raise ValueError("System name must not be blank")
        self._factories[normalized] = factory

    def create(self, name: str) -> GameSystem:
        normalized = name.strip().lower()
        factory = self._factories.get(normalized)
        if factory is None:
            raise ValueError(f"Unknown game system: {name}")
        return factory()

    def available(self) -> list[str]:
        return sorted(self._factories)


system_registry = GameSystemRegistry()
system_registry.register("dnd5e", DnD5eSystem)
system_registry.register("story", StorySystem)
