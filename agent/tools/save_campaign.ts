import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Save a new campaign after the creation interview is complete. The caller becomes the campaign owner and DM.",
  inputSchema: z.object({
    title: z.string().min(1).describe("Название кампании"),
    length: z.enum(["short", "medium", "long"]).describe("short = 1-3 сессии, medium = 4-10, long = 10+"),
    setting: z.string().min(1).describe("Сеттинг: мир и окружение кампании"),
    theme: z.string().min(1).describe("Основной лейтмотив кампании"),
    goal: z.string().optional().describe("Конечная цель игроков, если определена"),
    tone: z.string().optional().describe("Тон повествования"),
    openingScene: z.string().optional().describe("Стартовая сцена, с которой начнётся игра"),
    description: z.string().optional().describe("Развёрнутое описание кампании"),
  }),
  execute(input, ctx) {
    const identity = resolveCallerIdentity(ctx.session.auth.current);
    if (!identity) {
      return { ok: false, error: "Не удалось определить, кто вы. Кампанию можно создавать только из Telegram." };
    }
    try {
      const campaign = campaignStore.createCampaign(input, {
        userId: identity.userId,
        username: identity.username,
      });
      return {
        ok: true,
        campaign: {
          id: campaign.id,
          slug: campaign.slug,
          title: campaign.title,
          status: campaign.status,
        },
        note: "Кампания сохранена в статусе setup. Чтобы начать игру, её нужно запустить через start_campaign — тогда она привяжется к текущему чату.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
