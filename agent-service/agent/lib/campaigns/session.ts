/**
 * Извлечение личности звонящего и координат чата из session auth.
 *
 * Telegram-канал eve по умолчанию ставит на каждое входящее сообщение
 * auth: principalId вида `telegram:<userId>` (личка) или
 * `telegram:<chatId>:<userId>` (группа) и attributes с user_id, chat_id,
 * chat_type, message_thread_id и username.
 */

export interface CallerIdentity {
  userId: string;
  username?: string;
  chatId?: string;
  /** Тип чата из auth: private / group / supergroup / channel. */
  chatType?: string;
  messageThreadId?: number;
}

interface AuthLike {
  principalId?: string;
  attributes?: Record<string, string | readonly string[]>;
}

function attribute(auth: AuthLike, key: string): string | undefined {
  const value = auth.attributes?.[key];
  if (typeof value === "string") return value;
  return value?.[0];
}

/**
 * Разбирает auth текущей сессии. Возвращает undefined, когда личность
 * недоступна (например, сессия без Telegram-идентификации).
 */
export function resolveCallerIdentity(auth: unknown): CallerIdentity | undefined {
  if (!auth || typeof auth !== "object") return undefined;
  const candidate = auth as AuthLike;
  const userId = attribute(candidate, "user_id");
  if (!userId) return undefined;
  const rawThreadId = attribute(candidate, "message_thread_id");
  const messageThreadId = rawThreadId !== undefined ? Number(rawThreadId) : NaN;
  return {
    userId,
    username: attribute(candidate, "username"),
    chatId: attribute(candidate, "chat_id"),
    chatType: attribute(candidate, "chat_type"),
    messageThreadId: Number.isFinite(messageThreadId) ? messageThreadId : undefined,
  };
}
