import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { npcStore } from "../lib/campaigns/npc.ts";
import { npcStatusSchema, StoreError } from "../lib/campaigns/types.ts";

const relationshipSchema = z.object({
  attitude: z
    .number()
    .int()
    .min(-5)
    .max(5)
    .describe("Отношение NPC к персонажу: -5 (враг) .. +5 (союзник)."),
  notes: z.string().max(300).optional().describe("Короткая заметка: почему NPC так относится."),
});

export default defineTool({
  description:
    "Create or update an NPC of the campaign: profile (role, status, location), relationships " +
    "toward party members and a memory note about what the players did to this NPC. NPCs live in " +
    "the campaign's npcs/ folder and are loaded dynamically. Call this whenever a notable NPC " +
    "appears or something changes for them.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    name: z.string().min(1).max(100).describe("Имя NPC."),
    role: z.string().max(300).optional().describe("Роль/описание: трактирщик, капитан стражи и т.п."),
    status: npcStatusSchema.optional().describe("Статус NPC: alive, dead, unknown."),
    location: z.string().max(200).optional().describe("Где NPC сейчас находится."),
    relationships: z
      .record(z.string(), relationshipSchema)
      .optional()
      .describe("Отношения к персонажам: ключ — имя персонажа."),
    firstSeenDay: z.number().int().min(1).optional().describe("Игровой день первого появления."),
    lastSeenDay: z.number().int().min(1).optional().describe("Игровой день последней встречи."),
    memoryAppend: z
      .string()
      .max(500)
      .optional()
      .describe("Что игроки сделали с этим NPC / что NPC запомнил (дописывается в память NPC)."),
    memoryAppendDay: z.number().int().min(1).optional().describe("Игровой день для пометки memoryAppend."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const { campaignSlug: _slug, ...npcInput } = input;
      const npc = npcStore.upsertNpc(campaign.id, npcInput);
      return {
        ok: true,
        npc: {
          name: npc.name,
          slug: npc.slug,
          role: npc.role ?? null,
          status: npc.status,
          location: npc.location ?? null,
          relationships: npc.relationships,
        },
        note: "NPC сохранён в папке кампании (npcs/).",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
