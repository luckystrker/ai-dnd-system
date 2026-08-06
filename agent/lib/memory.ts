import { defineState } from "eve/context";

import { emptyCombatOrder, type CombatOrder } from "./engine/combat.ts";
import { MAX_PARTY } from "./campaigns/types.ts";

export { MAX_PARTY };

export interface PlayerCharacter {
  id: string;
  name: string;
  /** Telegram userId владельца персонажа (из листа, если известен). */
  ownerUserId?: string;
  stats: Record<string, number>;
}

export interface Enemy {
  name: string;
  hp: number;
  ac: number;
}

export interface GameState {
  started: boolean;
  /** Идентификатор кампании, к которой привязана текущая сессия. */
  campaignId?: string;
  scene: string;
  party: PlayerCharacter[];
  /** Активные враги боя (имена живых врагов из combat.order). */
  enemies: Enemy[];
  /** Порядок ходов боя: инициатива, текущий ход, раунд. */
  combat: CombatOrder;
  journal: string[];
}

/** Максимальный размер партии определён в campaigns/types.ts (реэкспорт выше). */

export const gameState = defineState("ttrpg.game", (): GameState => ({
  started: false,
  scene: "",
  party: [],
  enemies: [],
  combat: emptyCombatOrder(),
  journal: [],
}));

export function addToParty(character: PlayerCharacter): { ok: boolean; error?: string } {
  const state = gameState.get();
  if (state.party.length >= MAX_PARTY) {
    return { ok: false, error: `Party is full (max ${MAX_PARTY} players).` };
  }
  if (state.party.some((member) => member.name.toLowerCase() === character.name.toLowerCase())) {
    return { ok: false, error: `${character.name} is already in the party.` };
  }
  gameState.update((s) => ({ ...s, party: [...s.party, character] }));
  return { ok: true };
}

/**
 * Долговременная память кампании живёт в файлах папки кампании:
 * - history/days/day-NNNN.md — транскрипт по игровым дням (пишет hooks/transcript.ts);
 * - history/summary.md, history/key-events.md — саммари и ключевые моменты
 *   (пишет субагент chronicler, см. agent/lib/campaigns/journal.ts);
 * - npcs/ — профили и память NPC (agent/lib/campaigns/npc.ts);
 * - characters/ — листы персонажей с динамическим состоянием.
 * gameState выше — только per-session состояние текущей игры.
 */
