/**
 * Автоматический транскрипт кампании.
 *
 * Пишет в history/days/day-NNNN.md текущей кампании:
 * - message.received  — что написал игрок (кто — из auth);
 * - message.completed — что ответил DM;
 * - action.result     — значимые игровые действия (броски, проверки, бой, инициатива).
 *
 * Хуки eve срабатывают at-least-once (ретрай шага переизлучает события с новыми
 * meta.id), поэтому дедупликация идёт по meta.id внутри файла дня.
 */
import { defineHook } from "eve/hooks";
import { toolResultFrom } from "eve/tools";

import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import type { Campaign } from "../lib/campaigns/types.ts";
import { appendTranscriptEntry } from "../lib/campaigns/journal.ts";
import { gameState } from "../lib/memory.ts";
import combatTool from "../tools/combat.ts";
import initiativeTool from "../tools/initiative.ts";
import rollDiceTool from "../tools/roll_dice.ts";
import skillCheckTool from "../tools/skill_check.ts";

/** Кампания для сообщения игрока: по чату из auth (работает ещё до hydrate). */
function campaignForInbound(auth: unknown): Campaign | undefined {
  const identity = resolveCallerIdentity(auth);
  if (!identity?.chatId) return undefined;
  return campaignStore.findByBoundChat(identity.chatId, identity.messageThreadId);
}

/** Кампания для событий хода: по campaignId из durable-состояния сессии. */
function campaignForTurn(): Campaign | undefined {
  const { campaignId } = gameState.get();
  if (!campaignId) return undefined;
  return campaignStore.getCampaign(campaignId);
}

export default defineHook({
  events: {
    async "message.received"(event, ctx) {
      try {
        const campaign = campaignForInbound(ctx.session.auth.current);
        if (!campaign) return;
        const text = event.data.message?.trim();
        if (!text) return;
        const identity = resolveCallerIdentity(ctx.session.auth.current);
        appendTranscriptEntry(campaign.slug, campaign.currentDay ?? 1, {
          kind: "player",
          author: identity?.username ?? identity?.userId,
          text,
          eventId: event.meta.id,
          at: event.meta.at,
        });
      } catch (error) {
        // Хук observe-only: ошибка транскрипта не должна ронять ход игры.
        console.error("transcript hook (message.received) failed:", error);
      }
    },

    async "message.completed"(event) {
      try {
        const text = event.data.message?.trim();
        if (!text) return;
        const campaign = campaignForTurn();
        if (!campaign) return;
        appendTranscriptEntry(campaign.slug, campaign.currentDay ?? 1, {
          kind: "dm",
          text,
          eventId: event.meta.id,
          at: event.meta.at,
        });
      } catch (error) {
        console.error("transcript hook (message.completed) failed:", error);
      }
    },

    async "action.result"(event) {
      try {
        const campaign = campaignForTurn();
        if (!campaign) return;
        const day = campaign.currentDay ?? 1;
        const logAction = (label: string, output: unknown): void => {
          appendTranscriptEntry(campaign.slug, day, {
            kind: "action",
            text: `${label}: ${String(output)}`,
            eventId: event.meta.id,
            at: event.meta.at,
          });
        };
        const roll = toolResultFrom(event.data.result, rollDiceTool);
        if (roll) logAction("Бросок костей", roll.output);
        const check = toolResultFrom(event.data.result, skillCheckTool);
        if (check) logAction("Проверка навыка", check.output);
        const combat = toolResultFrom(event.data.result, combatTool);
        if (combat) logAction("Бой", combat.output);
        const initiative = toolResultFrom(event.data.result, initiativeTool);
        if (initiative) logAction("Инициатива", initiative.output.replace(/\n/g, "; "));
      } catch (error) {
        console.error("transcript hook (action.result) failed:", error);
      }
    },
  },
});
