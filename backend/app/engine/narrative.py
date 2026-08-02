NARRATIVE_TONE_ADDONS: dict[str, str] = {
    "dnd5e": "Use high-fantasy imagery, heroic choices, dungeons, dragons, and concise casual narration.",
    "story": "Prioritize character voice, consequences, and collaborative fiction over fixed rules.",
}


def narrative_addon(system_name: str) -> str:
    return NARRATIVE_TONE_ADDONS.get(
        system_name.strip().lower(),
        "Keep the tone consistent with the room's selected game system.",
    )
