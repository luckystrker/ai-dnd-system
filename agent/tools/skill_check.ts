import { defineTool } from "eve/tools";
import { z } from "zod";

import { canActForCharacter } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import type { ToolSessionContext } from "../lib/campaigns/session.ts";
import {
  combineAdvantage,
  environmentModifiersForCheck,
  isLowStreak,
  makeLuckyRandom,
  skillCheck,
} from "../lib/engine/dnd5e.ts";
import { gameState } from "../lib/memory.ts";

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
    "Natural 20 always succeeds, natural 1 always fails. Pass advantage=true when the situation " +
    "favors the character (roll 2d20 keep higher), advantage=false for disadvantage (keep lower), " +
    "null for a normal single roll. The output contains the full roll breakdown (skill, ability, " +
    "d20, modifier, total, DC, outcome) - always show it to the player.",
  inputSchema: z.object({
    character_name: z.string().min(1).max(100),
    skill: z.string().min(1).max(50),
    difficulty: z.number().int().min(1).max(100),
    advantage: z.boolean().nullable().default(null),
  }),
  execute({ character_name, skill, difficulty, advantage }, ctx) {
    const access = canActForCharacter(ctx, character_name);
    if (!access.allowed) {
      return access.reason ?? "Проверка за этого персонажа запрещена.";
    }
    const stats = partyStats(character_name) ?? sessionStats(ctx);

    // Влияние окружения (C2): время суток и погода могут давать помеху/преимущество.
    // По правилу 5e преимущество и помеха взаимно отменяются (combineAdvantage).
    const campaignId = gameState.get().campaignId;
    const campaign = campaignId ? campaignStore.getCampaign(campaignId) : undefined;
    const env = environmentModifiersForCheck(
      { timeOfDay: campaign?.timeOfDay, weather: campaign?.weather },
      skill,
    );
    const effectiveAdvantage = combineAdvantage(advantage, env.advantage);

    // «Добрый» псевдорандом: слегка сдвигает d20 вверх и ломает серии низких
    // бросков. Применяется только к проверкам, не к бою/урону/инициативе.
    const lowStreak = isLowStreak(gameState.get().diceHistory);
    const result = skillCheck(stats, skill, difficulty, effectiveAdvantage, makeLuckyRandom(Math.random, lowStreak));
    // Запоминаем грань d20 для определения будущих серий неудач.
    gameState.update((s) => ({ ...s, diceHistory: [...s.diceHistory, result.roll].slice(-4) }));
    const abilityLabel = ABILITY_LABELS[result.ability] ?? result.ability.toUpperCase();
    const modifierSign = result.modifier >= 0 ? "+" : "";
    const advTag =
      effectiveAdvantage === true ? " (преимущество)" : effectiveAdvantage === false ? " (помеха)" : "";
    let outcome = result.success ? "УСПЕХ" : "ПРОВАЛ";
    if (result.naturalSuccess) outcome += " (натуральная 20)";
    if (result.naturalFailure) outcome += " (натуральная 1)";
    const statsFound = Object.keys(stats).length > 0;
    const noStatsNote = statsFound ? "" : " [характеристики персонажа не найдены, модификатор +0]";
    const envNote = env.reasons.length > 0 ? ` [${env.reasons.join("; ")}]` : "";
    return (
      `🎲 ${character_name} — проверка «${skill}» (${abilityLabel})${advTag}: ` +
      `d20 = ${result.roll} ${modifierSign}${result.modifier} = ${result.total} vs DC ${difficulty} -> ${outcome}` +
      noStatsNote +
      envNote
    );
  },
});
