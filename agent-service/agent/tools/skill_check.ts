import { defineTool } from "eve/tools";
import { z } from "zod";

import { skillCheck } from "../lib/engine/dnd5e";

interface ToolSessionContext {
  session: {
    auth: {
      current: { attributes: Readonly<Record<string, string | readonly string[]>> } | null;
      initiator: { attributes: Readonly<Record<string, string | readonly string[]>> } | null;
    };
  };
}

function sessionStats(ctx: ToolSessionContext): Record<string, unknown> {
  const auth = ctx.session.auth.current ?? ctx.session.auth.initiator;
  const value = auth?.attributes.stats;
  if (typeof value !== "string") {
    return {};
  }
  try {
    const parsed: unknown = JSON.parse(value);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? parsed as Record<string, unknown>
      : {};
  } catch {
    return {};
  }
}

export default defineTool({
  description: "Perform a d20 skill check using the current player's character stats.",
  inputSchema: z.object({
    character_name: z.string().min(1).max(100),
    skill: z.string().min(1).max(50),
    difficulty: z.number().int().min(1).max(100),
    advantage: z.boolean().nullable().default(null),
  }),
  execute({ character_name, skill, difficulty, advantage }, ctx) {
    const result = skillCheck(sessionStats(ctx), skill, difficulty, advantage);
    const status = result.success ? "SUCCESS" : "FAILURE";
    return `${character_name} - ${skill}: ${result.roll} + ${result.modifier} = ${result.total} vs DC ${difficulty} -> ${status}`;
  },
});
