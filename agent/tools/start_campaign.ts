import { defineTool } from "eve/tools";
import { z } from "zod";

import { hydrateGameState } from "../lib/campaigns/hydrate.ts";
import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Start a campaign in the current chat: binds the campaign to this chat (or forum topic) and makes it active. Only the campaign DM can do this. One chat/topic can host only one active campaign.",
  inputSchema: z.object({
    campaignId: z.string().describe("Идентификатор или slug кампании"),
  }),
  execute({ campaignId }, ctx) {
    const identity = resolveCallerIdentity(ctx.session.auth.current);
    if (!identity) {
      return { ok: false, error: "Не удалось определить, кто вы." };
    }
    if (!identity.chatId) {
      return { ok: false, error: "Не удалось определить текущий чат." };
    }
    try {
      const campaign = campaignStore.bindAndActivate(campaignId, identity.userId, {
        chatId: identity.chatId,
        messageThreadId: identity.messageThreadId,
      });
      const characters = campaignStore.listCharacters(campaign.id);
      hydrateGameState(campaign, characters);
      return {
        ok: true,
        campaign: {
          id: campaign.id,
          slug: campaign.slug,
          title: campaign.title,
          setting: campaign.setting,
          theme: campaign.theme,
          goal: campaign.goal ?? null,
          tone: campaign.tone ?? null,
          openingScene: campaign.openingScene ?? null,
          boundChat: campaign.boundChat,
        },
        characters: characters.map((sheet) => ({
          id: sheet.id,
          name: sheet.name,
          characterClass: sheet.characterClass,
          race: sheet.race,
          level: sheet.level,
        })),
        note: "Кампания привязана к этому чату. Открой первую сцену и представь мир игрокам.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
