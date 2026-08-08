import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { readLedger } from "../lib/campaigns/journal.ts";
import { StoreError, type LedgerType } from "../lib/campaigns/types.ts";

export default defineTool({
  description:
    "Read the campaign loot/economy ledger (history/ledger.md): an append-only log of gold and items " +
    "found or spent, recorded automatically when characters are granted loot or quests completed. " +
    "Optionally filter by in-game day or by found/spent, and limit the number of entries. The ledger " +
    "is not loaded into the memory block (it grows) — use this tool or search_memory to recall it.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Идентификатор или slug кампании. Если не указан — кампания текущего чата."),
    day: z.number().int().min(1).optional().describe("Только записи этого игрового дня."),
    type: z
      .enum(["found", "spent"])
      .optional()
      .describe("Только найденное (found) или только потраченное (spent)."),
    limit: z.number().int().min(1).max(200).optional().default(50).describe("Сколько последних записей вернуть."),
  }),
  execute({ campaignSlug, day, type, limit }, ctx) {
    try {
      const campaign = resolveCampaign(ctx.session.auth.current, campaignSlug);
      if (!campaign) {
        return { ok: false, error: "Кампания не найдена: укажи campaignSlug или запусти кампанию в этом чате." };
      }
      const filter: { day?: number; type?: LedgerType } = {};
      if (day !== undefined) filter.day = day;
      if (type !== undefined) filter.type = type;
      const entries = readLedger(campaign.slug, Object.keys(filter).length > 0 ? filter : undefined, limit);
      return {
        ok: true,
        count: entries.length,
        entries,
        note: entries.length === 0 ? "Журнал пуст (или нет записей по фильтру)." : "Записи — от старых к новым.",
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
