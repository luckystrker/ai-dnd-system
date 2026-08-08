/**
 * Персистентное состояние боя в папке кампании (combat.md).
 *
 * gameState (enemies + combat) живёт только в сессии и теряется при рестарте.
 * Этот модуль — точка сохранения/восстановления боя между сессиями: хук
 * combat-autosave пишет сюда после боевых ходов, hydrateGameState читает при
 * старте сессии. Перезаписываемый файл (не append), всегда актуальный снимок.
 */
import { existsSync, mkdirSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";

import {
  deserializeCombatOrder,
  serializeCombatOrder,
  type CombatOrder,
} from "../engine/combat.ts";
import { buildDocument, splitFrontmatter } from "./frontmatter.ts";
import { assertCampaignSlug, campaignDataRoot } from "./store.ts";

/**
 * Снимок активного врага. Совместим по структуре с Enemy из memory.ts, но
 * определён здесь, чтобы модуль хранения не тянул зависимость от eve-рантайма
 * (memory.ts импортирует defineState из eve/context).
 */
export interface SerializedEnemy {
  id: string;
  name: string;
  hp: number;
  ac: number;
}

export interface CombatSnapshot {
  combat: CombatOrder;
  enemies: SerializedEnemy[];
}

function combatPath(campaignSlug: string): string {
  assertCampaignSlug(campaignSlug);
  return join(campaignDataRoot(), campaignSlug, "combat.md");
}

/** Сохраняет снимок активного боя в combat.md (перезапись). */
export function saveCombatState(campaignSlug: string, snapshot: CombatSnapshot): void {
  const dir = join(campaignDataRoot(), campaignSlug);
  mkdirSync(dir, { recursive: true });
  const data: Record<string, unknown> = {
    combat: serializeCombatOrder(snapshot.combat),
    enemies: snapshot.enemies,
    savedAt: new Date().toISOString(),
  };
  writeFileSync(combatPath(campaignSlug), buildDocument(data, ""), "utf8");
}

/**
 * Читает сохранённый снимок боя или null, если файла нет / данные невалидны.
 * Невалидные данные → null (без выброса): безопасный фолбэк на пустой бой.
 */
export function loadCombatState(campaignSlug: string): CombatSnapshot | null {
  const path = combatPath(campaignSlug);
  if (!existsSync(path)) return null;
  let data: Record<string, unknown>;
  try {
    data = splitFrontmatter(readFileSync(path, "utf8")).data;
  } catch {
    return null;
  }
  const combat = deserializeCombatOrder(data.combat);
  if (!combat || !combat.started || combat.order.length === 0) return null;
  const enemies = Array.isArray(data.enemies)
    ? (data.enemies
        .map((raw): SerializedEnemy | null => {
          if (typeof raw !== "object" || raw === null) return null;
          const v = raw as Record<string, unknown>;
          if (typeof v.id !== "string" || typeof v.name !== "string") return null;
          if (typeof v.hp !== "number" || typeof v.ac !== "number") return null;
          return { id: v.id, name: v.name, hp: v.hp, ac: v.ac };
        })
        .filter((e): e is SerializedEnemy => e !== null))
    : [];
  if (enemies.length === 0) return null;
  return { combat, enemies };
}

/** Удаляет сохранение боя (бой завершён или не активен). Не падает, если файла нет. */
export function clearCombatState(campaignSlug: string): void {
  const path = combatPath(campaignSlug);
  if (existsSync(path)) rmSync(path, { force: true });
}
