import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { upsertCampaignSummaryDay } from "../lib/campaigns/journal.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Add (or replace) the campaign-wide summary section for one in-game day in history/summary.md. " +
    "This is the rolling digest the DM loads in later sessions instead of the full transcript.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    day: z.number().int().min(1).describe("Игровой день, для которого пишется саммари."),
    text: z
      .string()
      .min(10)
      .max(1500)
      .describe("Сжатое саммари дня для хроники кампании: ключевые события, решения и последствия."),
  }),
  execute({ campaignSlug, day, text }, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, campaignSlug);
      upsertCampaignSummaryDay(campaign.slug, day, text);
      return { ok: true, day, note: "Саммари дня добавлено в хронику кампании (history/summary.md)." };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
