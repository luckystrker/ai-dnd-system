/**
 * Поиск по долговременной памяти кампании.
 *
 * Два пути:
 * 1. FTS5-индекс (fts_journal) для «журнальных» таблиц — транскрипт, ключевые
 *    события, журнал лута: префиксное совпадение термов (морфология:
 *    «прокляти» находит «проклятие»), SQL-сортировка по свежести, не сканирует
 *    все строки. Индекс инкрементальный (catch-up по id), поддерживается
 *    AFTER DELETE-триггерами. FTS — ускоритель: при ошибке или пустом
 *    результате источник доискивается JS-сканом, поэтому регрессий против
 *    старого подстрочного поиска нет.
 * 2. Подстрочный JS-скан для остальных (небольших, с UPDATE'ами) источников —
 *    NPC, локации, фракции, персонажи, квесты, состояние мира, кампания.
 *
 * Полный аналог «в какой день упоминалось про X» — без внешних сервисов.
 */
import type BetterSqlite3 from "better-sqlite3";

import { openCampaignDb } from "./sqlite-db.ts";

export interface MemoryHit {
  /** Человекочитаемый источник: "транскрипт, день 3", "NPC Марта", ... */
  source: string;
  /** Номер игрового дня, если известен. */
  day?: number;
  /** Фрагмент текста с подсветкой совпадения. */
  snippet: string;
}

export interface SearchOptions {
  /** Максимум результатов. */
  limit?: number;
  /** Ширина окна сниппета в символах вокруг совпадения. */
  context?: number;
  /** Максимум сниппетов на один источник. */
  perSource?: number;
}

export const DEFAULT_LIMIT = 20;
export const DEFAULT_CONTEXT = 120;
const DEFAULT_PER_SOURCE = 3;

/**
 * Разбивает запрос на «термы» — нормализованные слова длиной ≥ 2.
 * Регистр и пунктуация игнорируются; кириллица и латиница поддерживаются.
 */
export function queryTerms(query: string): string[] {
  const matches = query.toLowerCase().match(/[\p{L}\p{N}]{2,}/gu);
  if (!matches) return [];
  // Отбрасываем дубликаты, сохраняя порядок.
  return [...new Set(matches)];
}

/** Нормализует строку для поиска: нижний регистр, единые пробелы. */
export function normalize(text: string): string {
  return text.toLowerCase().replace(/\s+/g, " ").trim();
}

/**
 * Строит сниппет вокруг первого совпадения любого терма в нормализованной строке.
 * Возвращает undefined, если ни один терм не найден.
 */
export function snippetAround(text: string, terms: string[], context: number): string | undefined {
  const lower = normalize(text);
  if (lower.length === 0) return undefined;
  let earliest = -1;
  for (const term of terms) {
    const at = lower.indexOf(term);
    if (at !== -1 && (earliest === -1 || at < earliest)) earliest = at;
  }
  if (earliest === -1) return undefined;
  const start = Math.max(0, earliest - Math.floor(context / 2));
  const end = Math.min(lower.length, earliest + context);
  // Срез берём по исходной строке, чтобы сохранить регистр; но выравниваем по
  // границам слов, чтобы не разрезать термин пополам.
  const slice = text.slice(start, end);
  const prefix = start > 0 ? "…" : "";
  const suffix = end < text.length ? "…" : "";
  return `${prefix}${slice}${suffix}`;
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : value === null || value === undefined ? "" : String(value);
}

/** Склеивает поля источника в один текст для сниппета (переносы строк схлопываются). */
function joinFields(fields: string[]): string {
  return fields
    .map((field) => field.replace(/\s+/g, " ").trim())
    .filter(Boolean)
    .join(" ");
}

/** Описание одного источника памяти: SQL-запрос и форматирование хита. */
interface MemorySource {
  table: string;
  /** Колонка с id кампании (у campaigns — собственная id-колонка). */
  idColumn?: string;
  select: string;
  /** ORDER BY для JS-пути: свежие дни/записи первыми (perSource берёт голову). */
  orderBy: string;
  source: (row: Record<string, unknown>) => string;
  day: (row: Record<string, unknown>) => number | undefined;
  snippet: (row: Record<string, unknown>) => string;
  /**
   * true — источник индексируется в FTS5 (fts_journal) и ищется префиксным
   * MATCH; только append-only таблицы с колонками (id, campaign_id, day, line).
   */
  fts?: boolean;
}

