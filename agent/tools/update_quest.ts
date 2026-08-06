import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Update a quest: change its status (offered/accepted/active/failed/abandoned), objective, " +
    "difficulty, reward plan or deadline. Use when the story moves the quest forward. " +
    "To COMPLETE a quest with rewards use complete_quest instead.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    quest: z.string().min(1).max(150).describe("Название (или slug/id) квеста."),
    status: z
      .enum(["offered", "accepted", "active", "failed", "abandoned"])
      .optional()
      .describe("Новый статус (completed — только через complete_quest)."),
    title: z.string().min(1).max(120).optional().describe("Новое название квеста."),
    objective: z.string().min(1).max(500).optional().describe("Новая цель квеста."),
    difficulty: z.enum(["easy", "medium", "hard"]).optional().describe("Новая сложность."),
    deadlineDay: z.number().int().min(1).max(9999).optional().describe("Новый игровой день дедлайна."),
    rewardPlan: z
      .object({
        xp: z.number().int().min(0).max(10_000_000).optional().describe("XP каждому участнику."),
        gold: z.number().int().min(0).max(1_000_000).optional().describe("Золото на всю партию."),
        items: z.array(z.string().max(120)).max(20).optional().describe("Предметы (каждому участнику)."),
        note: z
          .string()
          .max(300)
          .optional()
          .describe("Свободная часть награды: услуга NPC, репутация, сюжетный бонус."),
      })
      .optional()
      .describe("Запланированная награда (заменяет прежнюю целиком)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const { campaignSlug: _slug, quest: questRef, ...patch } = input;
      const updated = campaignStore.updateQuest(campaign.id, questRef, patch);
      return {
        ok: true,
        quest: {
          id: updated.id,
          title: updated.title,
          objective: updated.objective,
          difficulty: updated.difficulty,
          status: updated.status,
          rewardPlan: updated.rewardPlan ?? null,
          deadlineDay: updated.deadlineDay ?? null,
        },
        note: "Квест обновлён. Если квест завершён в сюжете — вызови complete_quest, чтобы выдать награды.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
