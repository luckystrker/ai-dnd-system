/**
 * Персистентное состояние боя: таблица combat_snapshot в базе кампаний
 * (campaigns.db), одна строка на кампанию.
 *
 * gameState (enemies + combat) живёт только в сессии и теряется при рестарте.
 * Этот модуль — точка сохранения/восстановления боя между сессиями: хук
 * combat-autosave пишет сюда после боевых ходов, hydrateGameState читает при
 * старте сессии. Перезаписываемая запись (не append), всегда актуальный снимок.
 */
import type BetterSqlite3 from "better-sqlite3";

import {
  deserializeCombatOrder,
  serializeCombatOrder,
  type CombatOrder,
} from "../engine/combat.ts";
import { openCampaignDb } from "./sqlite-db.ts";

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

const SCHEMA = `
CREATE TABLE IF NOT EXISTS combat_snapshot (
  campaign_id TEXT PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE,
  combat TEXT NOT NULL,          -- JSON serializeCombatOrder
  enemies TEXT NOT NULL,         -- JSON SerializedEnemy[]
  saved_at TEXT NOT NULL
);
`;

// Ленивое открытие БД: handle и DDL создаются при первом обращении, а не
// на импорте модуля (eve-снапшот компиляции падает при открытии
// better-sqlite3 на этапе сборки тулов).
let dbHandle: BetterSqlite3.Database | undefined;
let schemaReady = false;
function db(): BetterSqlite3.Database {
  if (!dbHandle) dbHandle = openCampaignDb();
  if (!schemaReady) {
    dbHandle.exec(SCHEMA);
    schemaReady = true;
  }
  return dbHandle;
}

/** id кампании по slug; undefined, если кампании нет (тихий no-op). */
function campaignIdOf(campaignSlug: string): string | undefined {
  const row = db().prepare("SELECT id FROM campaigns WHERE slug = ?").get(campaignSlug) as
    | { id: string }
    | undefined;
  return row?.id;
}

/** Сохраняет снимок активного боя (перезапись строки кампании). */
export function saveCombatState(campaignSlug: string, snapshot: CombatSnapshot): void {
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  db().prepare(
    "INSERT OR REPLACE INTO combat_snapshot (campaign_id, combat, enemies, saved_at) VALUES (?, ?, ?, ?)",
  ).run(
    id,
    JSON.stringify(serializeCombatOrder(snapshot.combat)),
    JSON.stringify(snapshot.enemies),
    new Date().toISOString(),
  );
}

/**
 * Читает сохранённый снимок боя или null, если записи нет / данные невалидны.
 * Невалидные данные → null (без выброса): безопасный фолбэк на пустой бой.
 */
export function loadCombatState(campaignSlug: string): CombatSnapshot | null {
  const id = campaignIdOf(campaignSlug);
  if (!id) return null;
  const row = db().prepare(
    "SELECT combat, enemies FROM combat_snapshot WHERE campaign_id = ?",
  ).get(id) as { combat: string; enemies: string } | undefined;
  if (!row) return null;
  let combat: CombatOrder | null;
  try {
    combat = deserializeCombatOrder(JSON.parse(row.combat));
  } catch {
    return null;
  }
  if (!combat || !combat.started || combat.order.length === 0) return null;
  let enemies: SerializedEnemy[];
  try {
    const parsed: unknown = JSON.parse(row.enemies);
    enemies = Array.isArray(parsed)
      ? (parsed
          .map((raw): SerializedEnemy | null => {
            if (typeof raw !== "object" || raw === null) return null;
            const v = raw as Record<string, unknown>;
            if (typeof v.id !== "string" || typeof v.name !== "string") return null;
            if (typeof v.hp !== "number" || typeof v.ac !== "number") return null;
            return { id: v.id, name: v.name, hp: v.hp, ac: v.ac };
          })
          .filter((e): e is SerializedEnemy => e !== null))
      : [];
  } catch {
    return null;
  }
  if (enemies.length === 0) return null;
  return { combat, enemies };
}

/** Удаляет сохранение боя (бой завершён или не активен). Не падает, если записи нет. */
export function clearCombatState(campaignSlug: string): void {
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  db().prepare("DELETE FROM combat_snapshot WHERE campaign_id = ?").run(id);
}
