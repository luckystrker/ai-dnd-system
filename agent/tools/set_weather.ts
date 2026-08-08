import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Set the current weather/environment of the campaign (e.g. «ясно», «туман», «шторм», «сильный ветер»). " +
    "Weather has mechanical effects: fog/reduced visibility gives disadvantage on sight-based checks " +
    "(Perception/Investigation/Survival); strong wind/storm gives disadvantage on ranged attacks.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    weather: z
      .string()
      .min(1)
      .max(200)
      .describe("Текущая погода/окружение: «ясно», «туман», «дождь», «шторм», «сильный ветер» и т.п."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      campaignStore.setEnvironment(campaign.id, { weather: input.weather });
      return {
        ok: true,
        weather: input.weather,
        note: `Погода установлена: ${input.weather}.`,
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
