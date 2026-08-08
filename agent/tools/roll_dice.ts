import { defineTool } from "eve/tools";
import { z } from "zod";

import { rollDiceNotation, type NotationResult } from "../lib/engine/dnd5e";

/** Развёрнутое сообщение о броске для группы кубиков. */
function groupLine(spec: string, rolls: number[], subtotal: number, pairs?: Array<[number, number]>): string {
  if (pairs && pairs.length > 0) {
    const shown = pairs.map(([pick, dropped]) => `${pick}|${dropped}`).join(", ");
    return `${spec} → [${shown}] = ${subtotal}`;
  }
  return `${spec} → [${rolls.join(", ")}] = ${subtotal}`;
}

/** Форматирует результат броска в строку для игрока. */
function formatRoll(result: NotationResult, advantage: "advantage" | "disadvantage" | "normal"): string {
  const tag =
    advantage === "advantage"
      ? " (преимущество)"
      : advantage === "disadvantage"
        ? " (слабость)"
        : "";
  const lines = result.groups.map((g) => groupLine(g.spec, g.rolls, g.subtotal, g.pairs));
  const mod =
    result.modifier === 0
      ? null
      : `${result.modifier > 0 ? "+" : ""}${result.modifier} модификатор`;
  const parts = [`🎲 Бросок ${result.notation}${tag}:`, ...lines];
  if (mod) parts.push(mod);
  parts.push(`Итого: ${result.total}`);
  return parts.join("\n");
}

export default defineTool({
  description:
    "Roll dice using standard notation (e.g. '4d20', '2d6+1d8+3', '1d20+5', 'd8'). " +
    "Use for any uncertain game outcome: enemy attacks vs a player's AC, random tables, " +
    "damage outside the combat tool, loot, etc. Advantage/disadvantage applies to d20 dice " +
    "ONLY (each d20 is rolled twice, keeping the higher for advantage or the lower for " +
    "disadvantage). Never use invented results.",
  inputSchema: z.object({
    notation: z
      .string()
      .min(1)
      .max(50)
      .describe(
        'Dice notation: "4d20", "2d4", "2d6+1d8+3", "1d20+5", "d8". ' +
          "Up to 5 groups and at most 100 dice total, plus an optional flat modifier (+N / -N).",
      ),
    advantage: z
      .enum(["advantage", "disadvantage", "normal"])
      .default("normal")
      .describe(
        "Applies to d20 dice ONLY: 'advantage' rolls each d20 twice and keeps the higher, " +
          "'disadvantage' keeps the lower, 'normal' rolls once. Non-d20 dice are unaffected.",
      ),
  }),
  execute({ notation, advantage }) {
    const advBool = advantage === "advantage" ? true : advantage === "disadvantage" ? false : null;
    try {
      const result = rollDiceNotation(notation, { advantage: advBool });
      return formatRoll(result, advantage);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return `Не удалось разобрать бросок «${notation}»: ${message}`;
    }
  },
});
