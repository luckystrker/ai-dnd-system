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
  side: CombatantSide;
  bonus?: number;
  /** Враги: текущие хиты (для урона в бою). */
  hp?: number;
  /** Враги: класс брони (для проверки попаданий). */
  ac?: number;
}

export interface CombatantEntry {
  name: string;
  side: CombatantSide;
  bonus: number;
  roll: number;
  total: number;
  hp?: number;
  ac?: number;
}

export interface CombatOrder {
  started: boolean;
  round: number;
  /** Индекс текущего участника в order; -1, пока бой не начат. */
  current: number;
  order: CombatantEntry[];
}

export function emptyCombatOrder(): CombatOrder {
  return { started: false, round: 1, current: -1, order: [] };
}

/** Бросает инициативу каждому участнику и сортирует по убыванию (ничьи — по имени). */
export function rollInitiative(
  combatants: CombatantInput[],
  random: RandomSource = Math.random,
): CombatantEntry[] {
  return combatants
    .map((combatant) => {
      const roll = 1 + Math.floor(random() * 20);
      return {
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
 * Если живых не осталось — возвращает порядок без изменений.
 */
export function nextCombatant(order: CombatOrder, alive: AliveCheck): CombatOrder {
  const count = order.order.length;
  if (!order.started || count === 0) return order;
  for (let step = 1; step <= count; step += 1) {
    const index = (order.current + step) % count;
    if (alive(order.order[index])) {
      return {
        ...order,
        current: index,
        round: order.current >= 0 && index === 0 ? order.round + 1 : order.round,
      };
    }
  }
  return order;
}
