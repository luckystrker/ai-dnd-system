export interface DiceResult {
  rolls: number[];
  total: number;
}

export interface CheckResult {
  roll: number;
  modifier: number;
  total: number;
  difficulty: number;
  success: boolean;
  margin: number;
  /** Аббревиатура характеристики, против которой шла проверка (str/dex/...). */
  ability: string;
  /** Выпала натуральная 20 — успех независимо от DC. */
  naturalSuccess: boolean;
  /** Выпала натуральная 1 — провал независимо от модификаторов. */
  naturalFailure: boolean;
}

export const SKILL_ABILITY_MAP: Record<string, string> = {
  athletics: "str",
  acrobatics: "dex",
  sleight_of_hand: "dex",
  stealth: "dex",
  arcana: "int",
  history: "int",
  investigation: "int",
  nature: "int",
  religion: "int",
  animal_handling: "wis",
  insight: "wis",
  medicine: "wis",
  perception: "wis",
  survival: "wis",
  deception: "cha",
  intimidation: "cha",
  performance: "cha",
  persuasion: "cha",
};

/** Русские названия навыков (агент часто передаёт "Восприятие" или "Восприятие (Perception)"). */
const SKILL_RU_ALIASES: Record<string, string> = {
  атлетика: "athletics",
  акробатика: "acrobatics",
  "ловкость рук": "sleight_of_hand",
  скрытность: "stealth",
  магия: "arcana",
  аркана: "arcana",
  история: "history",
  расследование: "investigation",
  природа: "nature",
  религия: "religion",
  "уход за животными": "animal_handling",
  проницательность: "insight",
  медицина: "medicine",
  восприятие: "perception",
  выживание: "survival",
  обман: "deception",
  запугивание: "intimidation",
  выступление: "performance",
  убеждение: "persuasion",
};

/**
 * Разрешает название навыка в аббревиатуру характеристики. Понимает
 * английские и русские названия, включая смешанные "Восприятие (Perception)".
 * Неизвестный навык консервативно resolves в "str".
 */
export function resolveSkillAbility(skill: string): string {
  const name = skill.trim().toLowerCase();
  const candidates: string[] = [name];
  const paren = /\(([^)]+)\)/.exec(name);
  if (paren) candidates.push(paren[1].trim());
  for (const candidate of candidates) {
    const underscored = candidate.replace(/[\s-]+/g, "_");
    const mapped = SKILL_ABILITY_MAP[underscored] ?? SKILL_RU_ALIASES[candidate];
    if (mapped) return SKILL_ABILITY_MAP[mapped] ?? mapped;
  }
  return "str";
}

/** Полные и сокращённые названия характеристик (листы хранят strength, dexterity, ...). */
const ABILITY_ALIASES: Record<string, string> = {
  strength: "str",
  str: "str",
  dexterity: "dex",
  dex: "dex",
  constitution: "con",
  con: "con",
  intelligence: "int",
  int: "int",
  wisdom: "wis",
  wis: "wis",
  charisma: "cha",
  cha: "cha",
};

/** Достаёт числовое значение характеристики по любому из её написаний. */
export function abilityScore(stats: Record<string, unknown>, ability: string): number | undefined {
  const key = ABILITY_ALIASES[ability.trim().toLowerCase()] ?? ability.trim().toLowerCase();
  for (const [rawKey, rawValue] of Object.entries(stats)) {
    if (ABILITY_ALIASES[rawKey.trim().toLowerCase()] !== key) continue;
    const parsed = typeof rawValue === "number" ? rawValue : Number(rawValue);
    if (Number.isFinite(parsed)) return Math.trunc(parsed);
  }
  return undefined;
}

/**
 * Модификатор характеристики (D&D 5e): floor((score - 10) / 2).
 * Без значения характеристики — 0 (эквивалент счёта 10).
 */
export function abilityModifier(stats: Record<string, unknown>, ability: string): number {
  const score = abilityScore(stats, ability) ?? 10;
  return Math.floor((score - 10) / 2);
}

/** Бонус мастерства по уровню (D&D 5e): +2 на 1-4, +3 на 5-8, +4 на 9-12, +5 на 13-16, +6 на 17-20. */
export function proficiencyBonus(level: number): number {
  const clamped = Number.isFinite(level) ? Math.max(1, Math.floor(level)) : 1;
  return Math.floor((clamped - 1) / 4) + 2;
}

export type RandomSource = () => number;

function randomInt(sides: number, random: RandomSource): number {
  return Math.floor(random() * sides) + 1;
}

export function rollDice(
  sides: number,
  count = 1,
  random: RandomSource = Math.random,
): DiceResult {
  if (!Number.isInteger(sides) || sides < 1) {
    throw new Error("Dice must have at least one side");
  }
  if (!Number.isInteger(count) || count < 1) {
    throw new Error("Dice count must be at least one");
  }
  if (count > 100) {
    throw new Error("Dice count must not exceed 100");
  }
  const rolls = Array.from({ length: count }, () => randomInt(sides, random));
  return { rolls, total: rolls.reduce((sum, roll) => sum + roll, 0) };
}

export function skillCheck(
  stats: Record<string, unknown>,
  skill: string,
  difficulty: number,
  advantage: boolean | null = null,
  random: RandomSource = Math.random,
): CheckResult {
  const ability = resolveSkillAbility(skill);
  const modifier = abilityModifier(stats, ability);
  const firstRoll = randomInt(20, random);
  const secondRoll = randomInt(20, random);
  const roll = advantage === true
    ? Math.max(firstRoll, secondRoll)
    : advantage === false
      ? Math.min(firstRoll, secondRoll)
      : firstRoll;
  const total = roll + modifier;
  // Натуральная 20 — успех всегда, натуральная 1 — провал всегда.
  const naturalSuccess = roll === 20;
  const naturalFailure = roll === 1;
  const success = naturalSuccess || (!naturalFailure && total >= difficulty);
  return {
    roll,
    modifier,
    total,
    difficulty,
    success,
    margin: total - difficulty,
    ability,
    naturalSuccess,
    naturalFailure,
  };
}
