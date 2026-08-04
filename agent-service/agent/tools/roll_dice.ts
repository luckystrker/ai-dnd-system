import { defineTool } from "eve/tools";
import { z } from "zod";

import { rollDice } from "../lib/engine/dnd5e";

export default defineTool({
  description: "Roll dice for an uncertain game outcome. Never use invented results.",
  inputSchema: z.object({
    sides: z.number().int().min(1),
    count: z.number().int().min(1).max(100).default(1),
  }),
  execute({ sides, count }) {
    const result = rollDice(sides, count);
    return `Rolled ${count}d${sides}: [${result.rolls.join(", ")}] = ${result.total}`;
  },
});
