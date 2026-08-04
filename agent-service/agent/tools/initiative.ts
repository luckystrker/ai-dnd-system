import { defineTool } from "eve/tools";
import { z } from "zod";

import { rollDice } from "../lib/engine/dnd5e";

export default defineTool({
  description:
    "Roll initiative for a list of combatants and return the turn order " +
    "(highest roll first).",
  inputSchema: z.object({
    combatants: z
      .array(
        z.object({
          name: z.string().min(1).max(100),
          bonus: z.number().int().min(-10).max(20).default(0),
        }),
      )
      .min(1)
      .max(20),
  }),
  execute({ combatants }) {
    const entries = combatants.map((combatant) => {
      const roll = rollDice(20, 1).total;
      return {
        name: combatant.name,
        roll,
        total: roll + combatant.bonus,
      };
    });
    entries.sort((a, b) => b.total - a.total);
    const order = entries
      .map((e, index) => `${index + 1}. ${e.name} (${e.total} = ${e.roll} + ${e.total - e.roll})`)
      .join("\n");
    return `Initiative order:\n${order}`;
  },
});
