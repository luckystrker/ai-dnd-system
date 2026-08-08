import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { factionStore } from "../lib/campaigns/factions.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Create or update a faction of the campaign: a guild, clan, order or other organization with a " +
    "standing (reputation) toward the party on a -5 (enemy) .. +5 (ally) scale. Factions live in the " +
    "campaign's factions/ folder. Use this to introduce political groups; use adjust_standing for " +
    "reputation changes from quest outcomes.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    name: z.string().min(1).max(100).describe("Имя фракции."),
    description: z.string().max(2000).optional().describe("Что известно о фракции: цели, состав, нрав."),
    standing: z
      .number()
      .int()
      .min(-5)
      .max(5)
      .optional()
      .describe("Текущая репутация: -5 (враг) .. +5 (союзник). По умолчанию 0 (нейтрально)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const { campaignSlug: _slug, ...factionInput } = input;
      const faction = factionStore.upsertFaction(campaign.id, factionInput);
      return {
        ok: true,
        faction: {
          name: faction.name,
          slug: faction.slug,
          description: faction.description ?? null,
          standing: faction.standing,
        },
        note: "Фракция сохранена в папке кампании (factions/).",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
