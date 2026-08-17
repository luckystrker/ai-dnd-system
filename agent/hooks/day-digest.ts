/**
 * Страховочный авто-дайджест игрового дня.
 *
 * Покрывает провал: DM забыл делегировать chronicler после advance_day → день
 * остаётся без саммари → после compaction контекста факт теряется для LLM
 * (полный оригинал всегда сохраняется в транскрипте дня).
 *
 * На turn.completed: если у текущего дня НЕТ summary и НЕТ headline (chronicler
 * ещё не отработал), пишем детерминированный (не-LLM) дайджест из хвоста
 * транскрипта, помеченный AUTO_DIGEST_MARK. Chronicler затем перезапишет это
 * поле качественным саммари. Маркер делает хук идемпотентным — повторные срабатывания
 * не затирают готовое. Все ошибки глотаются (observe-only, как transcript.ts).
 */
import { defineHook } from "eve/hooks";

import { campaignStore } from "../lib/campaigns/store.ts";
import {
  AUTO_DIGEST_MARK,
  buildDayDigest,
  readDayTail,
  setDaySummary,
} from "../lib/campaigns/journal.ts";
import { readGameState } from "../lib/memory.ts";

function slugForTurn(): string | undefined {
  const { campaignId } = readGameState();
  if (!campaignId) return undefined;
  return campaignStore.getCampaign(campaignId)?.slug;
}

export default defineHook({
  events: {
    async "turn.completed"() {
      try {
        const slug = slugForTurn();
        if (!slug) return;
        const campaignId = readGameState().campaignId;
        const day = campaignId ? campaignStore.getCampaign(campaignId)?.currentDay ?? 1 : 1;
        const record = readDayTail(slug, day, 8);
        if (!record) return;
        // chronicler уже отработал (есть summary без маркера или есть headline) — не лезем.
        const hasHumanSummary = Boolean(record.summary) && !record.summary!.startsWith(AUTO_DIGEST_MARK);
        if (hasHumanSummary || record.headline) return;
        const digest = buildDayDigest(record.entries);
        if (!digest) return;
        setDaySummary(slug, day, digest);
      } catch (error) {
        console.error("day-digest hook (turn.completed) failed:", error);
      }
    },
  },
});
