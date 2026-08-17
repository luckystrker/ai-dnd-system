/**
 * Журнал кампании: транскрипт по игровым дням, саммари дней, накопительное
 * саммари кампании, ключевые события и журнал экономики.
 *
 * Всё хранится в SQLite-базе кампаний (campaigns.db) в таблицах:
 *   campaign_days      — метаданные дня (day/date/note/summary/headline)
 *   transcript_entries — строки транскрипта дня, по одной строке на запись
 *   key_events         — ключевые события кампании (permanent/regular)
 *   ledger_rows        — журнал экономики (found/spent)
 *   campaign_summary   — саммари завершённых дней (секции «## День N»)
 * Строки хранятся в отрендеренном виде — ровно как раньше читались из
 * MD-файлов (байт-в-байт), поэтому формат выдачи не изменился. Дедуп по
 * eventId (маркер «<!-- evt:... -->» в хвосте строки) — уникальным
 * индексом UNIQUE (campaign_id, event_id) + INSERT OR IGNORE; SQLite
 * допускает много NULL в уникальной колонке — записи без маркера не
 * дедуплицируются.
 */
import { createHash } from "node:crypto";
import type BetterSqlite3 from "better-sqlite3";

import { openCampaignDb } from "./sqlite-db.ts";
import type { LedgerRow, LedgerType } from "./types.ts";

/** Одна запись транскрипта. */
export interface TranscriptEntry {
  kind: "player" | "dm" | "action";
  /** Для kind=player: имя/username автора. */
  author?: string;
  text: string;
  /** meta.id события eve — для дедупликации при ретраях хуков. */
  eventId?: string;
  /** ISO-время события; по умолчанию «сейчас». */
  at?: string;
}

export interface DayRecord {
  day: number;
  date?: string;
  note?: string;
  summary?: string;
  /** Короткая необрезаемая шапка дня (для компактной хроники). */
  headline?: string;
  entries: string[];
}

export interface DayHeadline {
  day: number;
  headline: string;
}

