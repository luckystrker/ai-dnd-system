import { defineTool } from "eve/tools";
import { z } from "zod";

import { characterCard } from "../lib/campaigns/card.ts";
import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { appendLedgerRow } from "../lib/campaigns/journal.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Grant a character new items, abilities, gold, XP or conditions - everything is ADDED to what " +
    "the character already has (no replacement). Use for loot, quest rewards, learning new abilities, " +
    "level-up gains. Interactive calls require the campaign DM; automatic calls (chronicler) pass campaignSlug explicitly.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    character: z.string().min(1).max(100).describe("Имя (или slug) персонажа."),
    items: z
      .array(z.string().max(120))
      .max(50)
      .optional()
      .describe("Новые предметы, добавляемые в инвентарь (без замены существующих)."),
    abilities: z
      .array(
        z.object({
          name: z.string().min(1).max(80),
          description: z.string().min(1).max(300),
          level: z.number().int().min(1).max(20).optional(),
        }),
      )
      .max(20)
      .optional()
      .describe("Новые способности/заклинания (дубликаты по имени игнорируются)."),
    gold: z.number().int().min(0).max(1_000_000).optional().describe("Сколько золота прибавить."),
    xp: z.number().int().min(0).max(10_000_000).optional().describe("Сколько опыта прибавить."),
    conditions: z
      .array(z.string().max(60))
      .max(20)
      .optional()
      .describe("Новые активные состояния (отравлен, оглушён и т.п.)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const { campaignSlug: _slug, character, ...patch } = input;
      const sheet = campaignStore.grantCharacter(campaign.id, character, patch);

      // C3: детерминированная запись в журнал экономики (без отдельного вызова).
      const day = campaign.currentDay ?? 1;
      if (patch.gold !== undefined && patch.gold > 0) {
        appendLedgerRow(campaign.slug, {
          day,
          type: "found",
          itemOrGold: `${patch.gold} золотых`,
          by: character,
        }, `grant:${character}:gold:${day}:${patch.gold}`);
      }
      if (patch.items && patch.items.length > 0) {
        appendLedgerRow(campaign.slug, {
          day,
          type: "found",
          itemOrGold: patch.items.join(", "),
          by: character,
        }, `grant:${character}:items:${day}:${patch.items.length}`);
      }

      return {
        ok: true,
        character: characterCard(sheet),
        note: "Предметы, способности и ресурсы добавлены персонажу. Отрази это в ответе игрокам.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
