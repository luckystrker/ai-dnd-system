import asyncio

from app.memory.campaign import DMContext
from app.memory.vector_store import DeterministicEmbeddingProvider
from app.memory.visibility import Visibility, is_visible_to
from app.services.game_state import deep_merge


def test_deep_merge_preserves_unmodified_nested_state():
    original = {"party": {"hp": 8, "inventory": ["torch"]}, "weather": "clear"}
    merged = deep_merge(original, {"party": {"hp": 6}})

    assert merged == {"party": {"hp": 6, "inventory": ["torch"]}, "weather": "clear"}
    assert original["party"]["hp"] == 8


def test_visibility_policy():
    assert is_visible_to(Visibility.PUBLIC, observer_id="player")
    assert is_visible_to(Visibility.PARTY, audience="player")
    assert not is_visible_to(Visibility.PARTY, audience="npc")
    assert is_visible_to(
        Visibility.PRIVATE,
        observer_id="npc-1",
        visibility_target="npc-1",
    )
    assert not is_visible_to(
        Visibility.PRIVATE,
        observer_id="npc-2",
        visibility_target="npc-1",
    )
    assert not is_visible_to(Visibility.DM_ONLY, observer_id="player")
    assert is_visible_to(
        Visibility.REGIONAL,
        observer_region="market",
        tags=["region:market"],
    )


def test_deterministic_embedding_is_normalized_and_repeatable():
    provider = DeterministicEmbeddingProvider(dimensions=16)
    first = asyncio.run(provider.embed("the old keep"))
    second = asyncio.run(provider.embed("the old keep"))

    assert first == second
    assert len(first) == 16
    assert round(sum(value * value for value in first), 6) == 1.0


def test_dm_context_render_is_bounded():
    context = DMContext(
        state={"hp": 10},
        recent=[{"type": "travel", "payload": {"text": "x" * 100}}],
        recalled=[],
    )

    rendered = context.render(max_chars=80)
    assert len(rendered) == 80
    assert "Current Game State" in rendered
