import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Resolve (close) an open thread: a promise kept, a mystery solved, a debt paid. " +
    "Pass the thread id, or a text fragment that uniquely matches it. The thread moves to " +
    "resolved and stops being injected into memory.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    thread: z
      .string()
      .min(1)
      .max(300)
      .describe("Id нити или текст-фрагмент для поиска по открытым нитям."),
    day: z
      .number()
      .int()
      .min(1)
      .max(9999)
      .optional()
      .describe("Игровой день закрытия (по умолчанию — текущий)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const thread = campaignStore.resolveThread(campaign.id, input.thread, input.day);
      return {
        ok: true,
        thread: {
          id: thread.id,
          text: thread.text,
          kind: thread.kind,
          status: thread.status,
          dayOpened: thread.dayOpened,
          dayClosed: thread.dayClosed ?? null,
        },
        note: "Нить закрыта и больше не подставляется в память DM.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
