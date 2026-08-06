/**
 * Минимальный клиент Runware: одна imageInference-задача через REST.
 * Чистый fetch, без SDK и WebSocket. Конфигурация — в env:
 * RUNWARE_API_KEY (обязателен), RUNWARE_MODEL, RUNWARE_WIDTH, RUNWARE_HEIGHT.
 */
import { randomUUID } from "node:crypto";

import { DEFAULT_NEGATIVE_PROMPT } from "./scene-prompt.ts";

const RUNWARE_API_URL = "https://api.runware.ai/v1";
const DEFAULT_MODEL = "runware:101@1";
const DEFAULT_SIZE = 1024;

/** Лимит генерации: дольше ждать не даём, DM продолжает текстом. */
export const RUNWARE_TIMEOUT_MS = 30_000;

export function runwareConfigured(): boolean {
  return Boolean(process.env.RUNWARE_API_KEY);
}

interface RunwareTaskResult {
  taskType?: string;
  imageURL?: string;
  error?: string;
  errorMessage?: string;
  [key: string]: unknown;
}

function extractTasks(payload: unknown): RunwareTaskResult[] {
  if (Array.isArray(payload)) return payload as RunwareTaskResult[];
  if (payload && typeof payload === "object") {
    const data = (payload as Record<string, unknown>).data;
    if (Array.isArray(data)) return data as RunwareTaskResult[];
  }
  return [];
}

export interface SceneImageOptions {
  positivePrompt: string;
  negativePrompt?: string;
  /** Дополнительный сигнал отмены (например, отмена активного хода). */
  abortSignal?: AbortSignal;
}

/** Генерирует одну картинку и возвращает её URL. Бросает Error с причиной. */
export async function generateSceneImage(options: SceneImageOptions): Promise<string> {
  const apiKey = process.env.RUNWARE_API_KEY;
  if (!apiKey) throw new Error("RUNWARE_API_KEY is not configured");

  const signal = options.abortSignal
    ? AbortSignal.any([AbortSignal.timeout(RUNWARE_TIMEOUT_MS), options.abortSignal])
    : AbortSignal.timeout(RUNWARE_TIMEOUT_MS);

  const response = await fetch(RUNWARE_API_URL, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${apiKey}`,
    },
    signal,
    body: JSON.stringify([
      {
        taskType: "imageInference",
        taskUUID: randomUUID(),
        model: process.env.RUNWARE_MODEL ?? DEFAULT_MODEL,
        positivePrompt: options.positivePrompt,
        negativePrompt: options.negativePrompt ?? DEFAULT_NEGATIVE_PROMPT,
        width: Number(process.env.RUNWARE_WIDTH ?? DEFAULT_SIZE),
        height: Number(process.env.RUNWARE_HEIGHT ?? DEFAULT_SIZE),
      },
    ]),
  });

  const payload: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const detail = payload === null ? "" : ` ${JSON.stringify(payload)}`;
    throw new Error(`Runware API error ${response.status}:${detail}`);
  }

  const task = extractTasks(payload).find((entry) => entry.taskType === "imageInference" || entry.imageURL);
  if (!task?.imageURL) {
    const reason = task?.error ?? task?.errorMessage;
    throw new Error(reason ? `Runware: ${reason}` : "Runware не вернул imageURL");
  }
  return task.imageURL;
}
