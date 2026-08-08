import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { setDayHeadline, setDaySummary } from "../lib/campaigns/journal.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Write the short summary of one in-game day into the day file. Call when closing a day " +
    "(usually via the chronicler) so later sessions can recall the day without reading the full transcript. " +
    "The optional headline is a one-line (<=140 chars) title of the day shown for EVERY past day in the " +
    "memory block, so the full campaign arc stays visible regardless of how long the campaign grows.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    day: z
      .number()
      .int()
      .min(1)
      .optional()
      .describe("Игровой день. Если не указан — текущий день кампании."),
    summary: z
      .string()
      .min(10)
      .max(1200)
      .describe("Саммари дня: место действия, главные события и итоги, 3-6 предложений."),
    headline: z
      .string()
      .min(3)
      .max(140)
      .optional()
      .describe("Короткая шапка дня (≤140 симв.): одной строкой, что главное случилось. Видна для каждого прошедшего дня в памяти кампании."),
  }),
  execute({ campaignSlug, day, summary, headline }, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, campaignSlug);
      const wantedDay = day ?? campaign.currentDay ?? 1;
      setDaySummary(campaign.slug, wantedDay, summary);
      if (headline) setDayHeadline(campaign.slug, wantedDay, headline);
      return { ok: true, day: wantedDay, note: "Саммари дня записано в файл дня." };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
