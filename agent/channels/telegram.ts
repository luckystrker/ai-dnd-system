/**
 * Кастомный Telegram-канал: одна durable-сессия на чат (в форумах — на топик).
 *
 * Встроенный telegramChannel в группах ключит сессии по якорю сообщения:
 * каждое сообщение БЕЗ reply на последнее сообщение бота получает уникальный
 * continuation token и открывает новую сессию без истории. Из-за этого
 * интервью создания кампании «зацикливалось» на первом вопросе и никогда не
 * доходило до save_campaign.
 *
 * Здесь continuation token = chatId + message_thread_id, поэтому все
 * сообщения чата/топика попадают в одну сессию независимо от reply.
 * Контракт вебхука (путь, секретный заголовок, формат update) сохранён —
 * telegram-poll.ts и e2e-скрипты работают без изменений.
 */
import { defineChannel, POST } from "eve/channels";
import {
  answerTelegramCallbackQuery,
  buildTelegramTurnMessage,
  collectTelegramFileParts,
  createTelegramFetchFile,
  defaultTelegramAuth,
  formatTelegramContextBlock,
  parseTelegramUpdate,
  sendTelegramChatAction,
  sendTelegramMessage,
  splitTelegramMessageText,
  verifyTelegramRequest,
  type TelegramChatType,
  type TelegramMessage,
  type TelegramUser,
} from "eve/channels/telegram";

import { telegramChatQueue } from "../lib/chat-queue.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { MAX_PARTY } from "../lib/campaigns/types.ts";
import { telegramLimiter, TELEGRAM_RATE_LIMITS } from "../lib/rate-limit.ts";

const BOT_USERNAME = process.env.TELEGRAM_BOT_USERNAME;

/** Максимальный размер входящего update от Telegram (защита от аномальных payload). */
const MAX_UPDATE_BYTES = (() => {
  const raw = process.env.TELEGRAM_MAX_UPDATE_BYTES;
  const value = raw ? Number(raw) : NaN;
  return Number.isFinite(value) && value > 0 ? value : 1024 * 1024;
})();

/** Та же политика вложений, что по умолчанию у встроенного канала. */
const UPLOAD_POLICY = { allowedMediaTypes: "*", maxBytes: 25 * 1024 * 1024 } as const;

interface TelegramState {
  chatId: string | null;
  chatType: TelegramChatType | null;
  messageThreadId: number | null;
}

const initialState: TelegramState = { chatId: null, chatType: null, messageThreadId: null };

/** Один токен на чат/топик — вся переписка чата живёт в одной сессии. */
function continuationTokenFor(message: TelegramMessage): string {
  return `${message.chat.id}:${topicOf(message) ?? ""}`;
}

/**
 * Настоящий форум-топик сообщения. Осторожно: Telegram ставит
 * message_thread_id ЛЮБОМУ реплаю (= id сообщения, на который ответили),
 * а не только топикам форума. Отличить их можно только по флагу
 * is_topic_message из сырого update. Если брать message_thread_id как есть,
 * каждый реплай на новое сообщение бота открывал бы новую сессию.
 */
function topicOf(message: TelegramMessage): number | undefined {
  return message.raw?.is_topic_message === true ? message.messageThreadId : undefined;
}

/**
 * Auth для сессии. defaultTelegramAuth из eve копирует в attributes сырой
 * message_thread_id из update. Telegram ставит его ЛЮБОМУ реплаю в супергруппе
 * (= id корневого сообщения тред-реплая), а не только форум-топикам. Такой
 * «топик» ломает поиск кампании по привязанному чату: bound_thread_id NULL не
 * совпадает с id реплая, и память кампании в группах не подгружалась.
 * Приводим message_thread_id к настоящему топику форума (topicOf).
 */
function sessionAuth(message: TelegramMessage): ReturnType<typeof defaultTelegramAuth> {
  const auth = defaultTelegramAuth(message);
  if (!auth) return null;
  const topic = topicOf(message);
  const attributes: Record<string, string | readonly string[]> = {};
  for (const [key, value] of Object.entries(auth.attributes)) {
    if (key === "message_thread_id") continue;
    attributes[key] = value;
  }
  if (topic !== undefined) attributes.message_thread_id = String(topic);
  return { ...auth, attributes };
}