// --- FTS5: полнотекстовый индекс журнальных таблиц ---

/**
 * DDL индекса. fts_journal — обычная FTS5-таблица (текст хранится в индексе,
 * сниппеты читаются из неё), фильтр кампании — по хранимой колонке
 * campaign_id. AFTER DELETE-триггеры вычищают индекс при удалении строк
 * источников (это делает миграция дедупа key_events; журнальные таблицы
 * текст не обновляют). DDL идемпотентен — выполняется при каждом поиске
 * (дёшево; заодно чинит процесс, в котором fts-таблиц ещё не было).
 */
const FTS_SCHEMA = `
CREATE VIRTUAL TABLE IF NOT EXISTS fts_journal USING fts5(
  source UNINDEXED, source_rowid UNINDEXED, campaign_id UNINDEXED, day UNINDEXED, line,
  tokenize = 'unicode61'
);
CREATE TABLE IF NOT EXISTS fts_meta (
  source TEXT PRIMARY KEY,
  last_source_id INTEGER NOT NULL DEFAULT 0
);
CREATE TRIGGER IF NOT EXISTS fts_journal_delete_transcript AFTER DELETE ON transcript_entries BEGIN
  DELETE FROM fts_journal WHERE source = 'transcript_entries' AND source_rowid = OLD.id;
END;
CREATE TRIGGER IF NOT EXISTS fts_journal_delete_key_events AFTER DELETE ON key_events BEGIN
  DELETE FROM fts_journal WHERE source = 'key_events' AND source_rowid = OLD.id;
END;
CREATE TRIGGER IF NOT EXISTS fts_journal_delete_ledger_rows AFTER DELETE ON ledger_rows BEGIN
  DELETE FROM fts_journal WHERE source = 'ledger_rows' AND source_rowid = OLD.id;
END;
`;

/**
 * Доступен ли FTS5 на текущем поиске. Выставляется ensureFtsIndex; при
 * неудаче DDL (нет FTS5 в сборке, таблицы-источники ещё не созданы) —
 * все источники идут через JS-скан.
 */
let ftsEnabled = false;

/**
 * Создаёт индекс (идемпотентно) и доиндексирует строки, добавленные с
 * прошлого поиска. Ограничение: рассчитан на однопроцессный деплой — два
 * процесса с общим WAL могут оба доиндексировать одни и те же строки (в
 * fts_journal нет уникальности по source+source_rowid). Возвращает true,
 * если индекс готов.
 */
function ensureFtsIndex(handle: BetterSqlite3.Database): boolean {
  try {
    handle.exec(FTS_SCHEMA);
    ftsEnabled = true;
  } catch {
    // FTS5 недоступен или таблицы-источники ещё не созданы — JS-скан до
    // следующего поиска (DDL идемпотентен, повтор безопасен).
    ftsEnabled = false;
    return false;
  }
  const getLast = handle.prepare("SELECT last_source_id FROM fts_meta WHERE source = ?");
  const insertFts = handle.prepare(
    "INSERT INTO fts_journal(source, source_rowid, campaign_id, day, line) VALUES (?, ?, ?, ?, ?)",
  );
  const setLast = handle.prepare(
    `INSERT INTO fts_meta(source, last_source_id) VALUES (?, ?)
     ON CONFLICT(source) DO UPDATE SET last_source_id = excluded.last_source_id`,
  );
  for (const source of SOURCES) {
    if (!source.fts) continue;
    const table = source.table;
    try {
      const last = (getLast.get(table) as { last_source_id: number } | undefined)?.last_source_id ?? 0;
      const rows = handle.prepare(`SELECT id, campaign_id, day, line FROM ${table} WHERE id > ? ORDER BY id`)
        .all(last) as { id: number; campaign_id: string; day: number; line: string }[];
      if (rows.length === 0) continue;
      // Счётчик обновляется в той же транзакции, что и вставка: крэш между
      // ними не приведёт к повторной индексации (и дублям) при следующем поиске.
      const txn = handle.transaction((batch: typeof rows) => {
        for (const row of batch) insertFts.run(table, row.id, row.campaign_id, row.day, row.line);
        setLast.run(table, batch[batch.length - 1].id);
      });
      txn(rows);
    } catch {
      // Таблицы-источника может не быть (модуль-владелец ещё не создал DDL) —
      // пропускаем источник, остальные продолжают.
    }
  }
  return true;
}

