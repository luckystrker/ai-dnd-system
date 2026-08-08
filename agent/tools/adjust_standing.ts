import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { factionStore } from "../lib/campaigns/factions.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Adjust a faction's standing (reputation) toward the party by a delta, clamped to -5 .. +5. Use " +
    "for in-story reputation shifts (a favor done, a slight). Quest completion adjusts standing " +
    "automatically via complete_quest — use this tool for standalone events. The faction must exist " +
    "(create it first with upsert_faction).",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    faction: z.string().min(1).max(100).describe("Имя (или slug) фракции."),
    delta: z
      .number()
      .int()
      .min(-5)
      .max(5)
      .describe("На сколько изменить репутацию (клампится в -5 .. +5)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const faction = factionStore.adjustStanding(campaign.id, input.faction, input.delta);
      return {
        ok: true,
        faction: { name: faction.name, slug: faction.slug, standing: faction.standing },
        delta: input.delta,
        note: `Репутация фракции «${faction.name}» теперь ${faction.standing}.`,
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
