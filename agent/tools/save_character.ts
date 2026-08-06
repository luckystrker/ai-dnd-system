import { defineTool } from "eve/tools";
import { z } from "zod";

import { characterCard } from "../lib/campaigns/card.ts";
import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Save a new player character into an existing campaign. The caller must be a campaign member. " +
    "A character cannot exist outside a campaign. Pass the starting equipment, abilities and HP " +
    "generated for the character's class/race/level so the sheet is complete from the start.",
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
    equipment: z
      .array(z.string().max(120))
      .max(50)
      .optional()
      .describe("Стартовое снаряжение: оружие, броня, фокус, припасы, личные вещи"),
    abilities: z
      .array(
        z.object({
          name: z.string().min(1).max(80).describe("Название способности/заклинания"),
          description: z.string().min(1).max(300).describe("Что она делает и как используется"),
          level: z.number().int().min(1).max(20).optional().describe("Минимальный уровень, по умолчанию текущий"),
        }),
      )
      .max(20)
      .optional()
      .describe("Способности и заклинания персонажа (1-2 для нового 1-го уровня)"),
    gold: z.number().int().min(0).max(1_000_000).optional().describe("Стартовое золото"),
    maxHp: z.number().int().min(1).max(1000).optional().describe("Максимальные хиты по классу"),
    hp: z.number().int().min(-10).max(1000).optional().describe("Текущие хиты, по умолчанию равны maxHp"),
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
        character: characterCard(sheet),
        note: "Персонаж сохранён в папке кампании. Покажи игроку полную карточку персонажа.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
