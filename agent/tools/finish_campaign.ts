import { defineTool } from "eve/tools";
import { z } from "zod";

import { requireIdentity, resolveCampaign } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Finish/close a campaign: marks it as finished, keeps all data (history, characters, NPCs) for " +
    "later reading, and frees the chat so a new campaign can be started in it. Only the campaign DM " +
    "can do this. Use when the story is over or the group wants to close the campaign.",
  inputSchema: z.object({
    campaignId: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — кампания текущего чата."),
  }),
  execute({ campaignId }, ctx) {
    try {
      const identity = requireIdentity(ctx.session.auth.current);
      const campaign = resolveCampaign(ctx.session.auth.current, campaignId);
      if (!campaign) {
        return { ok: false, error: "В этом чате нет привязанной кампании." };
      }
      const finished = campaignStore.finishCampaign(campaign.id, identity.userId);
      return {
        ok: true,
        campaign: {
          id: finished.id,
          slug: finished.slug,
          title: finished.title,
          status: finished.status,
        },
        note:
          `Кампания «${finished.title}» завершена. Данные сохранены, чат свободен для новой кампании ` +
          "(/newcampaign). Попрощайся с игроками и подведи краткий итог их приключений.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