const SCHEMA = `
CREATE TABLE IF NOT EXISTS campaign_days (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  date TEXT,
  note TEXT,
  summary TEXT,
  headline TEXT,
  PRIMARY KEY (campaign_id, day)
);

CREATE TABLE IF NOT EXISTS transcript_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  line TEXT NOT NULL,          -- отрендеренная строка дня
  event_id TEXT,               -- из маркера "<!-- evt:... -->"
  UNIQUE (campaign_id, event_id)
);

CREATE TABLE IF NOT EXISTS key_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  line TEXT NOT NULL,          -- "- [важно] **День N**: ..." / "- **День N**: ..."
  permanent INTEGER NOT NULL DEFAULT 0,
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
);

CREATE TABLE IF NOT EXISTS ledger_rows (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  type TEXT NOT NULL,          -- "found" | "spent"
  line TEXT NOT NULL,          -- "- [День N] found: ..."
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
);

CREATE TABLE IF NOT EXISTS campaign_summary (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  text TEXT NOT NULL,          -- содержимое секции "## День N" (без заголовка)
  PRIMARY KEY (campaign_id, day)
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
    migrateKeyEventDedup(dbHandle);
    schemaReady = true;
  }
  return dbHandle;
}

/**
 * Детерминированный id ключевого события по содержимому: используется, когда
 * вызывающий не передал явный eventId. Позволяет уникальному индексу
 * (campaign_id, event_id) дедуплицировать повторы одного и того же события
 * (SQLite считает NULL'ы различными, поэтому без id индекс не работает).
 */
function contentEventId(campaignId: string, day: number, clean: string, permanent: boolean): string {
  return createHash("sha256")
    .update(`${campaignId}\u0000${day}\u0000${permanent ? "p" : "r"}\u0000${clean}`)
    .digest("hex")
    .slice(0, 40);
}

/** Бэкфилл event_id для старых строк key_events (с NULL): контентный хеш, чтобы
 *  уникальный индекс реально защищал. Точные дубли содержимого удаляются. */
function migrateKeyEventDedup(handle: BetterSqlite3.Database): void {
  const rows = handle.prepare(
    "SELECT id, campaign_id, day, line, permanent FROM key_events WHERE event_id IS NULL ORDER BY id",
  ).all() as
    | { id: number; campaign_id: string; day: number; line: string; permanent: number }[];
  if (rows.length === 0) return;
  const seen = new Set<string>();
  const deleteIds: number[] = [];
  const updates: Array<[string, number]> = [];
  for (const row of rows) {
    const clean = row.line
      .replace(/^-\s*(\[важно\]\s*)?\*\*День\s+\d+\*\*:\s*/, "")
      .replace(/\s*\n\s*/g, " ")
      .trim();
    if (!clean) continue;
    const key = `evt:${contentEventId(row.campaign_id, row.day, clean, row.permanent === 1)}`;
    if (seen.has(key)) {
      deleteIds.push(row.id);
    } else {
      seen.add(key);
      updates.push([key, row.id]);
    }
  }
  const del = handle.prepare("DELETE FROM key_events WHERE id = ?");
  for (const id of deleteIds) del.run(id);
  const upd = handle.prepare("UPDATE key_events SET event_id = ? WHERE id = ?");
  for (const [key, id] of updates) upd.run(key, id);
}

/** id кампании по slug; undefined, если кампании нет (тихий no-op). */
function campaignIdOf(campaignSlug: string): string | undefined {
  const row = db().prepare("SELECT id FROM campaigns WHERE slug = ?").get(campaignSlug) as
    | { id: string }
    | undefined;
  return row?.id;
}

/** Создаёт метаданные дня, если их ещё нет. */
export function ensureDay(campaignSlug: string, day: number, meta?: { note?: string; date?: string }): void {
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  db().prepare(
    "INSERT OR IGNORE INTO campaign_days (campaign_id, day, date, note) VALUES (?, ?, ?, ?)",
  ).run(id, day, meta?.date ?? new Date().toISOString().slice(0, 10), meta?.note ?? null);
}

/** Номера всех сохранённых дней по возрастанию. */
export function listDays(campaignSlug: string): number[] {
  const id = campaignIdOf(campaignSlug);
  if (!id) return [];
  const rows = db().prepare(
    "SELECT day FROM campaign_days WHERE campaign_id = ? ORDER BY day",
  ).all(id) as { day: number }[];
  return rows.map((row) => row.day);
}

/** Добавляет запись в транскрипт дня (append-only, дедуп по eventId). */
export function appendTranscriptEntry(campaignSlug: string, day: number, entry: TranscriptEntry): void {
  ensureDay(campaignSlug, day);
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  const marker = entry.eventId ? `evt:${entry.eventId}` : undefined;

  const time = (entry.at ?? new Date().toISOString()).slice(11, 16);
  const text = entry.text.replace(/\s*\n\s*/g, " ").trim();
  if (!text) return;
  let line: string;
  if (entry.kind === "player") {
    line = `- [${time}] **Игрок @${entry.author ?? "?"}**: ${text}`;
  } else if (entry.kind === "dm") {
    line = `- [${time}] **DM**: ${text}`;
  } else {
    line = `- [${time}] *${text}*`;
  }
  if (marker) line += ` <!-- ${marker} -->`;
  db().prepare(
    "INSERT OR IGNORE INTO transcript_entries (campaign_id, day, line, event_id) VALUES (?, ?, ?, ?)",
  ).run(id, day, line, marker ?? null);
}

/** Читает день целиком; undefined, если дня нет. */
export function readDay(campaignSlug: string, day: number): DayRecord | undefined {
  return readDayTail(campaignSlug, day, Number.POSITIVE_INFINITY);
}

/** Читает последние maxLines записей дня; undefined, если дня нет. */
export function readDayTail(campaignSlug: string, day: number, maxLines: number): DayRecord | undefined {
  const id = campaignIdOf(campaignSlug);
  if (!id) return undefined;
  const meta = db().prepare(
    "SELECT day, date, note, summary, headline FROM campaign_days WHERE campaign_id = ? AND day = ?",
  ).get(id, day) as
    | { day: number; date: string | null; note: string | null; summary: string | null; headline: string | null }
    | undefined;
  if (!meta) return undefined;
  const rows = db().prepare(
    "SELECT line FROM transcript_entries WHERE campaign_id = ? AND day = ? ORDER BY id",
  ).all(id, day) as { line: string }[];
  const entries = rows.slice(-Math.max(0, Math.floor(maxLines))).map((row) => row.line);
  return {
    day: meta.day,
    date: meta.date ?? undefined,
    note: meta.note ?? undefined,
    summary: meta.summary ?? undefined,
    headline: meta.headline ?? undefined,
    entries,
  };
}

/** Записывает саммари дня (строка сохраняется в метаданных дня). */
export function setDaySummary(campaignSlug: string, day: number, summary: string): void {
  ensureDay(campaignSlug, day);
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  db().prepare("UPDATE campaign_days SET summary = ? WHERE campaign_id = ? AND day = ?")
    .run(summary.trim(), id, day);
}

/**
 * Короткая «шапка» дня (≤140 симв.) — необрезаемая одна строка на каждый день,
 * чтобы в авто-блоке памяти была видна вся хроника кампании целиком, а не только
 * хвост последних ~2000 символов. Пишется chronicler'ом вместе с саммари.
 */
export function setDayHeadline(campaignSlug: string, day: number, headline: string): void {
  ensureDay(campaignSlug, day);
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  db().prepare("UPDATE campaign_days SET headline = ? WHERE campaign_id = ? AND day = ?")
    .run(headline.trim().slice(0, 140), id, day);
}

/**
 * Шапки всех дней по возрастанию (для компактной хроники в авто-блоке памяти).
 * Дни без headline пропускаются.
 */
export function listDayHeadlines(campaignSlug: string): DayHeadline[] {
  const id = campaignIdOf(campaignSlug);
  if (!id) return [];
  const rows = db().prepare(
    `SELECT day, headline FROM campaign_days
     WHERE campaign_id = ? AND headline IS NOT NULL AND headline != '' ORDER BY day`,
  ).all(id) as { day: number; headline: string }[];
  return rows.map((row) => ({ day: row.day, headline: row.headline }));
}

/** Обновляет (или добавляет) секцию дня в накопительном саммари кампании. */
export function upsertCampaignSummaryDay(campaignSlug: string, day: number, text: string): void {
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  db().prepare(
    `INSERT INTO campaign_summary (campaign_id, day, text) VALUES (?, ?, ?)
     ON CONFLICT(campaign_id, day) DO UPDATE SET text = excluded.text`,
  ).run(id, day, text.trim());
}

export function readCampaignSummary(campaignSlug: string): string {
  const id = campaignIdOf(campaignSlug);
  if (!id) return "";
  const rows = db().prepare(
    "SELECT day, text FROM campaign_summary WHERE campaign_id = ? ORDER BY day",
  ).all(id) as { day: number; text: string }[];
  if (rows.length === 0) return "";
  return rows.map((row) => `## День ${row.day}\n\n${row.text}`).join("\n\n").trim();
}

