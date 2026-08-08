import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { StoreError } from "../lib/campaigns/types.ts";
import { renderWorldState } from "../lib/campaigns/world-state.ts";

export default defineTool({
  description:
    "Read the current durable state of the world (history/world-state.md): who is dead, what places " +
    "changed, what decisions were taken — grouped by category. Reflects what is true now, unlike the " +
    "append-only key events. Read-only. The world-state is also loaded into the memory block each turn.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — кампания текущего чата."),
  }),
  execute({ campaignSlug }, ctx) {
    try {
      const campaign = resolveCampaign(ctx.session.auth.current, campaignSlug);
      if (!campaign) {
        return { ok: false, error: "Кампания не найдена: укажи campaignSlug или запусти кампанию в этом чате." };
      }
      const rendered = renderWorldState(campaign.slug);
      return {
        ok: true,
        empty: rendered.length === 0,
        state: rendered.length > 0 ? rendered : null,
        note: rendered.length === 0 ? "Состояние мира пусто." : "Текущие факты мира по категориям.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
