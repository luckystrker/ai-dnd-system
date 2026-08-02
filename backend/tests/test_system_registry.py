from app.engine.registry import system_registry
from app.engine.systems.dnd5e import DnD5eSystem
from app.services.system_service import system_summary


def test_registry_contains_dnd_and_story_systems():
    assert system_registry.available() == ["dnd5e", "story"]
    assert isinstance(system_registry.create("DND5E"), DnD5eSystem)


def test_dnd_character_template_and_tone():
    summary = system_summary("dnd5e")
    assert summary["character_template"]["stats"]["max_hp"] == 10
    assert "fantasy" in summary["tone"]
