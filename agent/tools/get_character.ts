import { defineTool } from "eve/tools";
import { z } from "zod";

import { characterCard } from "../lib/campaigns/card.ts";
import { resolveCampaign } from "../lib/campaigns/access.ts";
import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Show the full current character sheet: stats, HP, conditions, inventory, abilities, gold, XP, " +
    "location, background, motivation. Use when a player asks to see their character (/mychar) or " +
    "the current state of a party member.",
  inputSchema: z.object({
    campaignId: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — кампания текущего чата."),
    character: z
      .string()
      .optional()
      .describe("Имя (или slug) персонажа. Если не указан — берётся персонаж звонящего (должен быть ровно один)."),
  }),
  execute({ campaignId, character }, ctx) {
    try {
      const identity = resolveCallerIdentity(ctx.session.auth.current);
      if (!identity) {
        return { ok: false, error: "Не удалось определить, кто вы." };
      }
      const campaign = resolveCampaign(ctx.session.auth.current, campaignId);
      if (!campaign) {
        return { ok: false, error: "В этом чате нет привязанной кампании." };
      }
      const member = campaign.members.find((entry) => entry.userId === identity.userId);
      if (!member) {
        return { ok: false, error: "Вы не участник этой кампании." };
      }
      const characters = campaignStore.listCharacters(campaign.id);
      let sheet;
      if (character) {
        const needle = character.trim().toLowerCase();
        sheet = characters.find(
          (candidate) =>
            candidate.id === character ||
            candidate.slug.toLowerCase() === needle ||
            candidate.name.toLowerCase() === needle,
        );
        if (!sheet) {
          return { ok: false, error: `Персонаж «${character}» не найден в кампании.` };
        }
      } else {
        const own = characters.filter((candidate) => candidate.ownerUserId === identity.userId);
        if (own.length === 0) {
          return {
            ok: false,
            error: "У вас нет персонажа в этой кампании. Укажи имя персонажа или создай его через /newchar.",
          };
        }
        if (own.length > 1) {
          return {
            ok: false,
            error: `У вас несколько персонажей: ${own.map((candidate) => candidate.name).join(", ")}. Укажи имя.`,
          };
        }
        sheet = own[0];
      }
      return { ok: true, campaignSlug: campaign.slug, character: characterCard(sheet) };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
