import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";

export default defineTool({
  description: "List campaigns the caller owns or is a member of, with their statuses.",
  inputSchema: z.object({}),
  execute(_input, ctx) {
    const identity = resolveCallerIdentity(ctx.session.auth.current);
    if (!identity) {
      return { ok: false, error: "Не удалось определить, кто вы." };
    }
    const campaigns = campaignStore.listForUser(identity.userId).map((campaign) => ({
      id: campaign.id,
      slug: campaign.slug,
      title: campaign.title,
      status: campaign.status,
      length: campaign.length,
      setting: campaign.setting,
      isOwner: campaign.ownerUserId === identity.userId,
      memberCount: campaign.members.length,
      characterCount: campaignStore.listCharacters(campaign.id).length,
      boundChat: campaign.boundChat ?? null,
    }));
    return { ok: true, campaigns };
  },
});
