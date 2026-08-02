import random
import uuid
from collections.abc import Callable, Iterable
from typing import Literal

from pydantic import BaseModel, Field

Side = Literal["party", "enemy", "neutral"]


class Combatant(BaseModel):
    id: str
    name: str
    side: Side
    hp: int = Field(ge=0)
    max_hp: int = Field(gt=0)
    ac: int = Field(default=10, ge=0)
    initiative: int = 0
    conditions: list[str] = Field(default_factory=list)
    is_boss: bool = False


class CombatLogEntry(BaseModel):
    type: str
    actor_id: str | None = None
    target_id: str | None = None
    data: dict = Field(default_factory=dict)


class CombatState(BaseModel):
    combat_id: str = Field(default_factory=lambda: str(uuid.uuid4()))
    status: Literal["active", "victory", "defeat", "ended"] = "active"
    round: int = Field(default=1, ge=1)
    turn_index: int = Field(default=0, ge=0)
    turn_order: list[str] = Field(default_factory=list)
    combatants: dict[str, Combatant] = Field(default_factory=dict)
    log: list[CombatLogEntry] = Field(default_factory=list)


class AttackResult(BaseModel):
    roll: int
    attack_bonus: int
    total: int
    target_ac: int
    hit: bool
    critical: bool = False
    damage: int = 0
    target_hp: int
    defeated: bool = False


class SpellResult(BaseModel):
    roll: int
    save_modifier: int
    total: int
    difficulty: int
    success: bool
    damage: int
    target_hp: int
    condition: str | None = None
    defeated: bool = False


class CombatAction(BaseModel):
    actor_id: str
    action_type: Literal["attack", "spell", "dodge", "flee"] = "attack"
    target_id: str | None = None
    attack_bonus: int = 0
    damage_sides: int = Field(default=6, ge=1)
    damage_count: int = Field(default=1, ge=1, le=20)
    damage_bonus: int = 0
    save_dc: int = Field(default=12, ge=1)
    save_modifier: int = 0
    condition: str | None = Field(default=None, max_length=50)


