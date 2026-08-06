import { defineTool } from "eve/tools";
import { z } from "zod";

import { canActForCharacter } from "../lib/campaigns/access.ts";
import { emptyCombatOrder, nextCombatant, type CombatOrder } from "../lib/engine/combat.ts";
import { rollDice } from "../lib/engine/dnd5e.ts";
import { gameState, type GameState } from "../lib/memory.ts";

function parseDice(spec: string): { count: number; sides: number } {
  const match = /^(\d+)d(\d+)$/i.exec(spec.trim());
  if (!match) throw new Error(`Invalid dice spec "${spec}", expected e.g. "2d6".`);
  return { count: Number(match[1]), sides: Number(match[2]) };
}

function combatOf(state: GameState): CombatOrder {
  return state.combat ?? emptyCombatOrder();
}

function notStarted(): string {
  return "Бой не начат. Сначала вызови initiative со всеми участниками (партия и враги с HP и КД).";
}

/** Строка текущего хода вида «Раунд 2. Ход: Инокентий (5).». */
function currentTurnText(combat: CombatOrder): string {
  const entry = combat.order[combat.current];
  if (!combat.started || combat.current < 0 || !entry) return "";
  return `Раунд ${combat.round}. Ход: ${entry.name} (${entry.total}).`;
}

function statusText(state: GameState): string {
  const combat = combatOf(state);
  if (!combat.started || combat.order.length === 0) return notStarted();
  const lines = combat.order.map((entry, index) => {
    const marker = index === combat.current ? "→ " : "  ";
    const enemy = state.enemies.find((candidate) => candidate.name.toLowerCase() === entry.name.toLowerCase());
    const hpNote =
      entry.side === "enemy"
        ? `, HP ${enemy?.hp ?? entry.hp ?? "?"}, КД ${enemy?.ac ?? entry.ac ?? "?"}`
        : "";
    return `${marker}${index + 1}. ${entry.name} (${entry.total})${hpNote}`;
  });
  const turn = currentTurnText(combat);
  return [`Порядок ходов:`, ...lines, turn].filter(Boolean).join("\n");
}

/** Жив ли участник: враг — пока он в списке активных, партия — всегда. */
function aliveCheck(
  enemies: GameState["enemies"],
): (entry: { name: string; side: CombatOrder["order"][number]["side"] }) => boolean {
  return (entry) =>
    entry.side !== "enemy" || enemies.some((enemy) => enemy.name.toLowerCase() === entry.name.toLowerCase());
}

export default defineTool({
  description:
    "Resolve turn-based combat tracked by the initiative tool. " +
    "actions: attack — a party member attacks a named enemy (rolls d20+bonus vs enemy AC, applies damage, " +
    "removes defeated enemies, then advances to the next combatant's turn); " +
    "next — advance to the next combatant without an attack (after enemy turns, movement, etc.); " +
    "status — show the current order, enemy HP and whose turn it is; " +
    "end — end the combat and clear all enemies.",
  inputSchema: z
    .object({
      action: z.enum(["attack", "next", "status", "end"]),
      attacker: z.string().min(1).max(100).optional(),
      enemy: z.string().min(1).max(100).optional(),
      bonus: z.number().int().min(-10).max(20).default(0),
      damage_dice: z.string().min(1).max(20).default("1d8"),
    })
    .refine((value) => value.action !== "attack" || Boolean(value.attacker), {
      message: "attacker is required when action is attack",
      path: ["attacker"],
    })
    .refine((value) => value.action !== "attack" || Boolean(value.enemy), {
      message: "enemy is required when action is attack",
      path: ["enemy"],
    }),
  execute({ action, attacker, enemy, bonus, damage_dice }, ctx) {
    const state = gameState.get();
    const combat = combatOf(state);

    if (action === "status") return statusText(state);

    if (!combat.started || combat.order.length === 0) {
      return notStarted();
    }

    if (action === "end") {
      gameState.update((s) => ({ ...s, enemies: [], combat: emptyCombatOrder() }));
      return "Бой окончен: порядок ходов и враги сброшены.";
    }

    if (action === "next") {
      const advanced = nextCombatant(combat, aliveCheck(state.enemies));
      gameState.update((s) => ({ ...s, combat: advanced }));
      const turn = currentTurnText(advanced);
      return turn ? `Ход переходит дальше. ${turn}` : notStarted();
    }

    const partyMember = state.party.find(
      (entry) => entry.name.trim().toLowerCase() === attacker!.trim().toLowerCase(),
    );
    if (!partyMember) {
      const party = state.party.map((entry) => entry.name).join(", ") || "(партия пуста)";
      return `Персонаж «${attacker}» не найден в партии. Участники партии: ${party}.`;
    }
    const access = canActForCharacter(ctx, attacker!);
    if (!access.allowed) {
      return access.reason ?? "Действие от имени этого персонажа запрещено.";
    }

    const target = state.enemies.find((candidate) => candidate.name.toLowerCase() === enemy!.toLowerCase());
    if (!target) {
      const active = state.enemies
        .map((candidate) => `${candidate.name} (${candidate.hp} HP, КД ${candidate.ac})`)
        .join(", ");
      return `Нет врага «${enemy}». Активные враги: ${active}.`;
    }

    const roll = rollDice(20, 1).total;
    const hit = roll + bonus >= target.ac;
    const dice = parseDice(damage_dice);
    const damage = hit ? rollDice(dice.sides, dice.count).total : 0;

    let resultLine: string;
    let defeated = false;
    if (hit) {
      const remaining = Math.max(0, target.hp - damage);
      defeated = remaining === 0;
      const hpNote = defeated
        ? `${target.name} повержен!`
        : `У ${target.name} осталось ${remaining} HP.`;
      resultLine = `${attacker} атакует ${target.name}: d20 = ${roll} + ${bonus} против КД ${target.ac} → попадание, урон ${damage}. ${hpNote}`;
    } else {
      resultLine = `${attacker} атакует ${target.name}: d20 = ${roll} + ${bonus} против КД ${target.ac} → промах.`;
    }

    let nextEnemies = state.enemies;
    if (hit) {
      if (defeated) {
        nextEnemies = state.enemies.filter((candidate) => candidate.name !== target.name);
      } else {
        nextEnemies = state.enemies.map((candidate) =>
          candidate.name === target.name ? { ...candidate, hp: Math.max(0, target.hp - damage) } : candidate,
        );
      }
    }

    if (hit && nextEnemies.length === 0) {
      gameState.update((s) => ({ ...s, enemies: [], combat: emptyCombatOrder() }));
      return `${resultLine}\nВсе враги повержены. Бой окончен.`;
    }

    const advanced = nextCombatant(combat, aliveCheck(nextEnemies));
    gameState.update((s) => ({ ...s, enemies: nextEnemies, combat: advanced }));
    const turn = currentTurnText(advanced);
    return turn ? `${resultLine}\n${turn}` : resultLine;
  },
});
