export interface DiceResult {
  rolls: number[];
  total: number;
  /**
   * При advantage/disadvantage на d20 здесь оба брошенных значения каждой кости
   * (выбранное первым, отброшенное вторым). Только для sides===20 с advantage!==null.
   */
  pairs?: Array<[number, number]>;
}

/** Одна группа кубиков в нотации (например «2d6» в «2d6+1d8+3»). */
export interface DiceGroup {
  count: number;
  sides: number;
}

/** Распарсенная нотация броска: группы кубиков и плоский модификатор. */
export interface ParsedNotation {
  groups: DiceGroup[];
  modifier: number;
}

/** Результат броска одной группы кубиков. */
export interface DiceGroupResult {
  /** Исходная спецификация группы, например «2d6». */
  spec: string;
  /** Выбранные значения (count штук, после выбора max/min при advantage/disadvantage). */
  rolls: number[];
  /** Оба брошенных значения — только для advantage/disadvantage на d20. */
  pairs?: Array<[number, number]>;
  subtotal: number;
}

/** Полный результат броска по нотации. */
export interface NotationResult {
  /** Исходная нотация, например «2d6+1d8+3». */
  notation: string;
  groups: DiceGroupResult[];
  modifier: number;
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

// --- Настройки «доброго» псевдорандома (только для skill_check). Легко тюнить. ---
/** При обычном броске d20: если выпало ниже среднего, с этим шансом перебрасываем и берём большее. */
export const LUCKY_REROLL_CHANCE = 0.5;
/** Бросок считается «низким», если грань ниже этого значения. */
export const LOW_ROLL_THRESHOLD = 10;
/** Сколько последних бросков d20 учитываем при определении серии неудач. */
export const LOW_STREAK_WINDOW = 2;
/** Серия из подряд низких бросков: следующий принудительно не ниже этого значения. */
export const LOW_STREAK_FLOOR = 11;

function randomInt(sides: number, random: RandomSource): number {
  return Math.floor(random() * sides) + 1;
}

/**
 * «Добрый» источник случайности для d20: слегка сдвигает распределение вверх и
 * ломает серии низких бросков. Применяется ТОЛЬКО к skill_check (по решению
 * дизайна); боевые броски, урон и инициатива остаются на честном Math.random.
 *
 * - Лёгкий сдвиг: при низком броске (грань < LOW_ROLL_THRESHOLD) с шансом
 *   LUCKY_REROLL_CHANCE перебрасываем и берём max (среднее d20 ≈ 12.2).
 * - Анти-серия: если recentLowStreak (последние LOW_STREAK_WINDOW бросков были
 *   ниже LOW_ROLL_THRESHOLD), форсируем грань не ниже LOW_STREAK_FLOOR.
 *
 * Возвращает функцию 0..1, совместимую с randomInt.
 */
export function makeLuckyRandom(base: RandomSource = Math.random, recentLowStreak = false): RandomSource {
  return () => {
    const r = base();
    if (recentLowStreak) {
      // floor = LOW_STREAK_FLOOR на d20 → r_min = (LOW_STREAK_FLOOR - 1) / 20
      const minUnit = (LOW_STREAK_FLOOR - 1) / 20;
      return Math.max(r, minUnit + base() * (1 - minUnit));
    }
    const face = Math.floor(r * 20) + 1;
    if (face < LOW_ROLL_THRESHOLD && base() < LUCKY_REROLL_CHANCE) {
      return Math.max(r, base());
    }
    return r;
  };
}

/**
 * true, если последние LOW_STREAK_WINDOW граней d20 — все ниже LOW_ROLL_THRESHOLD.
 * Используется skill_check для включения «доброго» режима (анти-серия).
 */
export function isLowStreak(history: readonly number[]): boolean {
  const recent = history.slice(-LOW_STREAK_WINDOW);
  return recent.length >= LOW_STREAK_WINDOW && recent.every((v) => v < LOW_ROLL_THRESHOLD);
}

export function rollDice(
  sides: number,
  count = 1,
  random: RandomSource = Math.random,
  advantage: boolean | null = null,
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
  const useAdv = advantage !== null && sides === 20;
  const rolls: number[] = [];
  const pairs: Array<[number, number]> = [];
  for (let i = 0; i < count; i++) {
    if (useAdv) {
      const a = randomInt(sides, random);
      const b = randomInt(sides, random);
      const pick = advantage === true ? Math.max(a, b) : Math.min(a, b);
      rolls.push(pick);
      pairs.push([pick, pick === a ? b : a]);
    } else {
      rolls.push(randomInt(sides, random));
    }
  }
  const result: DiceResult = { rolls, total: rolls.reduce((sum, roll) => sum + roll, 0) };
  if (useAdv) result.pairs = pairs;
  return result;
}

/**
 * Парсит нотацию броска: «4d20», «2d6+1d8+3», «1d20+5», «d8», «2d4-1».
 * Группы вида [count]d<sides> (count по умолчанию 1) и плоский модификатор ±N.
 */
export function parseDiceNotation(spec: string): ParsedNotation {
  const normalized = spec.replace(/\s+/g, "").toLowerCase();
  if (!normalized) {
    throw new Error(`Пустая нотация броска «${spec}». Пример: «2d6+1d8+3».`);
  }
  if (!/^[0-9d+\-]+$/.test(normalized)) {
    throw new Error(`Недопустимые символы в нотации «${spec}». Допускаются цифры, d, +, -.`);
  }
  const groups: DiceGroup[] = [];
  const groupPattern = /(\d*)d(\d+)/g;
  let match: RegExpExecArray | null;
  let lastEnd = 0;
  let consumed = "";
  while ((match = groupPattern.exec(normalized)) !== null) {
    // Проверяем, что между этой группой и предыдущей — только + или - или ничего.
    const between = normalized.slice(lastEnd, match.index);
    if (between && !/^[+-]$/.test(between)) {
      throw new Error(`Неправильная нотация броска «${spec}» (ожидался + или - между группами).`);
    }
    const count = match[1] === "" ? 1 : Number(match[1]);
    const sides = Number(match[2]);
    if (!Number.isInteger(count) || count < 1) {
      throw new Error(`Количество костей должно быть ≥ 1 в «${spec}».`);
    }
    if (!Number.isInteger(sides) || sides < 1) {
      throw new Error(`Количество граней должно быть ≥ 1 в «${spec}».`);
    }
    groups.push({ count, sides });
    lastEnd = groupPattern.lastIndex;
    consumed += match[0];
  }
  // Хвост после последней группы — модификатор: +N, -N или пусто.
  const tail = normalized.slice(lastEnd);
  let modifier = 0;
  if (tail) {
    if (!/^[+-]\d+$/.test(tail)) {
      throw new Error(`Неправильный модификатор «${tail}» в «${spec}». Пример: «+3» или «-1».`);
    }
    modifier = Number(tail);
  }
  if (groups.length === 0) {
    throw new Error(`В нотации «${spec}» нет костей. Пример: «2d6+1d8+3».`);
  }
  if (groups.length > 5) {
    throw new Error(`Слишком много групп костей в «${spec}» (максимум 5).`);
  }
  const totalCount = groups.reduce((sum, g) => sum + g.count, 0);
  if (totalCount > 100) {
    throw new Error(`Слишком много костей в «${spec}»: ${totalCount} (максимум 100).`);
  }
  return { groups, modifier };
}

/**
 * Бросает кубики по нотации. Advantage/disadvantage применяется ТОЛЬКО к d20-группам
 * (бросаем каждую d20 дважды, берём max/min); прочие кости кидаются честно.
 */
export function rollDiceNotation(
  spec: string,
  options: { random?: RandomSource; advantage?: boolean | null } = {},
): NotationResult {
  const random = options.random ?? Math.random;
  const advantage = options.advantage ?? null;
  const parsed = parseDiceNotation(spec);
  const groups: DiceGroupResult[] = parsed.groups.map((g) => {
    const result = rollDice(g.sides, g.count, random, g.sides === 20 ? advantage : null);
    const groupResult: DiceGroupResult = {
      spec: `${g.count}d${g.sides}`,
      rolls: result.rolls,
      subtotal: result.total,
    };
    if (result.pairs) groupResult.pairs = result.pairs;
    return groupResult;
  });
  const groupsTotal = groups.reduce((sum, g) => sum + g.subtotal, 0);
  const total = groupsTotal + parsed.modifier;
  return { notation: spec, groups, modifier: parsed.modifier, total };
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