/**
 * Добавляет ключевое событие кампании.
 * permanent-события помечаются маркером [важно] — они показываются в авто-блоке
 * памяти полностью (без обрезки), в отличие от обычных, которые обрезаются по
 * хвосту. permanent — для поворотных фактов (смерти, союзы, раскрытые секреты),
 * которые DM обязан помнить всю кампанию.
 */
export function appendKeyEvent(campaignSlug: string, day: number, text: string, eventId?: string, permanent = false): void {
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  const clean = text.replace(/\s*\n\s*/g, " ").trim();
  if (!clean) return;
  const prefix = permanent ? "[важно] " : "";
  let line = `- ${prefix}**День ${day}**: ${clean}`;
  // Маркер в тексте строки — только для явного eventId (идемпотентность ретраев).
  // Для неявного id используем контентный хеш в колонке event_id: строку не засоряем.
  const dedupId = eventId ?? contentEventId(id, day, clean, permanent);
  if (eventId) line += ` <!-- evt:${eventId} -->`;
  db().prepare(
    "INSERT OR IGNORE INTO key_events (campaign_id, day, line, permanent, event_id) VALUES (?, ?, ?, ?, ?)",
  ).run(id, day, line, permanent ? 1 : 0, `evt:${dedupId}`);
}

/** Разделяет ключевые события на permanent (без обрезки) и обычные (хвост). */
export function splitKeyEvents(campaignSlug: string): { permanent: string[]; regular: string[] } {
  const id = campaignIdOf(campaignSlug);
  if (!id) return { permanent: [], regular: [] };
  const rows = db().prepare(
    "SELECT line, permanent FROM key_events WHERE campaign_id = ? ORDER BY id",
  ).all(id) as { line: string; permanent: number }[];
  const permanent: string[] = [];
  const regular: string[] = [];
  for (const row of rows) {
    if (!row.line.startsWith("- ")) continue;
    if (row.permanent === 1) permanent.push(row.line);
    else regular.push(row.line);
  }
  return { permanent, regular };
}

