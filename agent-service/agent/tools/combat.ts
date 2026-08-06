import { defineTool } from "eve/tools";
import { z } from "zod";

import { canActForCharacter } from "../lib/campaigns/access.ts";
import { rollDice } from "../lib/engine/dnd5e";
import { gameState } from "../lib/memory";

function parseDice(spec: string): { count: number; sides: number } {
  const match = /^(\d+)d(\d+)$/i.exec(spec.trim());
  if (!match) throw new Error(`Invalid dice spec "${spec}", expected e.g. "2d6".`);
  return { count: Number(match[1]), sides: Number(match[2]) };
}

export default defineTool({
  description:
    "Resolve an action in turn-based combat: attack a named enemy or end combat. " +
    "Tracks enemy HP and AC in the current session. Use only together with the game state. " +
    "`enemy` is required only for action=attack; omit it when ending combat.",
  inputSchema: z
    .object({
      action: z.enum(["attack", "end"]),
      attacker: z.string().min(1).max(100),
      enemy: z.string().min(1).max(100).optional(),
      bonus: z.number().int().min(-10).max(20).default(0),
      damage_dice: z.string().min(1).max(20).default("1d8"),
    })
    .refine((value) => value.action !== "attack" || Boolean(value.enemy), {
      message: "enemy is required when action is attack",
      path: ["enemy"],
    }),
  execute({ action, attacker, enemy, bonus, damage_dice }, ctx) {
    const state = gameState.get();
    if (state.enemies.length === 0) {
      return "No combat is in progress. Narrate an encounter first to start one.";
    }

    const partyMember = state.party.find(
      (entry) => entry.name.trim().toLowerCase() === attacker.trim().toLowerCase(),
    );
    if (!partyMember) {
      const party = state.party.map((entry) => entry.name).join(", ") || "(партия пуста)";
      return `Персонаж «${attacker}» не найден в партии. Участники партии: ${party}.`;
    }
    const access = canActForCharacter(ctx, attacker);
    if (!access.allowed) {
      return access.reason ?? "Действие от имени этого персонажа запрещено.";
    }

    if (action === "end") {
      gameState.update((s) => ({ ...s, enemies: [], turn: 0 }));
      return `${attacker} and allies end the combat and leave the field.`;
    }

    const target = state.enemies.find(
      (e) => e.name.toLowerCase() === enemy!.toLowerCase(),
    );
    if (!target) {
      const active = state.enemies
        .map((e) => `${e.name} (${e.hp} HP, AC ${e.ac})`)
        .join(", ");
      return `No enemy named "${enemy}". Active enemies: ${active}.`;
    }

    const roll = rollDice(20, 1).total;
    const hit = roll + bonus >= target.ac;
    const damage = hit ? rollDice(parseDice(damage_dice).sides, parseDice(damage_dice).count).total : 0;
    if (hit) {
      const remaining = Math.max(0, target.hp - damage);
      if (remaining === 0) {
        gameState.update((s) => ({
          ...s,
          enemies: s.enemies.filter((e) => e.name !== target.name),
        }));
        return `${attacker} attacks ${target.name}: d20 = ${roll} + ${bonus} vs AC ${target.ac} -> HIT, ${damage} damage. ${target.name} is defeated!`;
      }
      gameState.update((s) => ({
        ...s,
        enemies: s.enemies.map((e) => (e.name === target.name ? { ...e, hp: remaining } : e)),
      }));
      return `${attacker} attacks ${target.name}: d20 = ${roll} + ${bonus} vs AC ${target.ac} -> HIT, ${damage} damage. ${target.name} has ${remaining} HP left.`;
    }
    return `${attacker} attacks ${target.name}: d20 = ${roll} + ${bonus} vs AC ${target.ac} -> MISS.`;
  },
});