/**
 * Строит FTS5 MATCH-запрос из термов: кавычки + префикс («прокляти*» матчит
 * «проклятие»), термы склеиваются AND — семантика как у JS-пути. Кавычки
 * обезопашивают термы, совпадающие с операторами FTS5 (AND/OR/NOT).
 * Звёздочка СНАРУЖИ кавычек: `"терм*"` молча теряет префиксное совпадение.
 */
export function buildMatchQuery(terms: string[]): string {
  return terms.map((term) => `"${term}"*`).join(" AND ");
}

/** Поиск одного FTS-источника: свежие дни первыми, не более perSource хитов. */
function searchSourceFts(
  handle: BetterSqlite3.Database,
  campaignId: string,
  source: MemorySource,
  terms: string[],
  context: number,
  perSource: number,
): MemoryHit[] {
  // Запас поверх perSource: если у совпавшей строки не собрался сниппет
  // (например, латинская диакритика: индекс сворачивает café→cafe, а JS-поиск
  // по сырому тексту терм не находит), хит выпадает — берём больше строк.
  const fetch = perSource * 3 + 4;
  const rows = handle.prepare(
    `SELECT day, line FROM fts_journal
     WHERE fts_journal MATCH ? AND campaign_id = ? AND source = ?
     ORDER BY day DESC, rowid DESC LIMIT ?`,
  ).all(buildMatchQuery(terms), campaignId, source.table, fetch) as { day: number; line: string }[];
  const hits: MemoryHit[] = [];
  for (const row of rows) {
    const snippet = snippetAround(row.line, terms, context);
    if (snippet) hits.push({ source: source.source(row), day: row.day, snippet });
  }
  return hits.slice(0, perSource);
}

const SOURCES: MemorySource[] = [
  {
    table: "transcript_entries",
    select: "line, day",
    orderBy: "day DESC, id DESC",
    source: (row) => `транскрипт, день ${row.day}`,
    day: (row) => (typeof row.day === "number" ? row.day : undefined),
    snippet: (row) => asString(row.line),
    fts: true,
  },
  {
    table: "key_events",
    select: "line, day",
    orderBy: "day DESC, id DESC",
    source: (row) => `ключевое событие, день ${row.day}`,
    day: (row) => (typeof row.day === "number" ? row.day : undefined),
    snippet: (row) => asString(row.line),
    fts: true,
  },
  {
    table: "ledger_rows",
    select: "line, day",
    orderBy: "day DESC, id DESC",
    source: (row) => `журнал лута, день ${row.day}`,
    day: (row) => (typeof row.day === "number" ? row.day : undefined),
    snippet: (row) => asString(row.line),
    fts: true,
  },
  {
    table: "world_changes",
    select: "text, category, day",
    orderBy: "day IS NULL, day DESC, id DESC",
    source: () => "состояние мира",
    day: (row) => (typeof row.day === "number" ? row.day : undefined),
    snippet: (row) =>
      asString(row.category) ? `${asString(row.category)}: ${asString(row.text)}` : asString(row.text),
  },
  {
    table: "npcs",
    select: "name, role, memory, last_seen_day",
    orderBy: "last_seen_day IS NULL, last_seen_day DESC, id DESC",
    source: (row) => `NPC ${asString(row.name)}`,
    day: (row) => (typeof row.last_seen_day === "number" ? row.last_seen_day : undefined),
    snippet: (row) => joinFields([asString(row.name), asString(row.role), asString(row.memory)]),
  },
  {
    table: "locations",
    select: "name, description, discovered_day",
    orderBy: "discovered_day IS NULL, discovered_day DESC, id DESC",
    source: (row) => `локация ${asString(row.name)}`,
    day: (row) => (typeof row.discovered_day === "number" ? row.discovered_day : undefined),
    snippet: (row) => joinFields([asString(row.name), asString(row.description)]),
  },
  {
    table: "factions",
    select: "name, description",
    orderBy: "id DESC",
    source: (row) => `фракция ${asString(row.name)}`,
    day: () => undefined,
    snippet: (row) => joinFields([asString(row.name), asString(row.description)]),
  },
  {
    table: "characters",
    select: "name, background, motivation",
    orderBy: "id DESC",
    source: (row) => `персонаж ${asString(row.name)}`,
    day: () => undefined,
    snippet: (row) => joinFields([asString(row.name), asString(row.background), asString(row.motivation)]),
  },
  {
    table: "quests",
    select: "title, objective, created_day",
    orderBy: "created_day DESC, id DESC",
    source: (row) => `квест ${asString(row.title)}`,
    day: (row) => (typeof row.created_day === "number" ? row.created_day : undefined),
    snippet: (row) => joinFields([asString(row.title), asString(row.objective)]),
  },
  {
    table: "campaigns",
    select: "title, description, setting",
    idColumn: "id",
    orderBy: "id DESC",
    source: () => "кампания",
    day: () => undefined,
    snippet: (row) => joinFields([asString(row.title), asString(row.description), asString(row.setting)]),
  },
];

