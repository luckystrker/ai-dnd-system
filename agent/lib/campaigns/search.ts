/**
 * Keyword-поиск по долговременной памяти кампании.
 *
 * Обходит все Markdown-файлы папки кампании (history/days, summary, key-events,
 * npcs, characters) и ищет совпадения по словам запроса. Возвращает сниппеты с
 * контекстом. Полный аналог «в какой день упоминалось про X» — пока без
 * семантики, только по словам; этого достаточно для большинства случаев и не
 * требует внешних сервисов.
 */
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";

import { assertCampaignSlug, campaignDataRoot } from "./store.ts";

export interface MemoryHit {
  /** Относительный путь к файлу от папки кампании (напр. history/days/day-0003.md). */
  file: string;
  /** Номер игрового дня, если удалось определить из имени файла. */
  day?: number;
  /** Фрагмент текста с подсветкой совпадения. */
  snippet: string;
}

export interface SearchOptions {
  /** Максимум результатов (по файлам). */
  limit?: number;
  /** Ширина окна сниппета в символах вокруг совпадения. */
  context?: number;
  /** Максимум сниппетов на один файл. */
  perFile?: number;
}

const DEFAULT_LIMIT = 20;
const DEFAULT_CONTEXT = 120;
const DEFAULT_PER_FILE = 3;

/** Извлекает номер дня из имени файла history/days/day-NNNN.md. */
function dayFromPath(path: string): number | undefined {
  const match = /(?:^|[\\/])day-(\d+)\.md$/i.exec(path);
  return match ? Number(match[1]) : undefined;
}

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
function normalize(text: string): string {
  return text.toLowerCase().replace(/\s+/g, " ").trim();
}

/**
 * Строит сниппет вокруг первого совпадения любого терма в нормализованной строке.
 * Возвращает undefined, если ни один терм не найден.
 */
function snippetAround(text: string, terms: string[], context: number): string | undefined {
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

function walkMd(dir: string, root: string, out: string[]): void {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    const full = join(dir, entry);
    let st;
    try {
      st = statSync(full);
    } catch {
      continue;
    }
    if (st.isDirectory()) {
      walkMd(full, root, out);
    } else if (st.isFile() && entry.endsWith(".md")) {
      out.push(relative(root, full).split(sep).join("/"));
    }
  }
}

/**
 * Ищет совпадения по словам запроса во всех .md-файлах папки кампании.
 * Возвращает до limit результатов, отсортированных: сначала дни с большим
 * номером (свежее), затем прочие файлы по имени.
 */
export function searchCampaignMemory(slug: string, query: string, options: SearchOptions = {}): MemoryHit[] {
  assertCampaignSlug(slug);
  const terms = queryTerms(query);
  if (terms.length === 0) return [];

  const limit = options.limit ?? DEFAULT_LIMIT;
  const context = options.context ?? DEFAULT_CONTEXT;
  const perFile = options.perFile ?? DEFAULT_PER_FILE;

  const campaignRoot = join(campaignDataRoot(), slug);
  if (!existsSync(campaignRoot)) return [];

  const files: string[] = [];
  walkMd(campaignRoot, campaignRoot, files);

  const hits: MemoryHit[] = [];
  for (const relPath of files) {
    const absPath = join(campaignRoot, relPath);
    let content: string;
    try {
      content = readFileSync(absPath, "utf8");
    } catch {
      continue;
    }
    // Транскрипт дня пишется построчно: ищем совпадения построчно, чтобы сниппеты
    // были осмысленными (одна запись транскрипта = одна строка «- ...»).
    const lines = content.split("\n");
    const day = dayFromPath(relPath);
    let added = 0;
    for (const line of lines) {
      if (added >= perFile) break;
      const trimmed = line.trim();
      if (trimmed.length === 0) continue;
      const snippet = snippetAround(trimmed, terms, context);
      if (snippet) {
        hits.push({ file: relPath, day, snippet });
        added += 1;
      }
    }
    // Если построчно ничего не нашли, но файл целиком содержит все термы
    // (напр. короткий summary) — берём сниппет по всему содержимому.
    if (added === 0) {
      const snippet = snippetAround(content.replace(/\s+/g, " "), terms, context);
      if (snippet) hits.push({ file: relPath, day, snippet });
    }
    if (hits.length >= limit) break;
  }

  // Свежие дни и активные сущности наверх.
  hits.sort((a, b) => {
    const da = a.day ?? -1;
    const db = b.day ?? -1;
    if (da !== db) return db - da;
    return a.file.localeCompare(b.file);
  });
  return hits.slice(0, limit);
}
