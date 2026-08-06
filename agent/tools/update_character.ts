import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Update the dynamic state of a player character in the campaign: HP, conditions, inventory, " +
    "gold, XP, level, location. Call whenever the game changes a character's state (damage, loot, " +
    "level up, travel) so the party state stays current between sessions.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    character: z.string().min(1).max(100).describe("Имя (или slug) персонажа."),
    level: z.number().int().min(1).max(20).optional().describe("Новый уровень."),
    hp: z.number().int().min(-10).max(1000).optional().describe("Текущие хиты."),
    maxHp: z.number().int().min(1).max(1000).optional().describe("Максимальные хиты."),
    conditions: z
      .array(z.string().max(60))
      .max(20)
      .optional()
      .describe("Активные состояния (полный новый список): отравлен, оглушён и т.п."),
    inventory: z
      .array(z.string().max(120))
      .max(100)
      .optional()
      .describe("Полный новый список снаряжения и ценных предметов."),
    abilities: z
      .array(
        z.object({
          name: z.string().min(1).max(80),
          description: z.string().min(1).max(300),
          level: z.number().int().min(1).max(20).optional(),
        }),
      )
      .max(20)
      .optional()
      .describe("Полный новый список способностей и заклинаний персонажа."),
    gold: z.number().int().min(0).max(1_000_000).optional().describe("Золото."),
    xp: z.number().int().min(0).max(10_000_000).optional().describe("Опыт."),
    location: z.string().max(200).optional().describe("Где сейчас персонаж."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const { campaignSlug: _slug, character, ...patch } = input;
      const sheet = campaignStore.updateCharacter(campaign.id, character, patch);
      return {
        ok: true,
        character: {
          name: sheet.name,
          level: sheet.level,
          hp: sheet.hp ?? null,
          maxHp: sheet.maxHp ?? null,
          conditions: sheet.conditions ?? [],
          inventory: sheet.inventory ?? [],
          abilities: sheet.abilities ?? [],
          gold: sheet.gold ?? null,
          xp: sheet.xp ?? null,
          location: sheet.location ?? null,
        },
        note: "Состояние персонажа обновлено в папке кампании.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
