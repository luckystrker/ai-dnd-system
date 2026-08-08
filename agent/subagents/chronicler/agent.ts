import { defineAgent } from "eve";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import { z } from "zod";

const llm = createOpenAICompatible({
  baseURL: process.env.LLM_BASE_URL ?? "https://openrouter.ai/api/v1",
  name: "ttrpg-llm-chronicler",
  apiKey: process.env.LLM_API_KEY,
});

export default defineAgent({
  description:
    "Campaign chronicler: reads the transcript of an in-game day and writes the campaign's " +
    "long-term memory — day summary, campaign summary, key events, NPC memories/relationships, " +
    "player character state, time/weather, locations/map, world-state and factions — into the " +
    "campaign folder. Delegate to it when closing an in-game day (after advance_day) or after a " +
    "major story milestone; pass the campaign slug, the day number and what to chronicle.",
  model: llm(process.env.LLM_MODEL ?? "deepseek-v4-flash"),
  modelContextWindowTokens: 128000,
  // Task-mode: летописец возвращает короткий структурированный ack
  // о том, сколько записей памяти он сделал.
  outputSchema: z.object({
    daySummaryWritten: z.boolean().describe("Записано ли саммари дня"),
    keyEventsWritten: z.number().describe("Сколько ключевых событий записано"),
    npcsUpdated: z.number().describe("Сколько NPC создано или обновлено"),
    charactersUpdated: z.number().describe("Сколько персонажей игроков обновлено"),
    locationsUpdated: z.number().describe("Сколько локаций создано или обновлено"),
    factionsUpdated: z.number().describe("Сколько фракций создано или обновлено"),
    worldChangesWritten: z.number().describe("Сколько записей состояния мира добавлено"),
    note: z.string().optional().describe("Одно предложение итога, если есть что отметить"),
  }),
});
