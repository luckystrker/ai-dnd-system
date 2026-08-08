import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { locationStore } from "../lib/campaigns/locations.ts";
import { StoreError } from "../lib/campaigns/types.ts";

const connectionSchema = z.object({
  to: z.string().min(1).max(100).describe("Куда ведёт связь: имя или slug локации."),
  via: z.string().max(200).optional().describe("Через что: дорога, река, перевал и т.п."),
  discoveredDay: z.number().int().min(1).optional().describe("Когда связь обнаружена (игровой день)."),
});

export default defineTool({
  description:
    "Create or update a location of the campaign (map node): description, connections to other " +
    "locations, the day it was discovered, and whether the party is currently there. Locations live " +
    "in the campaign's locations/ folder. Only one location can be current at a time — setting " +
    "current=true clears it from the others. Use move_party to travel, not this tool.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    name: z.string().min(1).max(100).describe("Имя локации."),
    description: z.string().max(2000).optional().describe("Что известно о локации / что видно."),
    connections: z
      .array(connectionSchema)
      .optional()
      .describe("Связи с другими локациями (полностью заменяют существующие)."),
    discoveredDay: z.number().int().min(1).optional().describe("Игровой день, когда локацию впервые обнаружили."),
    current: z
      .boolean()
      .optional()
      .describe("true — партия находится здесь сейчас (снимает флаг с остальных). Обычно ставится через move_party."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const { campaignSlug: _slug, ...locationInput } = input;
      const location = locationStore.upsertLocation(campaign.id, locationInput);
      return {
        ok: true,
        location: {
          name: location.name,
          slug: location.slug,
          description: location.description ?? null,
          connections: location.connections,
          discoveredDay: location.discoveredDay ?? null,
          visitedDays: location.visitedDays,
          current: location.current === true,
        },
        note: "Локация сохранена в папке кампании (locations/).",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
