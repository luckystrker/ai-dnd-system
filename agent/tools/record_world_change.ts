import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { StoreError } from "../lib/campaigns/types.ts";
import { upsertWorldChange } from "../lib/campaigns/world-state.ts";

export default defineTool({
  description:
    "Record a durable change to the world's current state — the opposite of " +
    "an append-only log: it holds what is true NOW, grouped by category. Use for deaths (" +
    "category «Погибшие»), destroyed/built places, decisions taken, alliance shifts. Prevents the " +
    "world from contradicting itself (e.g. reviving a dead NPC). Idempotent by text within a category.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    category: z
      .string()
      .min(1)
      .max(100)
      .describe("Категория факта: «Погибшие», «Изменения», «Решения» и т.п. (станет заголовком секции)."),
    text: z.string().min(1).max(300).describe("Содержание факта (текущее состояние мира)."),
    day: z.number().int().min(1).optional().describe("Игровой день, когда факт стал актуальным."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      upsertWorldChange(campaign.slug, {
        category: input.category,
        text: input.text,
        day: input.day,
      });
      return {
        ok: true,
        category: input.category,
        note: `Факт мира записан: [${input.category}] ${input.text}.`,
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
