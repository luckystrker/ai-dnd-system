import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { appendTranscriptEntry } from "../lib/campaigns/journal.ts";
import { locationStore } from "../lib/campaigns/locations.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Move the party to a discovered location: marks it current (clearing the flag from the previous " +
    "location), records the visit for the current in-game day, and writes an action line to the day " +
    "transcript. The location must already exist (create it first with upsert_location). Use this for " +
    "travel, not upsert_location's current flag.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    location: z.string().min(1).max(100).describe("Имя (или slug) локации назначения."),
    via: z
      .string()
      .max(200)
      .optional()
      .describe("Как добрались: дорога, река, перевал (для записи в транскрипт)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const target = locationStore.getLocation(campaign.id, input.location);
      if (!target) {
        return { ok: false, error: `Локация «${input.location}» не найдена. Создай её через upsert_location.` };
      }
      locationStore.setCurrent(campaign.id, target.slug);
      const day = campaign.currentDay ?? 1;
      locationStore.markVisited(campaign.id, target.slug, day);
      const via = input.via ? ` через ${input.via}` : "";
      appendTranscriptEntry(campaign.slug, day, {
        kind: "action",
        text: `Партия переместилась в «${target.name}»${via}.`,
        eventId: `move:${target.slug}:${day}`,
      });
      return {
        ok: true,
        location: target.name,
        currentDay: day,
        note: `Партия теперь в локации «${target.name}». Перемещение записано в транскрипт дня ${day}.`,
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
