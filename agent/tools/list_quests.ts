import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

const ACTIVE_STATUSES = new Set(["offered", "accepted", "active"]);

export default defineTool({
  description:
    "List the campaign's quests, active ones by default. Use to answer the /quests command (the " +
    "party's journal) or to check quest state and deadlines. Read-only: any campaign member can call it.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    status: z
      .enum(["offered", "accepted", "active", "completed", "failed", "abandoned"])
      .optional()
      .describe("Показать только квесты этого статуса."),
    includeAll: z
      .boolean()
      .optional()
      .describe("Показать все квесты, включая завершённые (по умолчанию только активные)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaign(ctx.session.auth.current, input.campaignSlug);
      if (!campaign) {
        return { ok: false, error: "В этом чате нет привязанной кампании." };
      }
      const quests = campaignStore.listQuests(campaign.id);
      const filtered = quests.filter((quest) => {
        if (input.status) return quest.status === input.status;
        if (!input.includeAll) return ACTIVE_STATUSES.has(quest.status);
        return true;
      });
      return {
        ok: true,
        quests: filtered.map((quest) => ({
          id: quest.id,
          title: quest.title,
          objective: quest.objective,
          difficulty: quest.difficulty,
          status: quest.status,
          giverNpc: quest.giverNpcSlug ?? null,
          deadlineDay: quest.deadlineDay ?? null,
          createdDay: quest.createdDay,
        })),
        note:
          "Для игроков (команда /quests) показывай активные квесты в игровом виде: название, цель, " +
          "кто выдал, дедлайн. Запланированную награду (rewardPlan) игрокам не раскрывай.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
