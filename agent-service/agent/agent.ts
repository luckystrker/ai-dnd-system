import { defineAgent } from "eve";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";

const llm = createOpenAICompatible({
  baseURL: process.env.LLM_BASE_URL ?? "https://openrouter.ai/api/v1",
  name: "ttrpg-llm",
  apiKey: process.env.LLM_API_KEY,
});

export default defineAgent({
  model: llm(process.env.LLM_MODEL ?? "deepseek-v4-flash"),
  modelContextWindowTokens: 900000,
  limits: {
    sessionTimeoutMs: false,
  },
});
