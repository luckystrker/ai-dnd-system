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
import { campaignDataRoot } from "./store.ts";

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
  entries: string[];
}

function historyDir(campaignSlug: string): string {
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

/** Добавляет ключевое событие в history/key-events.md. */
export function appendKeyEvent(campaignSlug: string, day: number, text: string, eventId?: string): void {
  const dir = historyDir(campaignSlug);
  mkdirSync(dir, { recursive: true });
  const path = join(dir, "key-events.md");
  const marker = eventId ? `evt:${eventId}` : undefined;
  if (marker && existsSync(path) && readFileSync(path, "utf8").includes(marker)) return;
  const clean = text.replace(/\s*\n\s*/g, " ").trim();
  if (!clean) return;
  let line = `- **День ${day}**: ${clean}`;
  if (marker) line += ` <!-- ${marker} -->`;
  appendFileSync(path, `${line}\n`, "utf8");
}

export function readKeyEvents(campaignSlug: string): string {
  const path = join(historyDir(campaignSlug), "key-events.md");
  if (!existsSync(path)) return "";
  return readFileSync(path, "utf8").trim();
}
