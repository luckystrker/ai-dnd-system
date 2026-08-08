import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { locationStore } from "../lib/campaigns/locations.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "List the campaign's known locations. By default returns discovered locations (the map the party " +
    "knows) with their connections; each entry notes whether the party is currently there. Read-only.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — кампания текущего чата."),
    includeUndiscovered: z
      .boolean()
      .optional()
      .default(false)
      .describe("Показать и необнаруженные локации (у которых нет discoveredDay)."),
  }),
  execute({ campaignSlug, includeUndiscovered }, ctx) {
    try {
      const campaign = resolveCampaign(ctx.session.auth.current, campaignSlug);
      if (!campaign) {
        return { ok: false, error: "Кампания не найдена: укажи campaignSlug или запусти кампанию в этом чате." };
      }
      let locations = locationStore.listLocations(campaign.id);
      if (!includeUndiscovered) {
        locations = locations.filter((location) => location.discoveredDay !== undefined);
      }
      const current = locations.find((location) => location.current === true);
      return {
        ok: true,
        count: locations.length,
        current: current?.name ?? null,
        locations: locations.map((location) => ({
          name: location.name,
          slug: location.slug,
          current: location.current === true,
          discoveredDay: location.discoveredDay ?? null,
          visitedDays: location.visitedDays,
          connections: location.connections,
          description: location.description ?? null,
        })),
        note: current ? `Партия сейчас в «${current.name}».` : "Текущая локация не задана.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
