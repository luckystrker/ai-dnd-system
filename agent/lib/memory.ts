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
  /** Уровень и хиты — из листа при гидратации; HP обновляется в бою. */
  level?: number;
  hp?: number;
  maxHp?: number;
}

export interface Enemy {
  /** Стабильный id врага (совпадает с combat.order entry id). */
  id: string;
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
  /**
   * Последние грани d20 из skill_check (для «доброго» псевдорандома: если серия
   * низких бросков, следующий слегка подтягивается). Хранятся только последние
   * LOW_STREAK_WINDOW значений. Боевые броски сюда не пишутся.
   */
  diceHistory: number[];
}

/** Максимальный размер партии определён в campaigns/types.ts (реэкспорт выше). */

export const gameState = defineState("ttrpg.game", (): GameState => ({
  started: false,
  scene: "",
  party: [],
  enemies: [],
  combat: emptyCombatOrder(),
  journal: [],
  diceHistory: [],
}));

/**
 * Нормализация сырого durable-состояния: подставляет дефолты отсутствующих полей.
 *
 * Durable-состояние сессии сохраняется в контексте eve как есть; если сессия
 * старше кода (поля GameState добавлялись со временем: combat, diceHistory),
 * `defineState` при восстановлении возвращает старый объект без новых полей, а
 * инициализатор повторно не запускается. Это валило skill_check и combat-autosave
 * (`undefined.slice` / `undefined.started`). Выделена отдельно для тестируемости.
 */
export function normalizeGameState(raw: Record<string, unknown>): GameState {
  return {
    started: raw.started === true,
    campaignId: typeof raw.campaignId === "string" ? raw.campaignId : undefined,
    scene: typeof raw.scene === "string" ? raw.scene : "",
    party: Array.isArray(raw.party) ? (raw.party as PlayerCharacter[]) : [],
    enemies: Array.isArray(raw.enemies) ? (raw.enemies as Enemy[]) : [],
    combat: (raw.combat as CombatOrder | undefined) ?? emptyCombatOrder(),
    journal: Array.isArray(raw.journal) ? (raw.journal as string[]) : [],
    diceHistory: Array.isArray(raw.diceHistory) ? (raw.diceHistory as number[]) : [],
  };
}

/**
 * Нормализованное чтение состояния: подставляет дефолты отсутствующих полей.
 *
 * Durable-состояние сессии сохраняется в контексте eve как есть; если сессия
 * старше кода (поля GameState добавлялись со временем: combat, diceHistory),
 * `defineState` при восстановлении возвращает старый объект без новых полей, а
 * инициализатор повторно не запускается. Это валило skill_check и combat-autosave
 * (`undefined.slice` / `undefined.started`). Читать gameState нужно только через
 * эту функцию, чтобы недостающие поля получали безопасные значения.
 */
export function readGameState(): GameState {
  return normalizeGameState(gameState.get() as unknown as Record<string, unknown>);
}

export function addToParty(character: PlayerCharacter): { ok: boolean; error?: string } {
  const state = readGameState();
  if (state.party.length >= MAX_PARTY) {
    return { ok: false, error: `Party is full (max ${MAX_PARTY} players).` };
  }
  if (state.party.some((member) => member.name.toLowerCase() === character.name.toLowerCase())) {
    return { ok: false, error: `${character.name} is already in the party.` };
  }
  gameState.update((s) => ({ ...s, party: [...(Array.isArray(s.party) ? s.party : []), character] }));
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