export function readKeyEvents(campaignSlug: string): string {
  const id = campaignIdOf(campaignSlug);
  if (!id) return "";
  const rows = db().prepare(
    "SELECT line FROM key_events WHERE campaign_id = ? ORDER BY id",
  ).all(id) as { line: string }[];
  return rows.map((row) => row.line).join("\n").trim();
}

/** Маркер авто-дайджеста в поле summary: отличает страховочное саммари от chronicler'а. */
export const AUTO_DIGEST_MARK = "*(авто-дайджест)*";

/**
 * Детерминированный (без LLM) дайджест из записей транскрипта дня. Страховка:
 * если chronicler не отработал, в саммари дня будет хоть что-то, чтобы факт не
 * потерялся после compaction. Chronicler перезапишет это поле качественным
 * саммари (а наличие маркера AUTO_DIGEST_MARK не даёт хуку затирать готовое).
 * Чистая функция — тестируемая без eve-рантайма.
 */
export function buildDayDigest(entries: readonly string[], maxSentences = 5): string {
  if (entries.length === 0) return "";
  // Берём последние записи (свежее важнее), чистим разметку и метки времени.
  const tail = entries.slice(-maxSentences * 2);
  const cleaned = tail
    .map((line) =>
      line
        .replace(/^-\s*\[\d{2}:\d{2}\]\s*/, "") // убрать "- [HH:MM] "
        .replace(/<!--\s*evt:[^>]*-->\s*$/, "") // убрать маркер дедупа
        .trim(),
    )
    .filter((text) => text.length > 0);
  if (cleaned.length === 0) return "";
  return `${AUTO_DIGEST_MARK} ${cleaned.join(" ")}`;
}

// --- C3. Журнал лута / экономики ---

const LEDGER_VERB: Record<LedgerType, string> = {
  found: "найдено",
  spent: "потрачено",
};

/**
 * Добавляет запись в журнал экономики (append-only, дедуп по eventId).
 * Записи пишутся детерминированно из тулов grant_character / complete_quest —
 * без отдельного LLM-вызова.
 */
export function appendLedgerRow(campaignSlug: string, row: LedgerRow, eventId?: string): void {
  const id = campaignIdOf(campaignSlug);
  if (!id) return;
  const marker = eventId ? `evt:${eventId}` : undefined;
  const item = row.itemOrGold.replace(/\s*\n\s*/g, " ").trim();
  if (!item) return;
  const verb = LEDGER_VERB[row.type] ?? row.type;
  const by = row.by ? ` (${row.by})` : "";
  const note = row.note ? ` — ${row.note.replace(/\s*\n\s*/g, " ").trim()}` : "";
  let line = `- [День ${row.day}] ${verb}: ${item}${by}${note}`;
  if (marker) line += ` <!-- ${marker} -->`;
  db().prepare(
    "INSERT OR IGNORE INTO ledger_rows (campaign_id, day, type, line, event_id) VALUES (?, ?, ?, ?, ?)",
  ).run(id, row.day, row.type, line, marker ?? null);
}

/** Читает журнал экономики (сырой текст). */
export function readLedgerRaw(campaignSlug: string): string {
  const id = campaignIdOf(campaignSlug);
  if (!id) return "";
  const rows = db().prepare(
    "SELECT line FROM ledger_rows WHERE campaign_id = ? ORDER BY id",
  ).all(id) as { line: string }[];
  return rows.map((row) => row.line).join("\n").trim();
}

/** Считывает журнал экономики, опционально с фильтром по дню/типу; последние maxLines записей. */
export function readLedger(
  campaignSlug: string,
  filter?: { day?: number; type?: LedgerType },
  maxLines = Number.POSITIVE_INFINITY,
): string[] {
  const id = campaignIdOf(campaignSlug);
  if (!id) return [];
  let sql = "SELECT line FROM ledger_rows WHERE campaign_id = ?";
  const params: unknown[] = [id];
  if (filter?.day !== undefined) {
    sql += " AND day = ?";
    params.push(filter.day);
  }
  if (filter?.type !== undefined) {
    sql += " AND type = ?";
    params.push(filter.type);
  }
  sql += " ORDER BY id";
  const rows = db().prepare(sql).all(...params) as { line: string }[];
  return rows.slice(-Math.max(0, Math.floor(maxLines))).map((row) => row.line);
}


