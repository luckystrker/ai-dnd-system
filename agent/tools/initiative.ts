import { defineTool } from "eve/tools";
import { z } from "zod";

import { rollInitiative } from "../lib/engine/combat.ts";
import { gameState } from "../lib/memory.ts";

export default defineTool({
  description:
    "Roll initiative and START turn-based combat. Call this the moment a fight begins: list EVERY participant — " +
    "party characters (side=party) and enemies (side=enemy, with their armor class ac and current hit points hp). " +
    "Persists the turn order in the session; the combat tool continues from there. " +
    "If a new enemy joins an ongoing fight, call this again with the full updated list — the order is re-rolled.",
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
  execute({ combatants }) {
    const order = rollInitiative(combatants);
    const enemies = order
      .filter((entry) => entry.side === "enemy")
      .map((entry) => ({ name: entry.name, hp: entry.hp as number, ac: entry.ac as number }));
    gameState.update((state) => ({
      ...state,
      enemies,
      combat: {
        started: order.length > 0,
        round: 1,
        current: order.length > 0 ? 0 : -1,
        order,
      },
    }));

    const lines = order.map((entry, index) => {
      const enemyNote = entry.side === "enemy" ? ` (враг, HP ${entry.hp}, КД ${entry.ac})` : "";
      return `${index + 1}. ${entry.name} (${entry.total} = ${entry.roll} + ${entry.bonus})${enemyNote}`;
    });
    const first = order[0];
    const started = first ? `Бой начат, раунд 1. Первый ход: ${first.name}.` : "Бой начат, раунд 1.";
    return `Инициатива:\n${lines.join("\n")}\n${started}`;
  },
});
