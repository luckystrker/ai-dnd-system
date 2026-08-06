import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { npcStore } from "../lib/campaigns/npc.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "List the NPCs of the campaign as a short roster (name, role, status, location). " +
    "Use get_npc to load a full card with relationships and memory.",
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
      const npcs = npcStore.listNpcs(campaign.id);
      return {
        ok: true,
        count: npcs.length,
        npcs: npcs.map((npc) => ({
          name: npc.name,
          role: npc.role ?? null,
          status: npc.status,
          location: npc.location ?? null,
          lastSeenDay: npc.lastSeenDay ?? null,
        })),
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
