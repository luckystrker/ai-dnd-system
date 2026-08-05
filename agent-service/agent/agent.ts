import { defineAgent, defineDynamic } from "eve";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";

import { resolveCallerIdentity } from "./lib/campaigns/session.ts";
import { campaignStore } from "./lib/campaigns/store.ts";
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
 * суммаризирует старые ходы; полный оригинал всегда остаётся в history/days/.
 */
const CONTEXT_WINDOW_BY_LENGTH: Record<CampaignLength, number> = {
  short: 900_000,
  medium: 900_000,
  long: 900_000,
};
const DEFAULT_CONTEXT_WINDOW = 900_000;

function contextWindowFor(auth: unknown): number {
  const identity = resolveCallerIdentity(auth);
  const campaign = identity?.chatId
    ? campaignStore.findByBoundChat(identity.chatId, identity.messageThreadId)
    : undefined;
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
});
