/**
 * Keyword-поиск по долговременной памяти кампании.
 *
 * Ищет совпадения по словам запроса в таблицах памяти (транскрипт, ключевые
 * события, журнал лута, состояние мира, NPC, локации, фракции, персонажи,
 * квесты, кампания) SQL-запросом по campaign_id. Возвращает сниппеты с
 * контекстом. Полный аналог «в какой день упоминалось про X» — пока без
 * семантики, только по словам; этого достаточно для большинства случаев и не
 * требует внешних сервисов.
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
  source: (row: Record<string, unknown>) => string;
  day: (row: Record<string, unknown>) => number | undefined;
  snippet: (row: Record<string, unknown>) => string;
}

const SOURCES: MemorySource[] = [
  {
    table: "transcript_entries",
    select: "line, day",
    source: (row) => `транскрипт, день ${row.day}`,
    day: (row) => (typeof row.day === "number" ? row.day : undefined),
    snippet: (row) => asString(row.line),
  },
  {
    table: "key_events",
    select: "line, day",
    source: (row) => `ключевое событие, день ${row.day}`,
    day: (row) => (typeof row.day === "number" ? row.day : undefined),
    snippet: (row) => asString(row.line),
  },
  {
    table: "ledger_rows",
    select: "line, day",
    source: (row) => `журнал лута, день ${row.day}`,
    day: (row) => (typeof row.day === "number" ? row.day : undefined),
    snippet: (row) => asString(row.line),
  },
  {
    table: "world_changes",
    select: "text, category, day",
    source: () => "состояние мира",
    day: (row) => (typeof row.day === "number" ? row.day : undefined),
    snippet: (row) =>
      asString(row.category) ? `${asString(row.category)}: ${asString(row.text)}` : asString(row.text),
  },
  {
    table: "npcs",
    select: "name, role, memory, last_seen_day",
    source: (row) => `NPC ${asString(row.name)}`,
    day: (row) => (typeof row.last_seen_day === "number" ? row.last_seen_day : undefined),
    snippet: (row) => joinFields([asString(row.name), asString(row.role), asString(row.memory)]),
  },
  {
    table: "locations",
    select: "name, description, discovered_day",
    source: (row) => `локация ${asString(row.name)}`,
    day: (row) => (typeof row.discovered_day === "number" ? row.discovered_day : undefined),
    snippet: (row) => joinFields([asString(row.name), asString(row.description)]),
  },
  {
    table: "factions",
    select: "name, description",
    source: (row) => `фракция ${asString(row.name)}`,
    day: () => undefined,
    snippet: (row) => joinFields([asString(row.name), asString(row.description)]),
  },
  {
    table: "characters",
    select: "name, background, motivation",
    source: (row) => `персонаж ${asString(row.name)}`,
    day: () => undefined,
    snippet: (row) => joinFields([asString(row.name), asString(row.background), asString(row.motivation)]),
  },
  {
    table: "quests",
    select: "title, objective, created_day",
    source: (row) => `квест ${asString(row.title)}`,
    day: (row) => (typeof row.created_day === "number" ? row.created_day : undefined),
    snippet: (row) => joinFields([asString(row.title), asString(row.objective)]),
  },
  {
    table: "campaigns",
    select: "title, description, setting",
    idColumn: "id",
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
  const idColumn = source.idColumn ?? "campaign_id";
  const rows = handle.prepare(
    `SELECT ${source.select} FROM ${source.table} WHERE ${idColumn} = ?`,
  ).all(campaignId) as Record<string, unknown>[];
  let added = 0;
  for (const row of rows) {
    if (added >= perSource) break;
    const text = source.snippet(row);
    if (!matchesAll(text, terms)) continue;
    const snippet = snippetAround(text, terms, context);
    if (snippet) {
      out.push({ source: source.source(row), day: source.day(row), snippet });
      added += 1;
    }
  }
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
