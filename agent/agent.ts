import { defineAgent, defineDynamic } from "eve";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";

import { findCampaignForIdentity } from "./lib/campaigns/access.ts";
import { resolveCallerIdentity } from "./lib/campaigns/session.ts";
import type { CampaignLength } from "./lib/campaigns/types.ts";

const llm = createOpenAICompatible({
  baseURL: process.env.LLM_BASE_URL ?? "https://openrouter.ai/api/v1",
  name: "ttrpg-llm",
  apiKey: process.env.LLM_API_KEY,
});

const model = llm(process.env.LLM_MODEL ?? "deepseek-v4-flash");

/**
 * Sliding window: чем длиннее кампания, тем больше последних сообщений
 * держится в контексте. При приближении к окну встроенный compaction eve
 * суммаризирует старые ходы; полный оригинал всегда остаётся в транскрипте дня.
 * Коротким кампаниям большое окно не нужно — так compaction срабатывает
 * раньше и расход токенов заметно ниже.
 */
const CONTEXT_WINDOW_BY_LENGTH: Record<CampaignLength, number> = {
  short: 256_000,
  medium: 512_000,
  long: 900_000,
};
const DEFAULT_CONTEXT_WINDOW = 512_000;

function contextWindowFor(auth: unknown): number {
  const identity = resolveCallerIdentity(auth);
  const campaign = identity ? findCampaignForIdentity(identity) : undefined;
  if (!campaign) return DEFAULT_CONTEXT_WINDOW;
  return CONTEXT_WINDOW_BY_LENGTH[campaign.length] ?? DEFAULT_CONTEXT_WINDOW;
}

export default defineAgent({
  model: defineDynamic({
    fallback: model,
    events: {
      // step.started выбран потому, что session/turn-селекции принимают только
      // строковые model id, а наша модель — LanguageModel кастомного провайдера.
      "step.started": (_event, ctx) => ({
        model,
        modelContextWindowTokens: contextWindowFor(ctx.session.auth.current),
      }),
    },
  }),
  modelContextWindowTokens: DEFAULT_CONTEXT_WINDOW,
  compaction: {
    thresholdPercent: 0.7,
  },
  limits: {
    sessionTimeoutMs: false,
  },
  build: {
    // Нативный аддон: бандлинг ломает поиск .node-бинарника, поэтому пакет
    // остаётся внешним и резолвится обычным Node.js-способом.
    externalDependencies: ["better-sqlite3"],
  },
});
