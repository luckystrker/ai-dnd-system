/**
 * Чистая логика порядка ходов в бою (без eve-рантайма, тестируемая).
 *
 * initiative-тул пишет порядок в durable gameState сессии, combat-тул его
 * продвигает. Здесь только роллы, сортировка и продвижение указателя хода.
 */
import type { RandomSource } from "./dnd5e.ts";

export type CombatantSide = "party" | "enemy";

export interface CombatantInput {
  name: string;
  /** Уникальный идентификатор; если не задан — генерируется из имени (slug). */
  id?: string;
  side: CombatantSide;
  bonus?: number;
  /** Враги: текущие хиты (для урона в бою). */
  hp?: number;
  /** Враги: класс брони (для проверки попаданий). */
  ac?: number;
}

export interface CombatantEntry {
  /** Уникальный идентификатор участника боя (стабильный между ходами). */
  id: string;
  name: string;
  side: CombatantSide;
  bonus: number;
  roll: number;
  total: number;
  hp?: number;
  maxHp?: number;
  ac?: number;
  /**
   * Действие Dodge (уклонение) в 5e даёт помеху (disadvantage) атакам по этому
   * участнику до начала его следующего хода; nextCombatant сбрасывает флаг тому,
   * чей ход наступил.
   */
  dodging?: boolean;
}

/** Превращает имя в slug-подобный id: нижний регистр, пробелы → дефисы, только буквы/цифры. */
function slugify(name: string): string {
  const slug = name
    .trim()
    .toLowerCase()
    .replace(/\s+/g, "-")
    .replace(/[^\p{L}\p{N}-]/gu, "");
  return slug || "combatant";
}

export interface CombatOrder {
  started: boolean;
  round: number;
  /** Индекс текущего участника в order; -1, пока бой не начат. */
  current: number;
  /** Текущий участник уже совершил атаку в этом ходу (одна атака за ход). */
  acted: boolean;
  order: CombatantEntry[];
}

export function emptyCombatOrder(): CombatOrder {
  return { started: false, round: 1, current: -1, acted: false, order: [] };
}

/** Бросает инициативу каждому участнику и сортирует по убыванию (ничьи — по имени). */
export function rollInitiative(
  combatants: CombatantInput[],
  random: RandomSource = Math.random,
): CombatantEntry[] {
  // Дедупликация id: если два участника дают одинаковый slug, второй получает -2, -3, ...
  const usedIds = new Set<string>();
  return combatants
    .map((combatant) => {
      const base = combatant.id?.trim() || slugify(combatant.name);
      let id = base;
      if (usedIds.has(id)) {
        let suffix = 2;
        while (usedIds.has(`${base}-${suffix}`)) suffix += 1;
        id = `${base}-${suffix}`;
      }
      usedIds.add(id);
      const roll = 1 + Math.floor(random() * 20);
      return {
        id,
        name: combatant.name,
        side: combatant.side,
        bonus: combatant.bonus ?? 0,
        roll,
        total: roll + (combatant.bonus ?? 0),
        hp: combatant.hp,
        ac: combatant.ac,
      };
    })
    .sort((a, b) => b.total - a.total || a.name.localeCompare(b.name));
}

/** Жив ли участник: враги — по наличию в активном списке, партия — всегда. */
export type AliveCheck = (entry: CombatantEntry) => boolean;

/**
 * Передвигает указатель на следующего живого участника. При замыкании круга
 * (переход на индекс 0 после уже идущего боя) номер раунда увеличивается.
 * Сброс acted разрешает следующему участнику атаковать; dodge сбрасывается
 * в начале его хода (эффект длится до начала следующего хода уклоняющегося).
 * Если живых не осталось — возвращает порядок без изменений.
 */
export function nextCombatant(order: CombatOrder, alive: AliveCheck): CombatOrder {
  const count = order.order.length;
  if (!order.started || count === 0) return order;
  for (let step = 1; step <= count; step += 1) {
    const index = (order.current + step) % count;
    if (alive(order.order[index])) {
      const orderWithResetDodge = order.order.map((entry, i) =>
        i === index ? { ...entry, dodging: false } : entry,
      );
      return {
        ...order,
        order: orderWithResetDodge,
        current: index,
        acted: false,
        round: order.current >= 0 && index === 0 ? order.round + 1 : order.round,
      };
    }
  }
  return order;
}

const DICE_SPEC = /(\d{1,2})d(4|6|8|10|12)/i;

/**
 * Ищет спецификацию урона в тексте предметов инвентаря: "короткий меч (1d6)"
 * → "1d6". Если передан weapon, ищет только в предметах, содержащих это имя;
 * иначе — по всему инвентарю.
 */
export function weaponDamageDice(items: readonly string[] | undefined, weapon?: string): string | undefined {
  const needle = weapon?.trim().toLowerCase();
  const candidates = needle ? (items ?? []).filter((item) => item.toLowerCase().includes(needle)) : (items ?? []);
  for (const item of candidates) {
    const match = DICE_SPEC.exec(item);
    if (match) return `${match[1]}d${match[2]}`;
  }
  return undefined;
}
