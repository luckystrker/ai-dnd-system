import { defineTool } from "eve/tools";
import { z } from "zod";

import { hydrateGameState } from "../lib/campaigns/hydrate.ts";
import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";

export default defineTool({
  description:
    "Load the campaign bound to the current chat (or a given campaign) and its characters into context. Call this at the first turn of a new session in a game chat before narrating anything.",
  inputSchema: z.object({
    campaignId: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — ищется кампания, привязанная к текущему чату."),
  }),
  execute({ campaignId }, ctx) {
    const identity = resolveCallerIdentity(ctx.session.auth.current);
    if (!identity) {
      return { ok: false, error: "Не удалось определить, кто вы." };
    }
    const campaign = campaignId
      ? campaignStore.getCampaign(campaignId)
      : identity.chatId
        ? campaignStore.findByBoundChat(identity.chatId, identity.messageThreadId)
        : undefined;
    if (!campaign) {
      return {
        ok: true,
        campaign: null,
        note: "В этом чате нет привязанной кампании. Можно создать новую (/newcampaign) или запустить существующую (/startcampaign).",
      };
    }
    const characters = campaignStore.listCharacters(campaign.id);
    if (campaign.status === "active") {
      hydrateGameState(campaign, characters);
    }
    return {
      ok: true,
      campaign: {
        id: campaign.id,
        slug: campaign.slug,
        title: campaign.title,
        status: campaign.status,
        length: campaign.length,
        setting: campaign.setting,
        theme: campaign.theme,
        goal: campaign.goal ?? null,
        tone: campaign.tone ?? null,
        openingScene: campaign.openingScene ?? null,
        members: campaign.members.map((member) => ({
          userId: member.userId,
          name: member.name ?? member.username ?? member.userId,
          role: member.role,
        })),
      },
      characters: characters.map((sheet) => ({
        id: sheet.id,
        name: sheet.name,
        ownerUserId: sheet.ownerUserId,
        characterClass: sheet.characterClass,
        race: sheet.race,
        level: sheet.level,
        stats: sheet.stats,
        motivation: sheet.motivation ?? null,
        background: sheet.background ?? null,
      })),
    };
  },
});
