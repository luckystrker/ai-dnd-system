import { defineTool } from "eve/tools";
import { z } from "zod";

import { skillCheck } from "../lib/engine/dnd5e";
import { gameState } from "../lib/memory";

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
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

/**
 * Характеристики персонажа из состояния партии (hydrate при запуске кампании
 * и get_game_context кладут туда листы с stats). Это основной источник:
 * auth-attributes канала stats не содержат.
 */
function partyStats(characterName: string): Record<string, unknown> | undefined {
  const name = characterName.trim().toLowerCase();
  const member = gameState.get().party.find((entry) => entry.name.trim().toLowerCase() === name);
  return member?.stats;
}

const ABILITY_LABELS: Record<string, string> = {
  str: "СИЛ",
  dex: "ЛОВ",
  con: "ТЕЛ",
  int: "ИНТ",
  wis: "МДР",
  cha: "ХАР",
};

export default defineTool({
  description:
    "Perform a d20 skill check using the character's ability modifier from the party state. " +
    "Natural 20 always succeeds, natural 1 always fails. The output contains the full roll " +
    "breakdown (skill, ability, d20, modifier, total, DC, outcome) - always show it to the player.",
  inputSchema: z.object({
    character_name: z.string().min(1).max(100),
    skill: z.string().min(1).max(50),
    difficulty: z.number().int().min(1).max(100),
    advantage: z.boolean().nullable().default(null),
  }),
  execute({ character_name, skill, difficulty, advantage }, ctx) {
    const stats = partyStats(character_name) ?? sessionStats(ctx);
    const result = skillCheck(stats, skill, difficulty, advantage);
    const abilityLabel = ABILITY_LABELS[result.ability] ?? result.ability.toUpperCase();
    const modifierSign = result.modifier >= 0 ? "+" : "";
    let outcome = result.success ? "УСПЕХ" : "ПРОВАЛ";
    if (result.naturalSuccess) outcome += " (натуральная 20)";
    if (result.naturalFailure) outcome += " (натуральная 1)";
    const statsFound = Object.keys(stats).length > 0;
    const noStatsNote = statsFound ? "" : " [характеристики персонажа не найдены, модификатор +0]";
    return (
      `🎲 ${character_name} — проверка «${skill}» (${abilityLabel}): ` +
      `d20 = ${result.roll} ${modifierSign}${result.modifier} = ${result.total} vs DC ${difficulty} -> ${outcome}` +
      noStatsNote
    );
  },
});