/** Все ли термы встречаются в нормализованном тексте (AND). */
function matchesAll(text: string, terms: string[]): boolean {
  const lower = normalize(text);
  return terms.every((term) => lower.includes(term));
}

/**
 * Ищет в одном источнике: строки кампании фильтруются по термам в JS.
 * Термы не уходят в SQL LIKE: встроенный lower() в SQLite работает только
 * с латиницей, и по кириллице («Каэль» vs «каэль») совпадения терялись бы.
 *
 * Для FTS-источников первым пробуется префиксный FTS5-поиск; при ошибке или
 * пустом результате источник доискивается JS-сканом — гарантия, что поиск
 * не хуже прежнего (включая совпадения внутри слова, которые префиксные
 * токены не ловят: «эль» → «Каэль»).
 *
 * Семантика perSource у обоих путей единая: берутся самые свежие совпадения
 * источника (ORDER BY day DESC, id DESC — так же, как в FTS-пути).
 */
function searchSource(
  handle: BetterSqlite3.Database,
  campaignId: string,
  source: MemorySource,
  terms: string[],
  context: number,
  perSource: number,
  out: MemoryHit[],
): void {
  if (source.fts && ftsEnabled) {
    try {
      const ftsHits = searchSourceFts(handle, campaignId, source, terms, context, perSource);
      if (ftsHits.length > 0) {
        out.push(...ftsHits);
        return;
      }
    } catch {
      // Падаем в JS-скан ниже.
    }
  }

  const idColumn = source.idColumn ?? "campaign_id";
  const rows = handle.prepare(
    `SELECT ${source.select} FROM ${source.table} WHERE ${idColumn} = ? ORDER BY ${source.orderBy}`,
  ).all(campaignId) as Record<string, unknown>[];
  const hits: MemoryHit[] = [];
  for (const row of rows) {
    const text = source.snippet(row);
    if (!matchesAll(text, terms)) continue;
    const snippet = snippetAround(text, terms, context);
    if (snippet) {
      hits.push({ source: source.source(row), day: source.day(row), snippet });
    }
  }
  // Голова в порядке day DESC, id DESC = самые свежие записи.
  out.push(...hits.slice(0, perSource));
}

/**
 * Ищет совпадения по словам запроса во всех таблицах памяти кампании.
 * Возвращает до limit результатов, отсортированных: сначала дни с большим
 * номером (свежее), затем по источнику.
 */
export function searchCampaignMemory(slug: string, query: string, options: SearchOptions = {}): MemoryHit[] {
  const terms = queryTerms(query);
  if (terms.length === 0) return [];

  const limit = options.limit ?? DEFAULT_LIMIT;
  const context = options.context ?? DEFAULT_CONTEXT;
  const perSource = options.perSource ?? DEFAULT_PER_SOURCE;

  const handle = openCampaignDb();
  const campaign = handle.prepare("SELECT id FROM campaigns WHERE slug = ?").get(slug) as
    | { id: string }
    | undefined;
  if (!campaign) return [];

  // Индекс строим только для существующей кампании: поиск по незнакомому slug
  // не должен запускать catch-up всей истории.
  ensureFtsIndex(handle);

  const hits: MemoryHit[] = [];
  for (const source of SOURCES) {
    try {
      searchSource(handle, campaign.id, source, terms, context, perSource, hits);
    } catch {
      // Таблицы источника могло не быть (модуль-владелец ещё не создал DDL) —
      // пропускаем источник, остальные продолжают искать.
    }
  }

  // Свежие дни и активные сущности наверх.
  hits.sort((a, b) => {
    const dayA = a.day ?? -1;
    const dayB = b.day ?? -1;
    if (dayA !== dayB) return dayB - dayA;
    return a.source.localeCompare(b.source);
  });
  return hits.slice(0, limit);
}
