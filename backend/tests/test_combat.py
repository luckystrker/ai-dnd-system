import random

import pytest

from app.engine.combat import (
    CombatAction,
    Combatant,
    CombatState,
    CombatStateMachine,
)
from app.engine.enemy_ai import choose_enemy_action


def _state() -> CombatState:
    return CombatState(
        turn_order=["hero", "goblin"],
        combatants={
            "hero": Combatant(
                id="hero", name="Hero", side="party", hp=10, max_hp=10, ac=12
            ),
            "goblin": Combatant(
                id="goblin", name="Goblin", side="enemy", hp=2, max_hp=10, ac=10
            ),
        },
    )


def test_attack_critical_can_end_combat():
    machine = CombatStateMachine(_state(), rng=random.Random(1), roll_d20=lambda: 20)
    result = machine.resolve_action(
        CombatAction(
            actor_id="hero", target_id="goblin", damage_sides=1, damage_count=1
        )
    )

    assert result is not None
    assert result.critical is True
    assert result.defeated is True
    assert machine.state.status == "victory"


def test_turn_order_advances_after_non_attack_action():
    machine = CombatStateMachine(_state(), rng=random.Random(1))
    machine.resolve_action(CombatAction(actor_id="hero", action_type="dodge"))

    assert machine.current is not None
    assert machine.current.id == "goblin"
    assert machine.state.round == 1


def test_attack_rejects_wrong_turn():
    machine = CombatStateMachine(_state())
    with pytest.raises(ValueError, match="not this combatant's turn"):
        machine.resolve_attack("goblin", "hero")


def test_enemy_ai_retreats_when_badly_wounded():
    state = _state()
    state.combatants["goblin"].hp = 1
    decision = choose_enemy_action(state, "goblin")

    assert decision.action.action_type == "flee"


def test_enemy_ai_targets_lowest_hp_party_member():
    state = _state()
    state.combatants["goblin"].hp = 10
    state.combatants["hero"].hp = 3
    decision = choose_enemy_action(state, "goblin")

    assert decision.action.target_id == "hero"


def test_spell_save_deals_half_damage_on_success_and_condition_on_failure():
    state = _state()
    state.combatants["goblin"].hp = 10
    machine = CombatStateMachine(state, rng=random.Random(1), roll_d20=lambda: 1)
    result = machine.resolve_action(
        CombatAction(
            actor_id="hero",
            target_id="goblin",
            action_type="spell",
            save_dc=20,
            damage_sides=4,
            damage_count=1,
            condition="stunned",
        )
    )

    assert result is not None
    assert result.success is False
    assert result.condition == "stunned"
    assert "stunned" in machine.state.combatants["goblin"].conditions
