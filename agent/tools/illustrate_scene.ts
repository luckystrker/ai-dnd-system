/**
 * Иллюстрация сцены: генерация картинки через Runware и доставка в чат
 * кампании. Работает только в чате с привязанной кампанией; любой сбой —
 * мягкая ошибка, игра продолжается текстом.
 */
import { defineTool } from "eve/tools";
import { z } from "zod";

import { callTelegramApi, sendTelegramChatAction } from "eve/channels/telegram";

import { resolveCampaign } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { generateSceneImage, runwareConfigured } from "../lib/runware.ts";
import { appearancesForCharacters, buildScenePrompt } from "../lib/scene-prompt.ts";

/** Лимит Telegram на подпись к фото. */
const TELEGRAM_CAPTION_MAX_LENGTH = 1024;

export default defineTool({
  description:
    "Generate an illustration of the current scene and post it to the campaign chat. " +
    "Works only inside a chat with an active campaign. Call it when a significant new " +
    "scene begins. Describe the scene in English. If it fails, just continue narrating " +
    "in text and do not mention the failure.",
  inputSchema: z.object({
    sceneDescription: z
      .string()
      .max(500)
      .describe(
        "Краткое описание сцены НА АНГЛИЙСКОМ: место, действие, атмосфера. Станет подписью к картинке.",
      ),
    characters: z
      .array(z.string())
      .max(6)
      .optional()
      .describe("Имена персонажей игроков в кадре — их внешность будет учтена в картинке."),
  }),
  async execute({ sceneDescription, characters }, ctx) {
    const campaign = resolveCampaign(ctx.session.auth.current);
    if (!campaign?.boundChat) {
      return { ok: false, error: "Иллюстрации доступны только в рамках кампании, привязанной к чату." };
    }
    if (!runwareConfigured()) {
      return { ok: false, error: "Генерация картинок не настроена (нет RUNWARE_API_KEY). Продолжай текстом." };
    }

    const sheets = campaignStore.listCharacters(campaign.id);
    const positivePrompt = buildScenePrompt({
      sceneDescription,
      appearances: appearancesForCharacters(characters, sheets),
      setting: campaign.setting,
      theme: campaign.theme,
    });

    let imageUrl: string;
    try {
      imageUrl = await generateSceneImage({ positivePrompt, abortSignal: ctx.abortSignal });
    } catch (error) {
      console.error("illustrate_scene: generation failed", error);
      return { ok: false, error: "Не получилось сгенерировать картинку. Продолжи сцену текстом." };
    }

    const { chatId, messageThreadId } = campaign.boundChat;
    try {
      await sendTelegramChatAction({ action: "upload_photo", chatId, messageThreadId }).catch(() => undefined);
      const result = await callTelegramApi({
        method: "sendPhoto",
        body: {
          chat_id: chatId,
          photo: imageUrl,
          caption: sceneDescription.slice(0, TELEGRAM_CAPTION_MAX_LENGTH),
          ...(messageThreadId !== undefined ? { message_thread_id: messageThreadId } : {}),
        },
      });
      if (!result.ok) {
        throw new Error(`sendPhoto failed with HTTP ${result.status}`);
      }
    } catch (error) {
      console.error("illustrate_scene: sendPhoto failed", error);
      return {
        ok: false,
        imageUrl,
        error: "Картинка сгенерирована, но отправить её в чат не вышло. Продолжи текстом.",
      };
    }

    return {
      ok: true,
      imageUrl,
      note: "Иллюстрация отправлена в чат кампании. Продолжай повествование, не пересказывая картинку.",
    };
  },
});