function isBotCommand(text: string, botUsername: string | undefined): boolean {
  const match = /^\/(?<command>[A-Za-z0-9_]+)(?:@(?<target>[A-Za-z0-9_]+))?(?:\s|$)/u.exec(text);
  if (!match) return false;
  const target = match.groups?.target;
  return target === undefined || (botUsername !== undefined && target.toLowerCase() === botUsername.toLowerCase());
}

/** Команда /join (в т.ч. /join@bot) — явное вступление в кампанию чата. */
function isJoinCommand(text: string): boolean {
  return /^\/join(?:@[A-Za-z0-9_]+)?(?:\s|$)/iu.test(text);
}

/**
 * Обработка /join: регистрирует автора как игрока кампании, привязанной
 * к чату/топику. Возвращает true, если команда обработана и ход агента
 * запускать не нужно.
 */
function handleJoinCampaign(message: TelegramMessage): boolean {
  const text = (message.text || message.caption) ?? "";
  if (!isBotCommand(text, BOT_USERNAME) || !isJoinCommand(text)) return false;
  if (!message.from || message.from.isBot) return true;

  const state: TelegramState = {
    chatId: message.chat.id,
    chatType: message.chat.type,
    messageThreadId: topicOf(message) ?? null,
  };
  const campaign = campaignStore.findByBoundChat(message.chat.id, topicOf(message), { anyStatus: true });
  if (!campaign) {
    void postText(state, "В этом чате нет кампании, к которой можно присоединиться.");
    return true;
  }
  const before = campaign.members.some((member) => member.userId === message.from!.id);
  const updated = campaignStore.autoRegister(campaign.id, {
    userId: message.from.id,
    name: displayName(message.from),
    username: message.from.username,
  });
  const after = updated.members.some((member) => member.userId === message.from!.id);
  let reply: string;
  if (before) {
    reply = `Ты уже участвуешь в кампании «${campaign.title}».`;
  } else if (after) {
    reply = `Ты присоединился к кампании «${campaign.title}»! Теперь можно создать персонажа.`;
  } else {
    reply = `Партия кампании «${campaign.title}» заполнена (максимум ${MAX_PARTY} игроков).`;
  }
  void postText(state, reply);
  return true;
}

/** Имя пользователя для транскрипта/регистрации, если нет username. */
function displayName(user: TelegramUser): string | undefined {
  const name = [user.firstName, user.lastName].filter(Boolean).join(" ").trim();
  return name || undefined;
}

/**
 * Правила диспетчеризации: боту уходит каждое осмысленное сообщение любого
 * чата — лички, группы и форумы без ограничений. В группах бот видит и
 * сообщения без @упоминания/реплая (переписку игроков между собой), чтобы
 * не терять контекст: правила ведения таких разговоров заданы в
 * instructions.md. В транскрипт сообщения пишет transcript-хук.
 */
function shouldDispatch(message: TelegramMessage): boolean {
  if (message.from?.isBot || message.chat.type === "channel") return false;
  const text = message.text || message.caption;
  return text.trim().length > 0 || message.attachments.length > 0;
}

async function startTyping(state: TelegramState): Promise<void> {
  const chatId = state.chatId;
  if (!chatId) return;
  try {
    await sendTelegramChatAction({
      action: "typing",
      chatId,
      messageThreadId: state.messageThreadId ?? undefined,
    });
  } catch {
    // Индикатор набора — не критично.
  }
}

async function postText(state: TelegramState, text: string): Promise<void> {
  const chatId = state.chatId;
  if (!chatId) return;
  try {
    for (const chunk of splitTelegramMessageText(text)) {
      await sendTelegramMessage({
        body: { text: chunk, message_thread_id: state.messageThreadId ?? undefined },
        chatId,
      });
    }
  } catch (error) {
    console.error("telegram channel: sendMessage failed", error);
  }
}

interface TelegramChannelContext {
  state: TelegramState;
}

