import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "List open threads (promises, mysteries, debts) of the campaign. Use to refresh which loose " +
    "ends are alive before planning a scene, or after reading a transcript to find threads to close.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    includeResolved: z
      .boolean()
      .optional()
      .describe("Включить в ответ и закрытые нити (по умолчанию только открытые)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaign(ctx.session.auth.current, input.campaignSlug);
      if (!campaign) {
        return { ok: false, error: "В этом чате нет привязанной кампании." };
      }
      const threads = campaignStore.listThreads(campaign.id).filter(
        (thread) => input.includeResolved === true || thread.status === "open",
      );
      return {
        ok: true,
        threads: threads.map((thread) => ({
          id: thread.id,
          text: thread.text,
          kind: thread.kind,
          status: thread.status,
          dayOpened: thread.dayOpened,
          dayClosed: thread.dayClosed ?? null,
        })),
        note:
          "Открытые нити — это обещания и тайны, к которым стоит возвращаться в игре. " +
          "Закрывай их через resolve_thread, когда сюжет их разрешил.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
