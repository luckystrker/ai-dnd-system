/**
 * Состояние мира кампании (C5 — последствия): перезаписываемый файл
 * history/world-state.md с актуальными фактами мира, сгруппированными по
 * категориям (## Погибшие, ## Изменения и т.п.). В отличие от key-events.md
 * (append-only журнал моментов), этот файл отражает текущее состояние мира —
 * поэтому записи upsert'ятся по (категория + текст), а не дописываются.
 *
 * Назначение: чтобы бот не «оживлял» убитых NPC и последовательно учитывал
 * прошлые решения. Грузится в авто-блок памяти целиком (компактно, с капом).
 */
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { buildDocument } from "./frontmatter.ts";
import { assertCampaignSlug, campaignDataRoot } from "./store.ts";
import type { WorldChange } from "./types.ts";

function worldStatePath(campaignSlug: string): string {
  assertCampaignSlug(campaignSlug);
  return join(campaignDataRoot(), campaignSlug, "history", "world-state.md");
}

/**
 * Записывает (upsert) изменение мира в категорию. Идемпотентно по тексту:
 * если запись с таким текстом уже есть в категории — обновляет её день,
 * иначе добавляет. Дубликаты (та же категория + тот же текст) не создаются.
 */
export function upsertWorldChange(campaignSlug: string, change: WorldChange): void {
  const categories = readWorldState(campaignSlug);
  const category = change.category.trim() || "Изменения";
  const text = change.text.replace(/\s*\n\s*/g, " ").trim();
  if (!text) return;

  const items = categories.get(category) ?? [];
  const dayMark = change.day !== undefined ? ` (день ${change.day})` : "";
  const line = `- ${text}${dayMark}`;
  const normalizedText = text.toLowerCase();
  const existingIndex = items.findIndex((item) => {
    // Убираем маркер списка «- » и пометку дня для сравнения по тексту.
    const itemText = item
      .replace(/^-\s+/, "")
      .replace(/\s*\(день \d+\)\s*$/, "")
      .trim()
      .toLowerCase();
    return itemText === normalizedText;
  });
  if (existingIndex >= 0) {
    items[existingIndex] = line;
  } else {
    items.push(line);
  }
  categories.set(category, items);
  writeWorldState(campaignSlug, categories);
}

/** Считывает состояние мира как карту категория → список строк. */
export function readWorldState(campaignSlug: string): Map<string, string[]> {
  const path = worldStatePath(campaignSlug);
  const categories = new Map<string, string[]>();
  if (!existsSync(path)) return categories;
  const body = readFileSync(path, "utf8");
  let currentCategory = "";
  for (const line of body.split("\n")) {
    const heading = /^##\s+(.+?)\s*$/.exec(line);
    if (heading) {
      currentCategory = heading[1].trim();
      if (!categories.has(currentCategory)) categories.set(currentCategory, []);
      continue;
    }
    if (line.startsWith("- ") && currentCategory) {
      categories.get(currentCategory)!.push(line);
    }
  }
  return categories;
}

/** Человекочитаемый рендер состояния мира (для memory-блока и list_world_state). */
export function renderWorldState(campaignSlug: string): string {
  return renderCategories(readWorldState(campaignSlug));
}

/** Рендерит переданные категории в текст. */
function renderCategories(categories: Map<string, string[]>): string {
  if (categories.size === 0) return "";
  const parts: string[] = [];
  for (const [category, items] of categories) {
    if (items.length === 0) continue;
    parts.push(`## ${category}\n${items.join("\n")}`);
  }
  return parts.join("\n\n");
}

function writeWorldState(campaignSlug: string, categories: Map<string, string[]>): void {
  const path = worldStatePath(campaignSlug);
  mkdirSync(join(path, ".."), { recursive: true });
  const body = renderCategories(categories);
  writeFileSync(path, buildDocument({ updatedAt: new Date().toISOString() }, body || "Нет записей."), "utf8");
}