export default defineChannel<TelegramState, TelegramChannelContext>({
  state: initialState,

  metadata(state) {
    return { chatId: state.chatId, chatType: state.chatType };
  },

  fetchFile: createTelegramFetchFile({ policy: UPLOAD_POLICY }),

  context(state) {
    return { state };
  },

  routes: [
    POST("/eve/v1/telegram", async (req, { send, waitUntil }) => {
      let raw: string;
      try {
        raw = await verifyTelegramRequest(req, {
          secretToken: process.env.TELEGRAM_WEBHOOK_SECRET_TOKEN,
        });
      } catch {
        return new Response("unauthorized", { status: 401 });
      }

      if (Buffer.byteLength(raw, "utf8") > MAX_UPDATE_BYTES) {
        return new Response("payload too large", { status: 413 });
      }

      let parsed: unknown;
      try {
        parsed = JSON.parse(raw);
      } catch {
        return new Response("ok");
      }

      const update = parseTelegramUpdate(parsed);
      if (!update) return new Response("ok");

      if (update.kind === "callback_query") {
        waitUntil(
          answerTelegramCallbackQuery({
            callbackQueryId: update.callbackQuery.id,
            text: "Unsupported action.",
          }).catch(() => undefined),
        );
        return new Response("ok");
      }

      const message = update.message;

      // Защита от флуда: превышение лимита молча игнорируем (ответ "ok"),
      // чтобы Telegram не ретраил запрос и не усиливал нагрузку.
      const chatKey = `chat:${message.chat.id}:${topicOf(message) ?? ""}`;
      if (!telegramLimiter.allow(chatKey, TELEGRAM_RATE_LIMITS.chatPerMinute, TELEGRAM_RATE_LIMITS.windowMs)) {
        return new Response("ok");
      }
      if (message.from && !message.from.isBot) {
        const userKey = `user:${message.from.id}`;
        if (!telegramLimiter.allow(userKey, TELEGRAM_RATE_LIMITS.userPerMinute, TELEGRAM_RATE_LIMITS.windowMs)) {
          return new Response("ok");
        }
      }

      // Апдейты одного чата обрабатываем строго по очереди: два send() одного
      // чата одновременно (два быстрых сообщения подряд) — это два параллельных
      // workflow-рана одной сессии, и второй падает с HookConflictError, теряя
      // сообщение. См. PerChatQueue.
      if (handleJoinCampaign(message)) return new Response("ok");

      if (!shouldDispatch(message)) return new Response("ok");

      const state: TelegramState = {
        chatId: message.chat.id,
        chatType: message.chat.type,
        messageThreadId: topicOf(message) ?? null,
      };

      waitUntil(startTyping(state));

      const contextBlock = formatTelegramContextBlock({
        botUsername: BOT_USERNAME,
        chatId: message.chat.id,
        chatTitle: message.chat.title,
        chatType: message.chat.type,
        messageId: message.messageId,
        // Тот же topic, что и в state.messageThreadId: сырой message_thread_id
        // у обычных реплаев — это id сообщения-родителя, а не форум-топик.
        messageThreadId: topicOf(message),
        userId: message.from?.id,
        username: message.from?.username,
      });
      const turnMessage = buildTelegramTurnMessage(
        message,
        collectTelegramFileParts(message.attachments, UPLOAD_POLICY),
      );

      waitUntil(
        telegramChatQueue.enqueue(chatKey, async () => {
          try {
            await send(
              { message: turnMessage, context: [contextBlock] },
              {
                auth: sessionAuth(message),
                continuationToken: continuationTokenFor(message),
                state,
              },
            );
          } catch (error) {
            console.error("telegram channel: message delivery failed", error);
          }
        }),
      );

      return new Response("ok");
    }),
  ],

  events: {
    async "turn.started"(_event, channel) {
      await startTyping(channel.state);
    },
    async "actions.requested"(_event, channel) {
      await startTyping(channel.state);
    },
    async "message.completed"(event, channel) {
      if (event.finishReason === "tool-calls" || !event.message) return;
      await postText(channel.state, event.message);
    },
    async "turn.failed"(event, channel) {
      const hint = event.message ? `: ${event.message}` : "";
      await postText(channel.state, `Не получилось обработать сообщение${hint}. Попробуй ещё раз.`);
    },
    async "session.failed"(_event, channel) {
      await postText(channel.state, "Сессия не восстановилась после ошибки. Напиши новое сообщение, чтобы продолжить.");
    },
  },
});
