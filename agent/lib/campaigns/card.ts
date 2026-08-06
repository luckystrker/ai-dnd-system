/**
 * Преобразование листа персонажа в плоскую структуру для ответа тулов.
 * Полная карточка: статы, хиты, инвентарь, способности, золото, XP, локация.
 */
import type { CharacterSheet } from "./types.ts";

export interface CharacterCard {
  id: string;
  name: string;
  slug: string;
  ownerUserId: string;
  characterClass: string;
  race: string;
  level: number;
  stats: Record<string, number>;
  hp: number | null;
  maxHp: number | null;
  conditions: string[];
  inventory: string[];
  abilities: { name: string; description: string; level: number | null }[];
  gold: number | null;
  xp: number | null;
  location: string | null;
  background: string | null;
  motivation: string | null;
  appearance: string | null;
  createdAt: string;
  updatedAt: string | null;
}

export function characterCard(sheet: CharacterSheet): CharacterCard {
  return {
    id: sheet.id,
    name: sheet.name,
    slug: sheet.slug,
    ownerUserId: sheet.ownerUserId,
    characterClass: sheet.characterClass,
    race: sheet.race,
    level: sheet.level,
    stats: sheet.stats,
    hp: sheet.hp ?? null,
    maxHp: sheet.maxHp ?? null,
    conditions: sheet.conditions ?? [],
    inventory: sheet.inventory ?? [],
    abilities: (sheet.abilities ?? []).map((ability) => ({
      name: ability.name,
      description: ability.description,
      level: ability.level ?? null,
    })),
    gold: sheet.gold ?? null,
    xp: sheet.xp ?? null,
    location: sheet.location ?? null,
    background: sheet.background ?? null,
    motivation: sheet.motivation ?? null,
    appearance: sheet.appearance ?? null,
    createdAt: sheet.createdAt,
    updatedAt: sheet.updatedAt ?? null,
  };
}
