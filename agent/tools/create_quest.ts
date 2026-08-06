import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Create a quest in the campaign: register an offered/accepted quest with its objective, " +
    "difficulty and optional reward plan. Use when an NPC offers the party a task or the players " +
    "take one on. The quest then lives in the campaign store and is shown in the injected memory. " +
    "Rewards: fill rewardPlan for special rewards; empty fields are computed from tables on completion.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    title: z.string().min(1).max(120).describe("Название квеста, короткое и узнаваемое."),
    giverNpc: z
      .string()
      .max(100)
      .optional()
      .describe("Кто выдал квест (имя NPC из его карточки, например «Бренна Хмурый»)."),
    objective: z.string().min(1).max(500).describe("Цель квеста: что нужно сделать, где, для кого."),
    difficulty: z
      .enum(["easy", "medium", "hard"])
      .describe("Сложность квеста: easy/medium/hard — по ней считаются XP и золото."),
    status: z
      .enum(["offered", "accepted", "active", "failed", "abandoned"])
      .optional()
      .describe("Статус квеста (по умолчанию offered; completed — только через complete_quest)."),
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
      .describe("Запланированная награда. Пустые поля на завершении заполняются по таблицам."),
    deadlineDay: z
      .number()
      .int()
      .min(1)
      .max(9999)
      .optional()
      .describe("Игровой день дедлайна: к нему мир требует результат."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const quest = campaignStore.createQuest(campaign.id, {
        title: input.title,
        giverNpcSlug: input.giverNpc,
        objective: input.objective,
        difficulty: input.difficulty,
        status: input.status,
        rewardPlan: input.rewardPlan,
        deadlineDay: input.deadlineDay,
      });
      return {
        ok: true,
        quest: {
          id: quest.id,
          title: quest.title,
          giverNpc: quest.giverNpcSlug ?? null,
          objective: quest.objective,
          difficulty: quest.difficulty,
          status: quest.status,
          rewardPlan: quest.rewardPlan ?? null,
          deadlineDay: quest.deadlineDay ?? null,
          createdDay: quest.createdDay,
        },
        note:
          "Квест создан и появится в секции «Активные квесты» памяти кампании. " +
          "Смена статусов — update_quest; завершение с выдачей наград — complete_quest.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