class CombatStateMachine:
    def __init__(
        self,
        state: CombatState,
        *,
        rng: random.Random | None = None,
        roll_d20: Callable[[], int] | None = None,
    ) -> None:
        self.state = state
        self.rng = rng or random.Random()
        self._roll_d20 = roll_d20 or (lambda: self.rng.randint(1, 20))

    @classmethod
    def start(
        cls,
        combatants: Iterable[Combatant],
        *,
        rng: random.Random | None = None,
    ) -> "CombatStateMachine":
        rng = rng or random.Random()
        roster = list(combatants)
        if not roster:
            raise ValueError("Combat requires at least one combatant")
        if not any(combatant.side == "party" for combatant in roster):
            raise ValueError("Combat requires at least one party combatant")
        if not any(combatant.side == "enemy" for combatant in roster):
            raise ValueError("Combat requires at least one enemy combatant")

        prepared: dict[str, Combatant] = {}
        for combatant in roster:
            if combatant.id in prepared:
                raise ValueError(f"Duplicate combatant id: {combatant.id}")
            combatant = combatant.model_copy(deep=True)
            combatant.initiative = rng.randint(1, 20)
            prepared[combatant.id] = combatant
        order = sorted(
            prepared.values(),
            key=lambda item: (item.initiative, item.side == "party"),
            reverse=True,
        )
        state = CombatState(
            turn_order=[combatant.id for combatant in order],
            combatants=prepared,
        )
        machine = cls(state, rng=rng)
        machine._skip_defeated()
        return machine

    @classmethod
    def from_state(
        cls,
        state: CombatState | dict,
        *,
        rng: random.Random | None = None,
    ) -> "CombatStateMachine":
        parsed = (
            state
            if isinstance(state, CombatState)
            else CombatState.model_validate(state)
        )
        return cls(parsed, rng=rng)

    @property
    def current(self) -> Combatant | None:
        if self.state.status != "active" or not self.state.turn_order:
            return None
        return self.state.combatants.get(self.state.turn_order[self.state.turn_index])

    def available_actions(self, actor_id: str) -> list[str]:
        actor = self.state.combatants.get(actor_id)
        if self.state.status != "active" or actor is None or actor.hp <= 0:
            return []
        if self.current is None or self.current.id != actor_id:
            return []
        actions = ["attack", "spell", "dodge", "flee"]
        if actor.side == "neutral":
            return ["dodge", "flee"]
        return actions

    def resolve_attack(
        self,
        actor_id: str,
        target_id: str,
        *,
        attack_bonus: int = 0,
        damage_sides: int = 6,
        damage_count: int = 1,
        damage_bonus: int = 0,
    ) -> AttackResult:
        self._require_turn(actor_id)
        target = self._target(target_id)
        if target.hp <= 0:
            raise ValueError("Target is already defeated")
        if target.side == self.state.combatants[actor_id].side:
            raise ValueError("Combatants cannot attack an ally")
        if damage_sides < 1 or damage_count < 1:
            raise ValueError("Damage dice must be positive")

        roll = self._roll_d20()
        total = roll + attack_bonus
        critical = roll == 20
        hit = critical or (roll != 1 and total >= target.ac)
        damage = 0
        if hit:
            damage = sum(self.rng.randint(1, damage_sides) for _ in range(damage_count))
            if critical:
                damage += sum(
                    self.rng.randint(1, damage_sides) for _ in range(damage_count)
                )
            damage += damage_bonus
            damage = max(0, damage)
            target.hp = max(0, target.hp - damage)

        result = AttackResult(
            roll=roll,
            attack_bonus=attack_bonus,
            total=total,
            target_ac=target.ac,
            hit=hit,
            critical=critical,
            damage=damage,
            target_hp=target.hp,
            defeated=target.hp == 0,
        )
        self.state.log.append(
            CombatLogEntry(
                type="attack",
                actor_id=actor_id,
                target_id=target_id,
                data=result.model_dump(),
            )
        )
        self._update_status()
        return result

    def resolve_action(self, action: CombatAction) -> AttackResult | SpellResult | None:
        self._require_turn(action.actor_id)
        if action.action_type == "attack":
            if not action.target_id:
                raise ValueError("Attack requires a target")
            result = self.resolve_attack(
                action.actor_id,
                action.target_id,
                attack_bonus=action.attack_bonus,
                damage_sides=action.damage_sides,
                damage_count=action.damage_count,
                damage_bonus=action.damage_bonus,
            )
        elif action.action_type == "spell":
            if not action.target_id:
                raise ValueError("Spell requires a target")
            result = self.resolve_spell(
                action.actor_id,
                action.target_id,
                save_dc=action.save_dc,
                save_modifier=action.save_modifier,
                damage_sides=action.damage_sides,
                damage_count=action.damage_count,
                damage_bonus=action.damage_bonus,
                condition=action.condition,
            )
        elif action.action_type == "dodge":
            actor = self.state.combatants[action.actor_id]
            if "dodging" not in actor.conditions:
                actor.conditions.append("dodging")
            self.state.log.append(
                CombatLogEntry(type="dodge", actor_id=action.actor_id)
            )
            result = None
        else:
            actor = self.state.combatants[action.actor_id]
            actor.conditions.append("fled")
            self.state.log.append(CombatLogEntry(type="flee", actor_id=action.actor_id))
            result = None

        if self.state.status == "active":
            self.advance_turn()
        return result

    def resolve_spell(
        self,
        actor_id: str,
        target_id: str,
        *,
        save_dc: int = 12,
        save_modifier: int = 0,
        damage_sides: int = 6,
        damage_count: int = 1,
        damage_bonus: int = 0,
        condition: str | None = None,
    ) -> SpellResult:
        self._require_turn(actor_id)
        target = self._target(target_id)
        if target.hp <= 0:
            raise ValueError("Target is already defeated")
        if target.side == self.state.combatants[actor_id].side:
            raise ValueError("Combatants cannot target an ally")
        if damage_sides < 1 or damage_count < 1:
            raise ValueError("Spell damage dice must be positive")

        roll = self._roll_d20()
        total = roll + save_modifier
        success = total >= save_dc
        damage = (
            sum(self.rng.randint(1, damage_sides) for _ in range(damage_count))
            + damage_bonus
        )
        damage = max(0, damage // 2 if success else damage)
        target.hp = max(0, target.hp - damage)
        applied_condition = None
        if (
            condition
            and not success
            and target.hp > 0
            and condition not in target.conditions
        ):
            target.conditions.append(condition)
            applied_condition = condition

        result = SpellResult(
            roll=roll,
            save_modifier=save_modifier,
            total=total,
            difficulty=save_dc,
            success=success,
            damage=damage,
            target_hp=target.hp,
            condition=applied_condition,
            defeated=target.hp == 0,
        )
        self.state.log.append(
            CombatLogEntry(
                type="spell",
                actor_id=actor_id,
                target_id=target_id,
                data=result.model_dump(),
            )
        )
        self._update_status()
        return result

    def advance_turn(self) -> Combatant | None:
        if self.state.status != "active":
            return None
        previous = self.current
        if previous and "dodging" in previous.conditions:
            previous.conditions.remove("dodging")
        for _ in range(len(self.state.turn_order)):
            self.state.turn_index = (self.state.turn_index + 1) % len(
                self.state.turn_order
            )
            if self.state.turn_index == 0:
                self.state.round += 1
            candidate = self.current
            if candidate and candidate.hp > 0 and "fled" not in candidate.conditions:
                return candidate
        self._update_status()
        return self.current

    def end(self) -> None:
        self.state.status = "ended"
        self.state.log.append(CombatLogEntry(type="combat_ended"))

    def _target(self, target_id: str) -> Combatant:
        target = self.state.combatants.get(target_id)
        if target is None:
            raise ValueError(f"Combatant {target_id} not found")
        return target

    def _require_turn(self, actor_id: str) -> None:
        actor = self._target(actor_id)
        if actor.hp <= 0:
            raise ValueError("Defeated combatants cannot act")
        if self.state.status != "active":
            raise ValueError("Combat is not active")
        if self.current is None or self.current.id != actor_id:
            raise ValueError("It is not this combatant's turn")

    def _skip_defeated(self) -> None:
        if self.current and self.current.hp <= 0:
            self.advance_turn()

    def _update_status(self) -> None:
        party_alive = any(
            combatant.side == "party"
            and combatant.hp > 0
            and "fled" not in combatant.conditions
            for combatant in self.state.combatants.values()
        )
        enemies_alive = any(
            combatant.side == "enemy"
            and combatant.hp > 0
            and "fled" not in combatant.conditions
            for combatant in self.state.combatants.values()
        )
        if not enemies_alive:
            self.state.status = "victory"
        elif not party_alive:
            self.state.status = "defeat"
