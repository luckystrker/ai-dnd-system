import { defineTool } from "eve/tools";
import { z } from "zod";

import { characterSheetFor } from "../lib/campaigns/access.ts";
import { rollInitiative, type CombatantEntry } from "../lib/engine/combat.ts";
import { gameState } from "../lib/memory.ts";

/** Привязывает HP/макс HP персонажей партии: свежий лист → состояние партии. */
function attachPartyHp(entries: CombatantEntry[], ctx: unknown): CombatantEntry[] {
  return entries.map((entry) => {
    if (entry.side !== "party") return entry;
    const sheet = characterSheetFor(ctx as never, entry.name);
    if (sheet?.hp !== undefined || sheet?.maxHp !== undefined) {
      return { ...entry, hp: sheet.hp ?? entry.hp, maxHp: sheet.maxHp ?? entry.maxHp };
    }
    const member = gameState.get().party.find(
      (candidate) => candidate.id === entry.id || candidate.name.trim().toLowerCase() === entry.name.trim().toLowerCase(),
    );
    if (member?.hp !== undefined || member?.maxHp !== undefined) {
      return { ...entry, hp: member.hp, maxHp: member.maxHp };
    }
    return entry;
  });
}

export default defineTool({
  description:
    "Roll initiative and START turn-based combat. Call this the moment a fight begins: list EVERY participant — " +
    "party characters (side=party) and enemies (side=enemy, with their armor class ac and current hit points hp). " +
    "Party members' HP is loaded from their sheets automatically. Each combatant gets a stable id (shown in the " +
    "result next to its name); pass that id to the combat tool's attacker/enemy/target fields — ids are exact, " +
    "names are matched as a fallback only. If a new enemy joins an ongoing fight, call this again with the full " +
    "updated list — the order is re-rolled and ids may change.",
  inputSchema: z.object({
    combatants: z
      .array(
        z.object({
          name: z.string().min(1).max(100),
          side: z.enum(["party", "enemy"]).describe("party — персонаж игрока, enemy — враг"),
          bonus: z.number().int().min(-10).max(20).default(0).describe("Модификатор инициативы (ЛОВ)"),
          hp: z.number().int().min(1).max(100000).optional().describe("Враги: текущие хиты (обязательно для enemy)"),
          ac: z.number().int().min(1).max(40).optional().describe("Враги: класс брони (обязательно для enemy)"),
        }),
      )
      .min(1)
      .max(20)
      .refine(
        (list) =>
          list.every(
            (combatant) => combatant.side !== "enemy" || (combatant.hp !== undefined && combatant.ac !== undefined),
          ),
        { message: "Для каждого врага (side=enemy) укажите hp и ac.", path: ["combatants"] },
      ),
  }),
  execute({ combatants }, ctx) {
    // Для персонажей партии подставляем id из листа, если он есть — это стабильный id игрока.
    const party = gameState.get().party;
    const withIds = combatants.map((c) => {
      if (c.side === "party") {
        const member = party.find((m) => m.name.trim().toLowerCase() === c.name.trim().toLowerCase());
        if (member?.id) return { ...c, id: member.id };
      }
      return c;
    });
    const order = attachPartyHp(rollInitiative(withIds), ctx);
    const enemies = order
      .filter((entry) => entry.side === "enemy")
      .map((entry) => ({ id: entry.id, name: entry.name, hp: entry.hp as number, ac: entry.ac as number }));
    gameState.update((state) => ({
      ...state,
      enemies,
      combat: {
        started: order.length > 0,
        round: 1,
        current: order.length > 0 ? 0 : -1,
        acted: false,
        order,
      },
    }));

    const lines = order.map((entry, index) => {
      const parts = [`${entry.total} = ${entry.roll} + ${entry.bonus}`];
      if (entry.side === "enemy") {
        parts.push(`враг, HP ${entry.hp}, КД ${entry.ac}`);
      } else if (entry.hp !== undefined) {
        parts.push(`HP ${entry.hp}/${entry.maxHp ?? "?"}`);
      }
      return `${index + 1}. ${entry.name} [id: ${entry.id}] (${parts.join(", ")})`;
    });
    const first = order[0];
    const started = first
      ? `Бой начат, раунд 1. Первый ход: ${first.name} (${first.id}). Используй id в combat.`
      : "Бой начат, раунд 1.";
    return `Инициатива:\n${lines.join("\n")}\n${started}`;
  },
});
