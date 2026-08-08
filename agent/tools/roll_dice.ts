import { defineTool } from "eve/tools";
import { z } from "zod";

import { rollDiceNotation, type NotationResult } from "../lib/engine/dnd5e";

/** Развёрнутое сообщение о броске для группы кубиков. */
function groupLine(
  spec: string,
  rolls: number[],
  subtotal: number,
  options: { pairs?: Array<[number, number]>; dropped?: number[] } = {},
): string {
  const { pairs, dropped } = options;
  if (pairs && pairs.length > 0) {
    const shown = pairs.map(([pick, drop]) => `${pick}|${drop}`).join(", ");
    return `${spec} → [${shown}] = ${subtotal}`;
  }
  if (dropped && dropped.length > 0) {
    return `${spec} → [${rolls.join(", ")}] (отброшено: ${dropped.join(", ")}) = ${subtotal}`;
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
  const lines = result.groups.map((g) => groupLine(g.spec, g.rolls, g.subtotal, { pairs: g.pairs, dropped: g.dropped }));
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
    "disadvantage). Group modifiers: khN/klN keep the highest/lowest N (e.g. '4d6kh3' rolls " +
    "4d6 and keeps the 3 highest), dhN/dlN drop the highest/lowest N (e.g. '4d6dl1' drops the " +
    "lowest, same as keep-highest-3 — use for ability score generation), and '!' rolls " +
    "exploding dice (a die on its max face adds another, e.g. '8d6!'). Never use invented results.",
  inputSchema: z.object({
    notation: z
      .string()
      .min(1)
      .max(50)
      .describe(
        'Dice notation: "4d20", "2d4", "2d6+1d8+3", "1d20+5", "d8", "4d6kh3", "4d6dl1", "8d6!". ' +
          "Up to 5 groups and at most 100 dice total, plus an optional flat modifier (+N / -N). " +
          "Group modifiers: khN/klN (keep highest/lowest N), dhN/dlN (drop highest/lowest N), ! (explode).",
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
