import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Append an open thread to the campaign ledger: a promise, mystery, debt or unresolved matter " +
    "that must resurface later (\"promised the mayor\", \"who is the hooded man\", \"owe a favor\"). " +
    "Open threads are injected into the DM memory every turn, so old hooks stay alive. " +
    "Close them with resolve_thread when the story resolves them.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    text: z
      .string()
      .min(1)
      .max(300)
      .describe("Нить одним предложением: кто кому что обещал/должен, какая тайна открыта."),
    kind: z
      .enum(["promise", "mystery", "debt", "unresolved"])
      .optional()
      .describe("Тип нити: promise — обещание, mystery — тайна, debt — долг, unresolved — незавершённое."),
    linkedQuest: z
      .string()
      .max(150)
      .optional()
      .describe("Название квеста, к которому относится нить (свяжет их для закрытия)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      let linkedQuestId: string | undefined;
      if (input.linkedQuest) {
        const needle = input.linkedQuest.toLowerCase();
        const quest = campaignStore.listQuests(campaign.id).find(
          (candidate) =>
            candidate.id === input.linkedQuest ||
            candidate.slug.toLowerCase() === needle ||
            candidate.title.toLowerCase() === needle,
        );
        if (!quest) {
          throw new StoreError(`Квест «${input.linkedQuest}» не найден — нить не создана.`, "not_found");
        }
        linkedQuestId = quest.id;
      }
      const thread = campaignStore.appendThread(campaign.id, {
        text: input.text,
        kind: input.kind,
        linkedQuestId,
      });
      return {
        ok: true,
        thread: {
          id: thread.id,
          text: thread.text,
          kind: thread.kind,
          status: thread.status,
          dayOpened: thread.dayOpened,
        },
        note: "Нить добавлена и будет всплывать в памяти DM, пока не закрыта (resolve_thread).",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
