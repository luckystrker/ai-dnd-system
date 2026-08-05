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

import { appendTranscriptEntry } from "../lib/campaigns/journal.ts";
import { campaignStore } from "../lib/campaigns/store.ts";

const BOT_USERNAME = process.env.TELEGRAM_BOT_USERNAME;

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

function isBotCommand(text: string, botUsername: string | undefined): boolean {
  const match = /^\/(?<command>[A-Za-z0-9_]+)(?:@(?<target>[A-Za-z0-9_]+))?(?:\s|$)/u.exec(text);
  if (!match) return false;
  const target = match.groups?.target;
  return target === undefined || (botUsername !== undefined && target.toLowerCase() === botUsername.toLowerCase());
}

/** Имя пользователя для транскрипта/регистрации, если нет username. */
function displayName(user: TelegramUser): string | undefined {
  const name = [user.firstName, user.lastName].filter(Boolean).join(" ").trim();
  return name || undefined;
}

/**
 * Наблюдение за всем трафиком группы в чате/топике, привязанном к кампании
 * (статус кампании не важен — авто-вступление работает в любой момент).
 *
 * Каждый написавший автоматически становится игроком кампании. Сообщения НЕ
 * в адрес бота дополнительно пишутся в транскрипт дня, чтобы DM видел
 * диалоги игроков между собой; ход агента при этом не запускается — пока
 * игроки общаются сами, бот молчит. Сообщения, проходящие shouldDispatch,
 * в транскрипт здесь не дублируются: их запишет transcript-хук.
 */
function observeCampaignTraffic(message: TelegramMessage): void {
  try {
    if (message.chat.type === "private" || message.chat.type === "channel") return;
    if (!message.from || message.from.isBot) return;
    const campaign = campaignStore.findByBoundChat(message.chat.id, topicOf(message), { anyStatus: true });
    if (!campaign) return;

    campaignStore.autoRegister(campaign.id, {
      userId: message.from.id,
      name: displayName(message.from),
      username: message.from.username,
    });

    if (shouldDispatch(message)) return;
    const text = message.text || message.caption;
    if (!text.trim()) return;
    appendTranscriptEntry(campaign.slug, campaign.currentDay ?? 1, {
      kind: "player",
      author: message.from.username ?? displayName(message.from) ?? message.from.id,
      text,
      eventId: `tg-${message.chat.id}-${message.messageId}`,
    });
  } catch (error) {
    // Наблюдение не должно ломать обработку самого сообщения.
    console.error("telegram channel: campaign observation failed", error);
  }
}

/**
 * Правила диспетчеризации как у встроенного канала: личка — всё; группы —
 * только команды, @упоминания и реплаи на сообщения бота.
 */
function shouldDispatch(message: TelegramMessage): boolean {
  if (message.from?.isBot || message.chat.type === "channel") return false;
  const text = message.text || message.caption;
  if (!text.trim() && message.attachments.length === 0) return false;
  if (message.chat.type === "private") return true;
  return (
    message.replyToMessage?.from?.isBot === true ||
    isBotCommand(text, BOT_USERNAME) ||
    (BOT_USERNAME !== undefined && text.toLowerCase().includes(`@${BOT_USERNAME.toLowerCase()}`))
  );
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
      observeCampaignTraffic(message);
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
        messageThreadId: message.messageThreadId,
        userId: message.from?.id,
        username: message.from?.username,
      });
      const turnMessage = buildTelegramTurnMessage(
        message,
        collectTelegramFileParts(message.attachments, UPLOAD_POLICY),
      );

      waitUntil(
        (async () => {
          try {
            await send(
              { message: turnMessage, context: [contextBlock] },
              {
                auth: defaultTelegramAuth(message),
                continuationToken: continuationTokenFor(message),
                state,
              },
            );
          } catch (error) {
            console.error("telegram channel: message delivery failed", error);
          }
        })(),
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
