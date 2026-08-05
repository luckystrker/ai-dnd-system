import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Save a new player character into an existing campaign. The caller must be a campaign member. A character cannot exist outside a campaign.",
  inputSchema: z.object({
    campaignId: z.string().describe("Идентификатор или slug кампании"),
    name: z.string().min(1).describe("Имя персонажа"),
    characterClass: z.string().min(1).describe("Класс персонажа (D&D 5e)"),
    race: z.string().min(1).describe("Раса персонажа"),
    level: z.number().int().min(1).max(20).optional().describe("Уровень, по умолчанию 1"),
    stats: z
      .record(z.string(), z.number())
      .optional()
      .describe("Характеристики: strength, dexterity, constitution, intelligence, wisdom, charisma и другие числа"),
    background: z.string().optional().describe("Предыстория персонажа"),
    motivation: z.string().optional().describe("Мотивация и цели персонажа"),
    appearance: z
      .string()
      .max(300)
      .optional()
      .describe(
        "Внешность персонажа для иллюстраций сцен: черты лица, волосы, одежда, приметы — короткое описание на английском",
      ),
  }),
  execute(input, ctx) {
    const identity = resolveCallerIdentity(ctx.session.auth.current);
    if (!identity) {
      return { ok: false, error: "Не удалось определить, кто вы." };
    }
    try {
      const { campaignId, ...character } = input;
      const sheet = campaignStore.saveCharacter(campaignId, identity.userId, character);
      return {
        ok: true,
        character: {
          id: sheet.id,
          name: sheet.name,
          slug: sheet.slug,
          characterClass: sheet.characterClass,
          race: sheet.race,
          level: sheet.level,
        },
        note: "Персонаж сохранён в папке кампании.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
