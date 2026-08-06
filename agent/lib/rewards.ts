/**
 * Таблицы наград и уровня: детерминированная часть выдачи наград за квесты.
 *
 * Идея: DM не фантазирует числа, а опирается на таблицы по сложности квеста
 * и уровню партии; свободная часть (уникальные предметы, услуги NPC,
 * репутация) остаётся на усмотрение DM и задаётся в rewardPlan квеста.
 */
import type { QuestDifficulty } from "./campaigns/types.ts";

/** Накопительные пороги XP на уровень (упрощённый D&D 5e, для справки). */
export const XP_THRESHOLDS: Record<number, number> = {
  1: 0,
  2: 300,
  3: 900,
  4: 2700,
  5: 6500,
  6: 14000,
  7: 23000,
  8: 34000,
  9: 48000,
  10: 64000,
  11: 85000,
  12: 100000,
  13: 120000,
  14: 140000,
  15: 165000,
  16: 195000,
  17: 225000,
  18: 265000,
  19: 305000,
  20: 355000,
};

/** Порог XP для достижения уровня (для level >= 20 возвращает 355000). */
export function xpForLevel(level: number): number {
  return XP_THRESHOLDS[level] ?? XP_THRESHOLDS[20];
}

/** Какой уровень соответствует накопленному XP (максимум 20). */
export function levelForXp(xp: number): number {
  let level = 1;
  for (const [candidate, threshold] of Object.entries(XP_THRESHOLDS)) {
    if (xp >= threshold) level = Number(candidate);
  }
  return level;
}

/**
 * Доля пути от текущего уровня к следующему, которую приносит квест.
 * Средний квест ≈ четверть уровня партии.
 */
export const QUEST_XP_SHARE: Record<QuestDifficulty, number> = {
  easy: 0.12,
  medium: 0.25,
  hard: 0.4,
};

/** XP каждому персонажу за квест по сложности и уровню партии. */
export function questXpPerCharacter(difficulty: QuestDifficulty, partyLevel: number): number {
  const clamped = Math.min(19, Math.max(1, partyLevel));
  const span = xpForLevel(clamped + 1) - xpForLevel(clamped);
  return Math.max(10, Math.round(span * QUEST_XP_SHARE[difficulty]));
}

/** Базовый диапазон золота на партию по сложности (до множителя уровня). */
export const QUEST_GOLD_BASE: Record<QuestDifficulty, [number, number]> = {
  easy: [25, 75],
  medium: [100, 300],
  hard: [500, 1500],
};

/** Диапазон золота на партию за квест: базовый × множитель уровня партии. */
export function questGoldRange(difficulty: QuestDifficulty, partyLevel: number): [number, number] {
  const [low, high] = QUEST_GOLD_BASE[difficulty];
  const multiplier = 1 + Math.max(0, partyLevel - 1) * 0.25;
  return [Math.round(low * multiplier), Math.round(high * multiplier)];
}

/** Середина диапазона (для дефолтной выдачи без броска). */
export function middleOf(range: [number, number]): number {
  return Math.round((range[0] + range[1]) / 2);
}

/** Кость хитов по классу (среднее + бонус ВЫН при level-up). */
export const CLASS_HIT_DIE: Record<string, number> = {
  barbarian: 12,
  fighter: 10,
  paladin: 10,
  ranger: 10,
  artificer: 8,
  bard: 8,
  cleric: 8,
  druid: 8,
  monk: 8,
  rogue: 8,
  warlock: 8,
  wizard: 6,
  sorcerer: 6,
};

/** Русские названия классов → канонический ключ таблицы костей хитов. */
const CLASS_ALIASES: Record<string, string> = {
  варвар: "barbarian",
  "воин": "fighter",
  боец: "fighter",
  паладин: "paladin",
  следопыт: "ranger",
  рейнджер: "ranger",
  изобретатель: "artificer",
  бард: "bard",
  жрец: "cleric",
  клирик: "cleric",
  друид: "druid",
  монах: "monk",
  плут: "rogue",
  вор: "rogue",
  колдун: "warlock",
  чародей: "sorcerer",
  волшебник: "wizard",
  маг: "wizard",
};

function canonicalClass(characterClass: string): string {
  const key = characterClass.trim().toLowerCase();
  return CLASS_ALIASES[key] ?? key;
}

/** Кость хитов класса; неизвестные классы получают d8. */
export function classHitDie(characterClass: string): number {
  return CLASS_HIT_DIE[canonicalClass(characterClass)] ?? 8;
}

/** Модификатор характеристики: floor((stat - 10) / 2). */
export function statModifier(stat: number): number {
  return Math.floor((stat - 10) / 2);
}
