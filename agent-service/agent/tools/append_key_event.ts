import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { appendKeyEvent } from "../lib/campaigns/journal.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Save one key moment of the campaign into long-term memory (history/key-events.md). " +
    "Use for turning points, promises, betrayals, discovered secrets — facts the DM must " +
    "remember sessions later. One call = one concise event.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    event: z
      .string()
      .min(5)
      .max(500)
      .describe("Ключевое событие в 1-2 предложениях: кто, что сделал и каковы последствия."),
    day: z
      .number()
      .int()
      .min(1)
      .optional()
      .describe("Игровой день события. Если не указан — текущий день кампании."),
  }),
  execute({ campaignSlug, event, day }, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, campaignSlug);
      const eventDay = day ?? campaign.currentDay ?? 1;
      appendKeyEvent(campaign.slug, eventDay, event);
      return { ok: true, day: eventDay, note: "Ключевое событие сохранено в память кампании." };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
