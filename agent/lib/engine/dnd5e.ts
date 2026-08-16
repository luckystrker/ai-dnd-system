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
  /** Оставить N наибольших (kh) значений; прочие отбрасываются. */
  keepHigh?: number;
  /** Оставить N наименьших (kl) значений; прочие отбрасываются. */
  keepLow?: number;
  /** Отбросить N наибольших (dh) значений. */
  dropHigh?: number;
  /** Отбросить N наименьших (dl) значений — «4d6dl1» = бросок характеристик. */
  dropLow?: number;
  /** Взрывные кубики: кость на максимальной грани добавляет ещё одну (рекурсивно, с лимитом). */
  explode?: boolean;
}

/** Распарсенная нотация броска: группы кубиков и плоский модификатор. */
export interface ParsedNotation {
  groups: DiceGroup[];
  modifier: number;
}

/** Результат броска одной группы кубиков. */
export interface DiceGroupResult {
  /** Исходная спецификация группы, например «2d6» или «4d6kh3». */
  spec: string;
  /** Учитываемые значения (после keep/drop; при advantage/disadvantage — выбранные). */
  rolls: number[];
  /** Отброшенные значения (keep/drop, кроме advantage-пар) — показываются игроку. */
  dropped?: number[];
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
 * history может прийти undefined/null из старого durable-состояния сессии —
 * тогда это не серия (безопасный дефолт вместо undefined.slice).
 */
export function isLowStreak(history: readonly number[] | null | undefined): boolean {
  const recent = (history ?? []).slice(-LOW_STREAK_WINDOW);
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

/** Лимит рекурсивных взрывных костей на одну группу (защита от бесконечности). */
const EXPLODE_LIMIT = 10;

/** Строковое представление группы с модификаторами, напр. «4d6kh3» или «2d6!». */
function groupSpec(g: DiceGroup): string {
  let spec = `${g.count}d${g.sides}`;
  if (g.keepHigh) spec += `kh${g.keepHigh}`;
  if (g.keepLow) spec += `kl${g.keepLow}`;
  if (g.dropHigh) spec += `dh${g.dropHigh}`;
  if (g.dropLow) spec += `dl${g.dropLow}`;
  if (g.explode) spec += "!";
  return spec;
}

/**
 * Парсит нотацию броска: «4d20», «2d6+1d8+3», «1d20+5», «d8», «2d4-1»,
 * «4d6kh3» (оставить 3 наибольших), «4d6dl1» (отбросить 1 наименьший),
 * «8d6!» (взрывные кубики — кость на максимальной грани добавляет ещё одну).
 * Группы вида [count]d<sides> (count по умолчанию 1) с опциональными модификаторами
 * и плоским модификатором ±N.
 */
export function parseDiceNotation(spec: string): ParsedNotation {
  const normalized = spec.replace(/\s+/g, "").toLowerCase();
  if (!normalized) {
    throw new Error(`Пустая нотация броска «${spec}». Пример: «2d6+1d8+3».`);
  }
  // Допустимые символы: цифры, d, модификаторы keep/drop (k,h,l,d), explode (!), операторы.
  if (!/^[0-9dkhl!+\-]+$/.test(normalized)) {
    throw new Error(`Недопустимые символы в нотации «${spec}». Допускаются цифры, d, +, -, kh/kl/dh/dl, !.`);
  }

  // Разбиваем на термы по +/-. Каждый терм — группа костей (с модификаторами) или модификатор.
  const terms = normalized.match(/[+-]?[^+-]+/g);
  if (!terms || terms.join("") !== normalized) {
    throw new Error(`Неправильная нотация броска «${spec}». Пример: «2d6+1d8+3».`);
  }

  const groups: DiceGroup[] = [];
  let modifier = 0;
  const groupRe = /^(\d*)d(\d+)((?:kh\d+|kl\d+|dh\d+|dl\d+|!)*)$/;
  const modRe = /(kh\d+|kl\d+|dh\d+|dl\d+|!)/g;
  for (const term of terms) {
    const sign = term.startsWith("-") ? -1 : 1;
    const body = term.replace(/^[+-]/, "");
    const groupMatch = groupRe.exec(body);
    if (groupMatch) {
      if (sign === -1) {
        throw new Error(`Группа костей не может быть отрицательной в «${spec}».`);
      }
      const count = groupMatch[1] === "" ? 1 : Number(groupMatch[1]);
      const sides = Number(groupMatch[2]);
      if (!Number.isInteger(count) || count < 1) {
        throw new Error(`Количество костей должно быть ≥ 1 в «${spec}».`);
      }
      if (!Number.isInteger(sides) || sides < 1) {
        throw new Error(`Количество граней должно быть ≥ 1 в «${spec}».`);
      }
      const group: DiceGroup = { count, sides };
      const modsStr = groupMatch[3];
      if (modsStr) {
        let m: RegExpExecArray | null;
        modRe.lastIndex = 0;
        while ((m = modRe.exec(modsStr)) !== null) {
          const token = m[0];
          if (token === "!") {
            group.explode = true;
            continue;
          }
          const kind = token.slice(0, 2);
          const n = Number(token.slice(2));
          if (kind === "kh") group.keepHigh = n;
          else if (kind === "kl") group.keepLow = n;
          else if (kind === "dh") group.dropHigh = n;
          else group.dropLow = n;
        }
        // keep и drop взаимно исключают друг друга; нельзя указать два keep или два drop.
        const keeps = [group.keepHigh, group.keepLow].filter((v) => v !== undefined).length;
        const drops = [group.dropHigh, group.dropLow].filter((v) => v !== undefined).length;
        if (group.keepHigh !== undefined && group.keepLow !== undefined) {
          throw new Error(`В «${spec}» нельзя одновременно kh и kl.`);
        }
        if (keeps > 0 && drops > 0) {
          throw new Error(`В «${spec}» нельзя комбинировать keep (kh/kl) и drop (dh/dl).`);
        }
        if (group.keepHigh !== undefined && (group.keepHigh < 1 || group.keepHigh >= count)) {
          throw new Error(`В «${spec}» kh должен быть от 1 до ${count - 1}.`);
        }
        if (group.keepLow !== undefined && (group.keepLow < 1 || group.keepLow >= count)) {
          throw new Error(`В «${spec}» kl должен быть от 1 до ${count - 1}.`);
        }
        if (group.dropHigh !== undefined && (group.dropHigh < 1 || group.dropHigh >= count)) {
          throw new Error(`В «${spec}» dh должен быть от 1 до ${count - 1}.`);
        }
        if (group.dropLow !== undefined && (group.dropLow < 1 || group.dropLow >= count)) {
          throw new Error(`В «${spec}» dl должен быть от 1 до ${count - 1}.`);
        }
      }
      groups.push(group);
    } else if (/^\d+$/.test(body)) {
      modifier += sign * Number(body);
    } else {
      throw new Error(`Неправильная нотация броска «${spec}» (терм «${body}» не распознан).`);
    }
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
 * Применяет keep/drop к списку значений: возвращает { kept, dropped }.
 * keep оставляет N наибольших/наименьших, drop убирает N наибольших/наименьших.
 */
function applyKeepDrop(values: number[], group: DiceGroup): { kept: number[]; dropped: number[] } {
  if (
    group.keepHigh === undefined &&
    group.keepLow === undefined &&
    group.dropHigh === undefined &&
    group.dropLow === undefined
  ) {
    return { kept: values, dropped: [] };
  }
  // Индексы по убыванию значения — единый способ выбрать и keep, и drop.
  const order = values.map((v, i) => ({ v, i })).sort((a, b) => b.v - a.v || a.i - b.i);
  const keepCount =
    group.keepHigh ?? (group.keepLow !== undefined ? group.keepLow : undefined);
  const dropCount =
    group.dropHigh ?? (group.dropLow !== undefined ? group.dropLow : undefined);

  let keptIdx: Set<number>;
  if (keepCount !== undefined) {
    // keep берёт N «лучших» (наибольших для kh, наименьших для kl).
    const ranked = group.keepLow !== undefined ? [...order].reverse() : order;
    keptIdx = new Set(ranked.slice(0, keepCount).map((e) => e.i));
  } else if (dropCount !== undefined) {
    // drop убирает N (наибольших для dh, наименьших для dl), остальное остаётся.
    const toRemove =
      group.dropHigh !== undefined ? order.slice(0, dropCount) : [...order].reverse().slice(0, dropCount);
    const removeIdx = new Set(toRemove.map((e) => e.i));
    keptIdx = new Set(values.map((_, i) => i).filter((i) => !removeIdx.has(i)));
  } else {
    keptIdx = new Set(values.map((_, i) => i));
  }

  const kept: number[] = [];
  const dropped: number[] = [];
  values.forEach((v, i) => (keptIdx.has(i) ? kept.push(v) : dropped.push(v)));
  return { kept, dropped };
}

/** Бросает группу с учётом взрывных костей: грань == sides добавляет ещё одну (рекурсивно). */
function rollGroupWithExplode(sides: number, count: number, explode: boolean, random: RandomSource): number[] {
  const out: number[] = [];
  for (let i = 0; i < count; i++) {
    let face = randomInt(sides, random);
    out.push(face);
    if (!explode) continue;
    let guard = 0;
    while (face === sides && guard < EXPLODE_LIMIT) {
      face = randomInt(sides, random);
      out.push(face);
      guard += 1;
    }
  }
  return out;
}

/**
 * Бросает кубики по нотации. Advantage/disadvantage применяется ТОЛЬКО к d20-группам
 * (бросаем каждую d20 дважды, берём max/min); прочие кости кидаются честно.
 * Модификаторы группы keep/drop (kh/kl/dh/dl) и взрывные кости (!) поддерживаются
 * только в нотации; advantage взаимоисключается с keep/drop на d20.
 */
export function rollDiceNotation(
  spec: string,
  options: { random?: RandomSource; advantage?: boolean | null } = {},
): NotationResult {
  const random = options.random ?? Math.random;
  const advantage = options.advantage ?? null;
  const parsed = parseDiceNotation(spec);
  const groups: DiceGroupResult[] = parsed.groups.map((g) => {
    const hasKeepDrop =
      g.keepHigh !== undefined || g.keepLow !== undefined || g.dropHigh !== undefined || g.dropLow !== undefined;
    const useAdv = advantage !== null && g.sides === 20 && !hasKeepDrop;
    if (useAdv) {
      // d20 с advantage/disadvantage: кидаем дважды, выбираем — как раньше.
      const result = rollDice(g.sides, g.count, random, advantage);
      const groupResult: DiceGroupResult = {
        spec: groupSpec(g),
        rolls: result.rolls,
        subtotal: result.total,
      };
      if (result.pairs) groupResult.pairs = result.pairs;
      return groupResult;
    }
    const values = rollGroupWithExplode(g.sides, g.count, g.explode === true, random);
    const { kept, dropped } = applyKeepDrop(values, g);
    const groupResult: DiceGroupResult = {
      spec: groupSpec(g),
      rolls: kept,
      subtotal: kept.reduce((sum, v) => sum + v, 0),
    };
    if (dropped.length > 0) groupResult.dropped = dropped;
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

// --- Окружение (C2): механические влияния времени суток и погоды ---

/** Контекст окружения: фрагмент Campaign, влияющий на броски. */
export interface Environment {
  timeOfDay?: string;
  weather?: string;
}

/** Модификаторы окружения для конкретного броска. */
export interface EnvironmentModifiers {
  /** Результирующее преимущество/помеха от окружения (null — нет влияния). */
  advantage: boolean | null;
  /** Человекочитаемые причины (для показа игроку). */
  reasons: string[];
}

/** Навыки, опирающиеся на зрение: ночь и плохая видимость дают по ним помеху. */
const VISUAL_SKILLS = new Set(["perception", "investigation", "survival"]);

/** Нормализует произвольное название навыка в английский ключ (perception, stealth ...). */
function normalizeSkillKey(skill: string): string {
  const name = skill.trim().toLowerCase();
  const paren = /\(([^)]+)\)/.exec(name);
  if (paren) {
    const inner = paren[1].trim().replace(/[\s-]+/g, "_");
    if (SKILL_ABILITY_MAP[inner]) return inner;
  }
  const underscored = name.replace(/[\s-]+/g, "_");
  if (SKILL_ABILITY_MAP[underscored]) return underscored;
  return SKILL_RU_ALIASES[name] ?? SKILL_RU_ALIASES[underscored] ?? underscored;
}

/** Проверяет, содержит ли строка погоды любой из шаблонов (нижний регистр). */
function weatherMatches(weather: string, patterns: string[]): boolean {
  const w = weather.toLowerCase();
  return patterns.some((pattern) => w.includes(pattern));
}

/** Плохая видимость: туман, дым, метель — мешают зрению. */
const REDUCED_VISIBILITY = [
  "туман", "мгла", "дым", "пелена", "метель", "fog", "mist", "haze", "smoke", "blizzard",
];
/** Сильный ветер/шторм: мешают дальнему бою. */
const STRONG_WIND = [
  "ветер", "ветр", "шторм", "буря", "ураган", "storm", "wind", "gale", "hurricane",
];

/**
 * Правило 5e: если есть хотя бы один источник преимущества и хотя бы один помехи —
 * они взаимно отменяются (обычный бросок). Иначе берётся единственное влияние.
 * undefined/null слои игнорируются.
 */
export function combineAdvantage(...layers: (boolean | null | undefined)[]): boolean | null {
  const clean = layers.filter((value): value is boolean => typeof value === "boolean");
  if (clean.length === 0) return null;
  const hasAdvantage = clean.some((value) => value === true);
  const hasDisadvantage = clean.some((value) => value === false);
  if (hasAdvantage && hasDisadvantage) return null;
  return hasAdvantage ? true : hasDisadvantage ? false : null;
}

/**
 * Влияние окружения на проверку навыка. Ночью — помеха на зрительные проверки
 * (Perception/Investigation/Survival) и преимущество на скрытность. Плохая
 * видимость (туман и т.п.) — помеха на зрительные проверки.
 */
export function environmentModifiersForCheck(env: Environment, skill: string): EnvironmentModifiers {
  const result: EnvironmentModifiers = { advantage: null, reasons: [] };
  const key = normalizeSkillKey(skill);
  const isVisual = VISUAL_SKILLS.has(key);

  if (env.timeOfDay === "night") {
    if (isVisual) {
      result.advantage = combineAdvantage(result.advantage, false);
      result.reasons.push("ночь: помеха на зрительную проверку");
    }
    if (key === "stealth") {
      result.advantage = combineAdvantage(result.advantage, true);
      result.reasons.push("ночь: преимущество на скрытность");
    }
  }

  if (isVisual && env.weather && weatherMatches(env.weather, REDUCED_VISIBILITY)) {
    result.advantage = combineAdvantage(result.advantage, false);
    result.reasons.push(`${env.weather}: плохая видимость, помеха на зрительную проверку`);
  }

  return result;
}

/**
 * Влияние окружения на атаку. Сильный ветер/шторм — помеха на дальние атаки.
 */
export function environmentModifiersForAttack(env: Environment, attackType: "melee" | "ranged"): EnvironmentModifiers {
  const result: EnvironmentModifiers = { advantage: null, reasons: [] };
  if (attackType === "ranged" && env.weather && weatherMatches(env.weather, STRONG_WIND)) {
    result.advantage = false;
    result.reasons.push(`${env.weather}: сильный ветер, помеха на дальнюю атаку`);
  }
  return result;
}
