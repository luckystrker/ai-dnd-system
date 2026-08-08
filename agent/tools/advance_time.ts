import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError, timeOfDaySchema, type TimeOfDay } from "../lib/campaigns/types.ts";

const ORDER: TimeOfDay[] = ["morning", "day", "evening", "night"];

const LABEL: Record<TimeOfDay, string> = {
  morning: "утро",
  day: "день",
  evening: "вечер",
  night: "ночь",
};

export default defineTool({
  description:
    "Advance the in-game time of day by one phase (morning -> day -> evening -> night -> morning). " +
    "Wrapping past night to morning means a new day starts — call advance_day next. Night imposes " +
    "disadvantage on sight-based checks (Perception/Investigation/Survival) and advantage on Stealth. " +
    "Optionally set the in-game calendar date string.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    timeOfDay: timeOfDaySchema
      .optional()
      .describe("Явно установить фазу суток; иначе — следующая по порядку."),
    inGameDate: z
      .string()
      .max(200)
      .optional()
      .describe("Дата в календаре мира (напр. «3 день Месяца Туманов»)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const current = campaign.timeOfDay ?? "morning";
      const next: TimeOfDay = input.timeOfDay ?? ORDER[(ORDER.indexOf(current) + 1) % ORDER.length];
      const updated = campaignStore.setEnvironment(campaign.id, {
        timeOfDay: next,
        inGameDate: input.inGameDate,
      });
      const wrappedToMorning = next === "morning" && current !== "morning";
      const note = wrappedToMorning
        ? "Прошла полночь — наступило утро. Теперь вызови advance_day, чтобы перевести игру на новый день (и закрыть старый через chronicler)."
        : `Игровое время: ${LABEL[next]}.`;
      return {
        ok: true,
        timeOfDay: next,
        inGameDate: updated.inGameDate ?? null,
        note,
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
