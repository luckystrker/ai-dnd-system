/**
 * Журнал кампании: транскрипт по игровым дням, саммари дней,
 * накопительное саммари кампании и ключевые события.
 *
 * Всё лежит в папке кампании:
 *   history/days/day-NNNN.md  — транскрипт дня (frontmatter: day/date/note/summary)
 *   history/summary.md        — саммари завершённых дней (секции «## День N»)
 *   history/key-events.md     — список ключевых моментов кампании
 */
import { appendFileSync, existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { buildDocument, splitFrontmatter } from "./frontmatter.ts";
import { assertCampaignSlug, campaignDataRoot } from "./store.ts";
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

function historyDir(campaignSlug: string): string {
  assertCampaignSlug(campaignSlug);
  return join(campaignDataRoot(), campaignSlug, "history");
}

function daysDir(campaignSlug: string): string {
  return join(historyDir(campaignSlug), "days");
}

function dayFileName(day: number): string {
  return `day-${String(day).padStart(4, "0")}.md`;
}

function dayPath(campaignSlug: string, day: number): string {
  return join(daysDir(campaignSlug), dayFileName(day));
}

/** Создаёт файл дня, если его ещё нет. */
export function ensureDay(campaignSlug: string, day: number, meta?: { note?: string; date?: string }): void {
  const path = dayPath(campaignSlug, day);
  if (existsSync(path)) return;
  mkdirSync(daysDir(campaignSlug), { recursive: true });
  const data: Record<string, unknown> = {
    day,
    date: meta?.date ?? new Date().toISOString().slice(0, 10),
  };
  if (meta?.note) data.note = meta.note;
  writeFileSync(path, buildDocument(data, `# Игровой день ${day}`), "utf8");
}

/** Номера всех сохранённых дней по возрастанию. */
export function listDays(campaignSlug: string): number[] {
  const dir = daysDir(campaignSlug);
  if (!existsSync(dir)) return [];
  const days: number[] = [];
  for (const entry of readdirSync(dir)) {
    const match = /^day-(\d+)\.md$/.exec(entry);
    if (match) days.push(Number(match[1]));
  }
  return days.sort((a, b) => a - b);
}

/** Добавляет запись в транскрипт дня (append-only, дедуп по eventId). */
export function appendTranscriptEntry(campaignSlug: string, day: number, entry: TranscriptEntry): void {
  ensureDay(campaignSlug, day);
  const path = dayPath(campaignSlug, day);
  const marker = entry.eventId ? `evt:${entry.eventId}` : undefined;
  if (marker && readFileSync(path, "utf8").includes(marker)) return;

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
  appendFileSync(path, `${line}\n`, "utf8");
}

/** Читает день целиком; undefined, если файла нет. */
export function readDay(campaignSlug: string, day: number): DayRecord | undefined {
  return readDayTail(campaignSlug, day, Number.POSITIVE_INFINITY);
}

/** Читает последние maxLines записей дня; undefined, если файла нет. */
export function readDayTail(campaignSlug: string, day: number, maxLines: number): DayRecord | undefined {
  const path = dayPath(campaignSlug, day);
  if (!existsSync(path)) return undefined;
  const { data, body } = splitFrontmatter(readFileSync(path, "utf8"));
  const entries = body
    .split("\n")
    .filter((line) => line.startsWith("- "))
    .slice(-Math.max(0, Math.floor(maxLines)));
  return {
    day: typeof data.day === "number" ? data.day : day,
    date: data.date ? String(data.date) : undefined,
    note: data.note ? String(data.note) : undefined,
    summary: data.summary ? String(data.summary) : undefined,
    headline: data.headline ? String(data.headline) : undefined,
    entries,
  };
}

/** Записывает саммари дня в frontmatter файла дня. */
export function setDaySummary(campaignSlug: string, day: number, summary: string): void {
  ensureDay(campaignSlug, day);
  const path = dayPath(campaignSlug, day);
  const { data, body } = splitFrontmatter(readFileSync(path, "utf8"));
  writeFileSync(path, buildDocument({ ...data, summary: summary.trim() }, body), "utf8");
}

/**
 * Короткая «шапка» дня (≤140 симв.) — необрезаемая одна строка на каждый день,
 * чтобы в авто-блоке памяти была видна вся хроника кампании целиком, а не только
 * хвост последних ~2000 символов. Пишется chronicler'ом вместе с саммари.
 */
export function setDayHeadline(campaignSlug: string, day: number, headline: string): void {
  ensureDay(campaignSlug, day);
  const path = dayPath(campaignSlug, day);
  const { data, body } = splitFrontmatter(readFileSync(path, "utf8"));
  writeFileSync(path, buildDocument({ ...data, headline: headline.trim().slice(0, 140) }, body), "utf8");
}

export interface DayHeadline {
  day: number;
  headline: string;
}

/**
 * Шапки всех дней по возрастанию (для компактной хроники в авто-блоке памяти).
 * Дни без headline пропускаются. Чтение идёт только по frontmatter — быстро.
 */
export function listDayHeadlines(campaignSlug: string): DayHeadline[] {
  return listDays(campaignSlug).flatMap((day) => {
    const path = dayPath(campaignSlug, day);
    if (!existsSync(path)) return [];
    const { data } = splitFrontmatter(readFileSync(path, "utf8"));
    return data.headline ? [{ day, headline: String(data.headline) }] : [];
  });
}

/** Обновляет (или добавляет) секцию дня в накопительном history/summary.md. */
export function upsertCampaignSummaryDay(campaignSlug: string, day: number, text: string): void {
  const dir = historyDir(campaignSlug);
  mkdirSync(dir, { recursive: true });
  const path = join(dir, "summary.md");
  const section = `## День ${day}\n\n${text.trim()}`;
  const content = existsSync(path) ? readFileSync(path, "utf8") : "";
  const sectionPattern = new RegExp(`## День ${day}\\n[\\s\\S]*?(?=\\n## День \\d|$)`);
  const updated = sectionPattern.test(content)
    ? content.replace(sectionPattern, section)
    : `${content.trim()}\n\n${section}`.trim();
  writeFileSync(path, `${updated.trim()}\n`, "utf8");
}

export function readCampaignSummary(campaignSlug: string): string {
  const path = join(historyDir(campaignSlug), "summary.md");
  if (!existsSync(path)) return "";
  return readFileSync(path, "utf8").trim();
}

/**
 * Добавляет ключевое событие в history/key-events.md.
 * permanent-события помечаются маркером [важно] — они показываются в авто-блоке
 * памяти полностью (без обрезки), в отличие от обычных, которые обрезаются по
 * хвосту. permanent — для поворотных фактов (смерти, союзы, раскрытые секреты),
 * которые DM обязан помнить всю кампанию.
 */
export function appendKeyEvent(campaignSlug: string, day: number, text: string, eventId?: string, permanent = false): void {
  const dir = historyDir(campaignSlug);
  mkdirSync(dir, { recursive: true });
  const path = join(dir, "key-events.md");
  const marker = eventId ? `evt:${eventId}` : undefined;
  if (marker && existsSync(path) && readFileSync(path, "utf8").includes(marker)) return;
  const clean = text.replace(/\s*\n\s*/g, " ").trim();
  if (!clean) return;
  const prefix = permanent ? "[важно] " : "";
  let line = `- ${prefix}**День ${day}**: ${clean}`;
  if (marker) line += ` <!-- ${marker} -->`;
  appendFileSync(path, `${line}\n`, "utf8");
}

/** Разделяет ключевые события на permanent (без обрезки) и обычные (хвост). */
export function splitKeyEvents(campaignSlug: string): { permanent: string[]; regular: string[] } {
  const raw = readKeyEvents(campaignSlug);
  if (!raw) return { permanent: [], regular: [] };
  const permanent: string[] = [];
  const regular: string[] = [];
  for (const line of raw.split("\n")) {
    if (!line.startsWith("- ")) continue;
    if (line.startsWith("- [важно] ")) permanent.push(line);
    else regular.push(line);
  }
  return { permanent, regular };
}

export function readKeyEvents(campaignSlug: string): string {
  const path = join(historyDir(campaignSlug), "key-events.md");
  if (!existsSync(path)) return "";
  return readFileSync(path, "utf8").trim();
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
 * Добавляет запись в журнал экономики (history/ledger.md, append-only).
 * Дедуп по eventId-маркеру (идиома из appendKeyEvent). Записи пишутся
 * детерминированно из тулов grant_character / complete_quest — без отдельного
 * LLM-вызова.
 */
export function appendLedgerRow(campaignSlug: string, row: LedgerRow, eventId?: string): void {
  const dir = historyDir(campaignSlug);
  mkdirSync(dir, { recursive: true });
  const path = join(dir, "ledger.md");
  const marker = eventId ? `evt:${eventId}` : undefined;
  if (marker && existsSync(path) && readFileSync(path, "utf8").includes(marker)) return;
  const item = row.itemOrGold.replace(/\s*\n\s*/g, " ").trim();
  if (!item) return;
  const verb = LEDGER_VERB[row.type] ?? row.type;
  const by = row.by ? ` (${row.by})` : "";
  const note = row.note ? ` — ${row.note.replace(/\s*\n\s*/g, " ").trim()}` : "";
  let line = `- [День ${row.day}] ${verb}: ${item}${by}${note}`;
  if (marker) line += ` <!-- ${marker} -->`;
  appendFileSync(path, `${line}\n`, "utf8");
}

/** Читает журнал экономики (сырой текст). */
export function readLedgerRaw(campaignSlug: string): string {
  const path = join(historyDir(campaignSlug), "ledger.md");
  if (!existsSync(path)) return "";
  return readFileSync(path, "utf8").trim();
}

/** Считывает журнал экономики, опционально с фильтром по дню/типу; последние maxLines записей. */
export function readLedger(
  campaignSlug: string,
  filter?: { day?: number; type?: LedgerType },
  maxLines = Number.POSITIVE_INFINITY,
): string[] {
  const raw = readLedgerRaw(campaignSlug);
  if (!raw) return [];
  const lines = raw.split("\n").filter((line) => line.startsWith("- "));
  const filtered = lines.filter((line) => {
    if (filter?.day !== undefined) {
      const match = /\[День (\d+)\]/.exec(line);
      if (!match || Number(match[1]) !== filter.day) return false;
    }
    if (filter?.type !== undefined) {
      const verb = LEDGER_VERB[filter.type];
      if (!line.includes(`${verb}:`)) return false;
    }
    return true;
  });
  return filtered.slice(-Math.max(0, Math.floor(maxLines)));
}
