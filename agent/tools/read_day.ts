import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { readDayTail } from "../lib/campaigns/journal.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Read the transcript of one in-game day of the campaign: who said and did what. " +
    "Defaults to the current day of the campaign bound to this chat.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — кампания текущего чата."),
    day: z
      .number()
      .int()
      .min(1)
      .optional()
      .describe("Номер игрового дня. Если не указан — текущий день кампании."),
    last: z
      .number()
      .int()
      .min(1)
      .max(500)
      .optional()
      .describe("Сколько последних записей дня вернуть. Если не указан — весь день."),
  }),
  execute({ campaignSlug, day, last }, ctx) {
    try {
      const campaign = resolveCampaign(ctx.session.auth.current, campaignSlug);
      if (!campaign) {
        return { ok: false, error: "Кампания не найдена: укажи campaignSlug или запусти кампанию в этом чате." };
      }
      const wantedDay = day ?? campaign.currentDay ?? 1;
      const record = readDayTail(campaign.slug, wantedDay, last ?? Number.POSITIVE_INFINITY);
      if (!record) {
        return { ok: false, error: `За игровой день ${wantedDay} записей ещё нет.` };
      }
      return {
        ok: true,
        campaignSlug: campaign.slug,
        day: record.day,
        date: record.date ?? null,
        note: record.note ?? null,
        summary: record.summary ?? null,
        entries: record.entries,
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
