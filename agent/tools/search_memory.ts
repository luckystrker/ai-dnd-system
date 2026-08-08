import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { searchCampaignMemory } from "../lib/campaigns/search.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Search the campaign's long-term memory by keywords: transcripts of past in-game days, " +
    "day/campaign summaries, key events, NPC cards and character sheets. Returns matching " +
    "snippets with the source file and day number. Use when you need a fact but don't know " +
    "which in-game day it happened in (read_day only works by day number). Read-only.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — кампания текущего чата."),
    query: z
      .string()
      .min(1)
      .max(200)
      .describe("Поисковый запрос: ключевые слова (имя, место, предмет, событие). Регистр не важен."),
    limit: z
      .number()
      .int()
      .min(1)
      .max(40)
      .optional()
      .describe("Сколько результатов вернуть (по умолчанию 20)."),
  }),
  execute({ campaignSlug, query, limit }, ctx) {
    try {
      const campaign = resolveCampaign(ctx.session.auth.current, campaignSlug);
      if (!campaign) {
        return { ok: false, error: "Кампания не найдена: укажи campaignSlug или запусти кампанию в этом чате." };
      }
      const hits = searchCampaignMemory(campaign.slug, query, limit ? { limit } : {});
      if (hits.length === 0) {
        return { ok: true, campaignSlug: campaign.slug, count: 0, results: [], note: "Ничего не найдено." };
      }
      return { ok: true, campaignSlug: campaign.slug, count: hits.length, results: hits };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
