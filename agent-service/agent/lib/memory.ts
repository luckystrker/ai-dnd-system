import { defineState } from "eve/context";

import { MAX_PARTY } from "./campaigns/types.ts";

export { MAX_PARTY };

export interface PlayerCharacter {
  id: string;
  name: string;
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
  enemies: Enemy[];
  turn: number;
  journal: string[];
}

/** Максимальный размер партии определён в campaigns/types.ts (реэкспорт выше). */

export const gameState = defineState("ttrpg.game", (): GameState => ({
  started: false,
  scene: "",
  party: [],
  enemies: [],
  turn: 0,
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
 * Долговременная память: пока заготовка.
 * TODO: реализовать персистентное хранилище (например Postgres + pgvector),
 * чтобы кампания переживала сессии, а recall работал по всем прошедшим событиям.
 */
export interface MemoryStore {
  remember(key: string, value: string): void;
  recall(key: string): string | undefined;
}

export const memory: MemoryStore = {
  remember(_key, _value) {
    // TODO: сохранить в хранилище
  },
  recall() {
    return undefined; // TODO: найти в хранилище
  },
};
