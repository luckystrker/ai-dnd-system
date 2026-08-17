/**
 * Состояние мира кампании (C5 — последствия): таблица world_changes в базе
 * кампаний (campaigns.db) с актуальными фактами мира, сгруппированными по
 * категориям (Погибшие, Изменения и т.п.). В отличие от key_events
 * (append-only журнал моментов), эта таблица отражает текущее состояние
 * мира — поэтому записи upsert'ятся по (категория + текст), а не дописываются.
 *
 * Назначение: чтобы бот не «оживлял» убитых NPC и последовательно учитывал
 * прошлые решения. Грузится в авто-блок памяти целиком (компактно, с капом).
 */
import type BetterSqlite3 from "better-sqlite3";

import { openCampaignDb } from "./sqlite-db.ts";
import type { WorldChange } from "./types.ts";

const SCHEMA = `
CREATE TABLE IF NOT EXISTS world_changes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  category TEXT NOT NULL,
  text TEXT NOT NULL,            -- чистый текст без "- " и без "(день N)"
  day INTEGER,                   -- NULL = без пометки дня
  text_norm TEXT NOT NULL,       -- lower(text)
  UNIQUE (campaign_id, category, text_norm)
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

/**
 * Записывает (upsert) изменение мира в категорию. Идемпотентно по тексту:
 * если запись с таким текстом уже есть в категории — обновляет её день,
 * иначе добавляет. Дубликаты (та же категория + тот же текст) не создаются.
 */
export function upsertWorldChange(campaignSlug: string, change: WorldChange): void {
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  const category = change.category.trim() || "Изменения";
  const text = change.text.replace(/\s*\n\s*/g, " ").trim();
  if (!text) return;
  db().prepare(
    `INSERT INTO world_changes (campaign_id, category, text, day, text_norm) VALUES (?, ?, ?, ?, ?)
     ON CONFLICT(campaign_id, category, text_norm) DO UPDATE SET day = excluded.day`,
  ).run(id, category, text, change.day ?? null, text.toLowerCase());
}

/**
 * Считывает состояние мира как карту категория → список отрендеренных строк
 * («- текст» или «- текст (день N)» — прежний формат строк).
 * Категории идут в порядке первого появления, записи — в порядке добавления.
 */
export function readWorldState(campaignSlug: string): Map<string, string[]> {
  const id = campaignIdOf(campaignSlug);
  const categories = new Map<string, string[]>();
  if (!id) return categories;
  const rows = db().prepare(
    "SELECT category, text, day FROM world_changes WHERE campaign_id = ? ORDER BY id",
  ).all(id) as { category: string; text: string; day: number | null }[];
  for (const row of rows) {
    const dayMark = row.day !== null ? ` (день ${row.day})` : "";
    const line = `- ${row.text}${dayMark}`;
    const items = categories.get(row.category) ?? [];
    items.push(line);
    categories.set(row.category, items);
  }
  return categories;
}

/** Человекочитаемый рендер состояния мира (для memory-блока и list_world_state). */
export function renderWorldState(campaignSlug: string): string {
  return renderCategories(readWorldState(campaignSlug));
}

/** Рендерит переданные категории в текст. */
export function renderCategories(categories: Map<string, string[]>): string {
  if (categories.size === 0) return "";
  const parts: string[] = [];
  for (const [category, items] of categories) {
    if (items.length === 0) continue;
    parts.push(`## ${category}\n${items.join("\n")}`);
  }
  return parts.join("\n\n");
}
