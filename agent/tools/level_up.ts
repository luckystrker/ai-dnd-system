import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { appendKeyEvent, appendTranscriptEntry } from "../lib/campaigns/journal.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";
import { classHitDie, levelForXp, statModifier } from "../lib/rewards.ts";

/** Бонус ВЫН из листа: принимаем каноничные и русские ключи характеристик. */
function constitutionModifier(stats: Record<string, number> | undefined): number {
  if (!stats) return 0;
  for (const key of ["constitution", "con", "выносливость", "телосложение", "CON"]) {
    if (typeof stats[key] === "number") return statModifier(stats[key]);
  }
  return 0;
}

export default defineTool({
  description:
    "Level up a character: advances level to the given (or XP-computed) value, increases max HP " +
    "by hit die + CON per level, and adds the new ability you pass. Call when complete_quest reports " +
    "a level-up, or when the story grants one. The ability must follow the balance rules of " +
    "character creation (level-appropriate, no epic spells at low levels).",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    character: z.string().min(1).max(100).describe("Имя (или slug) персонажа."),
    level: z
      .number()
      .int()
      .min(2)
      .max(20)
      .optional()
      .describe("Новый уровень (по умолчанию — по накопленному XP персонажа)."),
    ability: z
      .object({
        name: z.string().min(1).max(80).describe("Название новой способности/заклинания."),
        description: z.string().min(1).max(300).describe("Что делает и как применяется."),
      })
      .optional()
      .describe("Новая способность за уровень (обязательна для кастовых классов)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const needle = input.character.toLowerCase();
      const sheet = campaignStore.listCharacters(campaign.id).find(
        (candidate) =>
          candidate.id === input.character ||
          candidate.slug.toLowerCase() === needle ||
          candidate.name.toLowerCase() === needle,
      );
      if (!sheet) {
        throw new StoreError(`Персонаж «${input.character}» не найден в кампании.`, "not_found");
      }
      const oldLevel = sheet.level ?? 1;
      const newLevel = input.level ?? levelForXp(sheet.xp ?? 0);
      if (newLevel <= oldLevel) {
        throw new StoreError(
          `Персонаж «${sheet.name}» уже на уровне ${oldLevel} — повышение до ${newLevel} невозможно.`,
          "conflict",
        );
      }

      const delta = newLevel - oldLevel;
      const oldMaxHp = sheet.maxHp;
      const perLevel = classHitDie(sheet.characterClass) + constitutionModifier(sheet.stats);
      const newMaxHp = oldMaxHp !== undefined ? oldMaxHp + perLevel * delta : undefined;

      campaignStore.updateCharacter(campaign.id, sheet.name, {
        level: newLevel,
        maxHp: newMaxHp,
      });
      if (input.ability) {
        campaignStore.grantCharacter(campaign.id, sheet.name, {
          abilities: [{ ...input.ability, level: newLevel }],
        });
      }

      const day = campaign.currentDay ?? 1;
      appendKeyEvent(
        campaign.slug,
        day,
        `${sheet.name} достиг ${newLevel} уровня.`,
        `evt:levelup:${sheet.id}:${newLevel}`,
      );
      appendTranscriptEntry(campaign.slug, day, {
        kind: "action",
        text: `${sheet.name} достиг ${newLevel} уровня.`,
        eventId: `evt:levelup:${sheet.id}:${newLevel}`,
      });

      return {
        ok: true,
        character: {
          name: sheet.name,
          level: newLevel,
          maxHp: newMaxHp ?? null,
          gainedAbility: input.ability?.name ?? null,
          hpGain: oldMaxHp !== undefined ? newMaxHp! - oldMaxHp : null,
        },
        note:
          "Уровень повышен. Объяви игрокам, что персонаж получил новый уровень, " +
          "и опиши новую способность в игре.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
