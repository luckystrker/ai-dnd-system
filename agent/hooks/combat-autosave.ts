/**
 * Автосохранение состояния боя в combat.md.
 *
 * gameState.combat/enemies живут только в сессии и теряются при рестарте.
 * Этот хук — observe-only (как transcript.ts): после боевых ходов пишет снимок
 * в папку кампании, чтобы при следующем старте сессии бой восстановился.
 * Любая ошибка глотается — боевой автосейв не должен ронять ход игры.
 *
 * Пишем только при изменении боевого состояния, чтобы не дёргать ФС каждый ход:
 * сравниваем с последним записанным снимком. Завершённый бой (combat.started ===
 * false) стирает сохранение.
 */
import { defineHook } from "eve/hooks";
import { toolResultFrom } from "eve/tools";

import { clearCombatState, saveCombatState } from "../lib/campaigns/combat-store.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { gameState, readGameState } from "../lib/memory.ts";
import combatTool from "../tools/combat.ts";
import initiativeTool from "../tools/initiative.ts";

/** Стал ли бой активным/изменился ли по сравнению с последним снимком. */
let lastSignature = "";

/** Кампания текущего хода: по campaignId из durable-состояния сессии. */
function slugForTurn(): string | undefined {
  const { campaignId } = readGameState();
  if (!campaignId) return undefined;
  return campaignStore.getCampaign(campaignId)?.slug;
}

/** Компактная подпись состояния боя для сравнения «изменилось ли». */
function combatSignature(): string {
  const { combat, enemies } = readGameState();
  if (!combat.started) return "";
  const order = combat.order
    .map((e) => `${e.id}:${e.hp ?? "?"}:${e.dodging ? "d" : ""}`)
    .join(",");
  return `${combat.round}:${combat.current}:${combat.acted}|${order}|${enemies
    .map((e) => `${e.id}:${e.hp}`)
    .join(",")}`;
}

function syncCombatSave(): void {
  const slug = slugForTurn();
  if (!slug) return;
  const { combat, enemies } = readGameState();
  if (!combat.started || enemies.length === 0) {
    // Бой завершён/пуст: стираем сохранение и сбрасываем подпись.
    if (lastSignature !== "") {
      clearCombatState(slug);
      lastSignature = "";
    }
    return;
  }
  const signature = combatSignature();
  if (signature === lastSignature) return; // ничего не поменялось
  saveCombatState(slug, { combat, enemies });
  lastSignature = signature;
}

export default defineHook({
  events: {
    async "action.result"(event) {
      try {
        const isCombat =
          toolResultFrom(event.data.result, combatTool) ||
          toolResultFrom(event.data.result, initiativeTool);
        if (!isCombat) return;
        syncCombatSave();
      } catch (error) {
        console.error("combat-autosave hook (action.result) failed:", error);
      }
    },
    async "turn.completed"() {
      try {
        syncCombatSave();
      } catch (error) {
        console.error("combat-autosave hook (turn.completed) failed:", error);
      }
    },
  },
});
