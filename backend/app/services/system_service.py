from typing import Any

from app.engine.narrative import narrative_addon
from app.engine.registry import system_registry
from app.engine.rules_base import GameSystem


def get_system(name: str) -> GameSystem:
    return system_registry.create(name)


def system_summary(name: str) -> dict[str, Any]:
    system = get_system(name)
    return {
        "id": name,
        "tone": system.get_narrative_tone(),
        "narrative_addon": narrative_addon(name),
        "character_template": system.get_character_template(),
    }
