import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { npcStore } from "../lib/campaigns/npc.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Load the full card of one NPC: profile, relationships toward the party and the NPC's own " +
    "memory of what the players did. Use before roleplaying a conversation with a known NPC.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — кампания текущего чата."),
    name: z.string().min(1).max(100).describe("Имя (или slug) NPC."),
  }),
  execute({ campaignSlug, name }, ctx) {
    try {
      const campaign = resolveCampaign(ctx.session.auth.current, campaignSlug);
      if (!campaign) {
        return { ok: false, error: "Кампания не найдена: укажи campaignSlug или запусти кампанию в этом чате." };
      }
      const npc = npcStore.getNpc(campaign.id, name);
      if (!npc) {
        return { ok: false, error: `NPC «${name}» в кампании нет. Проверь список через list_npcs или создай через upsert_npc.` };
      }
      return {
        ok: true,
        npc: {
          name: npc.name,
          slug: npc.slug,
          role: npc.role ?? null,
          status: npc.status,
          location: npc.location ?? null,
          relationships: npc.relationships,
          firstSeenDay: npc.firstSeenDay ?? null,
          lastSeenDay: npc.lastSeenDay ?? null,
          memory: npc.memory || null,
        },
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
