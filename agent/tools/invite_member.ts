import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError, memberRoleSchema } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Invite a user into a campaign. Only a campaign DM can invite; only the owner can invite another DM. Invitees join as players by default.",
  inputSchema: z.object({
    campaignId: z.string().describe("Идентификатор или slug кампании"),
    userId: z.string().min(1).describe("Telegram user id приглашаемого"),
    name: z.string().optional().describe("Имя пользователя в Telegram, если известно"),
    username: z.string().optional().describe("Username пользователя в Telegram, если известно"),
    role: memberRoleSchema.optional().describe("Роль в кампании; по умолчанию player. Роль dm назначает только владелец."),
  }),
  execute(input, ctx) {
    const identity = resolveCallerIdentity(ctx.session.auth.current);
    if (!identity) {
      return { ok: false, error: "Не удалось определить, кто вы." };
    }
    try {
      const campaign = campaignStore.addMember(input.campaignId, identity.userId, {
        userId: input.userId,
        name: input.name,
        username: input.username,
        role: input.role,
      });
      return {
        ok: true,
        member: { userId: input.userId, role: input.role ?? "player" },
        memberCount: campaign.members.length,
        note: "Участник добавлен. Теперь он может создавать персонажей в этой кампании.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
