/**
 * Детерминированная сборка промпта для иллюстраций сцен: описание сцены +
 * внешность персонажей в кадре + сеттинг кампании. Без LLM-вызовов.
 */
import type { CharacterSheet } from "./campaigns/types.ts";

/** Фиксированный стилистический суффикс для консистентности стиля. */
export const STYLE_SUFFIX = "high fantasy, cinematic lighting, detailed illustration";

/** Дефолтный negative-промпт против типичных артефактов генерации. */
export const DEFAULT_NEGATIVE_PROMPT = "text, watermark, signature, blurry, low quality, deformed";

export interface ScenePromptInput {
  sceneDescription: string;
  /** Внешность персонажей в кадре (уже сматченная по листам). */
  appearances: readonly string[];
  setting: string;
  theme: string;
}

export function buildScenePrompt(input: ScenePromptInput): string {
  return [
    input.sceneDescription,
    ...input.appearances,
    input.setting ? `Setting: ${input.setting}` : "",
    input.theme ? `Theme: ${input.theme}` : "",
    STYLE_SUFFIX,
  ]
    .filter(Boolean)
    .map((part) => part.trim())
    .join(", ");
}

/**
 * Внешность персонажей по списку имён. Сопоставление case-insensitive по
 * имени листа; неизвестные имена тихо игнорируются, листы без внешности
 * ничего не добавляют.
 */
export function appearancesForCharacters(
  names: readonly string[] | undefined,
  sheets: readonly CharacterSheet[],
): string[] {
  if (!names || names.length === 0) return [];
  const appearances: string[] = [];
  for (const name of names) {
    const needle = name.trim().toLowerCase();
    if (!needle) continue;
    const sheet = sheets.find((candidate) => candidate.name.toLowerCase() === needle);
    if (sheet?.appearance) appearances.push(sheet.appearance);
  }
  return appearances;
}
