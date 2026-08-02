from typing import Any

DND5E_STARTER_PRESET: dict[str, Any] = {
    "system": "dnd5e",
    "title": "The Lantern Below",
    "tone": "high fantasy adventure",
    "locations": [
        {
            "id": "embercross",
            "name": "Embercross",
            "description": "A trade town built around a bridge of black volcanic glass.",
        },
        {
            "id": "lantern_mine",
            "name": "The Lantern Mine",
            "description": "An abandoned silver mine where blue lights move below the shafts.",
        },
        {
            "id": "old_watch",
            "name": "The Old Watch",
            "description": "A ruined fortress overlooking the road and the valley beyond.",
        },
    ],
    "factions": [
        {
            "id": "lantern_guild",
            "name": "Lantern Guild",
            "goal": "Keep the mine sealed.",
        },
        {
            "id": "ash_court",
            "name": "Ash Court",
            "goal": "Recover an ancient ember relic.",
        },
        {
            "id": "roadwardens",
            "name": "Roadwardens",
            "goal": "Protect travelers through the valley.",
        },
    ],
    "npcs": [
        {
            "id": "mara_voss",
            "name": "Mara Voss",
            "personality": "Practical, warm, and suspicious of easy heroics.",
            "goals": ["Protect Embercross", "Find her missing brother"],
        },
        {
            "id": "sir_calder",
            "name": "Sir Calder",
            "personality": "Formal and exhausted, hiding a dangerous oath.",
            "goals": ["Keep the Old Watch standing", "Atone for a failed patrol"],
        },
    ],
}
