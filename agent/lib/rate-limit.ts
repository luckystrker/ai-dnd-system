/**
 * In-memory sliding window rate limiter для защиты webhook от флуда.
 *
 * Один экземпляр на процесс — подходит для одиночного инстанса eve.
 * При горизонтальном масштабировании нужен общий стор (например, Redis).
 *
 * Чтобы карта не росла без ограничений (каждый уникальный chat/user id —
 * отдельный ключ), неактивные ключи периодически вычищаются: ключ, к которому
 * не обращались дольше windowMs, удаляется целиком.
 */
interface Bucket {
  /** Таймстампы разрешённых запросов в текущем окне. */
  hits: number[];
  /** Момент последнего обращения к ключу (для очистки неактивных). */
  lastAccessed: number;
}

export class SlidingWindowLimiter {
  private readonly buckets = new Map<string, Bucket>();

  /**
   * true, если запрос укладывается в лимит (и он учтён);
   * false, если окно уже заполнено.
   */
  allow(key: string, limit: number, windowMs: number, now: number = Date.now()): boolean {
    const windowStart = now - windowMs;
    const bucket = this.buckets.get(key);
    const hits = (bucket?.hits ?? []).filter((ts) => ts > windowStart);
    const allowed = hits.length < limit;
    if (allowed) hits.push(now);
    this.buckets.set(key, { hits, lastAccessed: now });
    // Периодически вычищаем ключи, к которым давно не обращались, — иначе карта
    // растёт с каждым новым chat/user id и процесс падает по OOM.
    if (this.buckets.size > 1000) this.sweep(now, windowMs);
    return allowed;
  }

  /** Удаляет ключи, неактивные дольше windowMs. Экспонировано для тестов. */
  sweep(now: number, windowMs: number): void {
    const cutoff = now - windowMs;
    for (const [key, bucket] of this.buckets) {
      if (bucket.lastAccessed < cutoff) this.buckets.delete(key);
    }
  }

  /** Число отслеживаемых ключей (для тестов и мониторинга). */
  size(): number {
    return this.buckets.size;
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
