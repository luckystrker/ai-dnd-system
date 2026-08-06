/**
 * In-memory sliding window rate limiter для защиты webhook от флуда.
 *
 * Один экземпляр на процесс — подходит для одиночного инстанса eve.
 * При горизонтальном масштабировании нужен общий стор (например, Redis).
 */
export class SlidingWindowLimiter {
  private readonly hits = new Map<string, number[]>();

  /**
   * true, если запрос укладывается в лимит (и он учтён);
   * false, если окно уже заполнено.
   */
  allow(key: string, limit: number, windowMs: number, now: number = Date.now()): boolean {
    const windowStart = now - windowMs;
    const timestamps = (this.hits.get(key) ?? []).filter((ts) => ts > windowStart);
    if (timestamps.length >= limit) {
      this.hits.set(key, timestamps);
      return false;
    }
    timestamps.push(now);
    this.hits.set(key, timestamps);
    // Периодически чистим полностью опустевшие ключи, чтобы карта не росла.
    if (this.hits.size > 10_000) {
      for (const [k, ts] of this.hits) {
        if (ts.length === 0) this.hits.delete(k);
      }
    }
    return true;
  }
}

function envLimit(name: string, fallback: number): number {
  const raw = process.env[name];
  if (!raw) return fallback;
  const value = Number(raw);
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

const MINUTE_MS = 60_000;

/** Общий лимитер Telegram-канала. */
export const telegramLimiter = new SlidingWindowLimiter();

/** Лимиты на чат/топик и на пользователя (сообщений в минуту). */
export const TELEGRAM_RATE_LIMITS = {
  chatPerMinute: envLimit("TELEGRAM_RATE_LIMIT_CHAT_PER_MIN", 20),
  userPerMinute: envLimit("TELEGRAM_RATE_LIMIT_USER_PER_MIN", 10),
  windowMs: MINUTE_MS,
};
