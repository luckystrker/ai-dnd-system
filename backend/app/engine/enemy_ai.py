from dataclasses import dataclass

from app.engine.combat import CombatAction, CombatState


@dataclass(frozen=True)
class EnemyDecision:
    action: CombatAction
    reason: str


def choose_enemy_action(state: CombatState, enemy_id: str) -> EnemyDecision:
    enemy = state.combatants.get(enemy_id)
    if enemy is None or enemy.side != "enemy":
        raise ValueError("Enemy combatant not found")
    if enemy.hp <= 0:
        raise ValueError("Defeated enemies cannot act")

    if enemy.hp / enemy.max_hp < 0.2:
        return EnemyDecision(
            action=CombatAction(actor_id=enemy_id, action_type="flee"),
            reason="The enemy is badly wounded and tries to retreat.",
        )

    targets = [
        combatant
        for combatant in state.combatants.values()
        if combatant.side == "party" and combatant.hp > 0
    ]
    if not targets:
        raise ValueError("No living party target")
    target = min(targets, key=lambda combatant: (combatant.hp, combatant.max_hp))
    return EnemyDecision(
        action=CombatAction(actor_id=enemy_id, target_id=target.id),
        reason=f"The enemy attacks the most vulnerable nearby target: {target.name}.",
    )
