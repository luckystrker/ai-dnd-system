import { defineTool } from "eve/tools";
import { z } from "zod";

import { requireIdentity, resolveCampaign } from "../lib/campaigns/access.ts";
import { ensureDay } from "../lib/campaigns/journal.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Advance the in-game day of the active campaign. Use when a new day begins in the story " +
    "(overnight rest, travel, big time skip). Only the campaign DM can do this.",
  inputSchema: z.object({
    campaignId: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — кампания текущего чата."),
    note: z
      .string()
      .max(300)
      .optional()
      .describe("Короткая пометка к новому дню: время суток, место, погода."),
  }),
  execute({ campaignId, note }, ctx) {
    try {
      const identity = requireIdentity(ctx.session.auth.current);
      const campaign = resolveCampaign(ctx.session.auth.current, campaignId);
      if (!campaign) {
        return { ok: false, error: "В этом чате нет привязанной кампании." };
      }
      const previousDay = campaign.currentDay ?? 1;
      const updated = campaignStore.advanceDay(campaign.id, identity.userId);
      const day = updated.currentDay ?? 1;
      ensureDay(updated.slug, day, { note });
      return {
        ok: true,
        campaignSlug: updated.slug,
        previousDay,
        day,
        note:
          `Наступил игровой день ${day}. Теперь вызови субагента chronicler, чтобы закрыть день ${previousDay}: ` +
          "передай ему slug кампании, номер закрытого дня и попроси записать саммари дня, " +
          "ключевые события и изменения NPC и персонажей.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
