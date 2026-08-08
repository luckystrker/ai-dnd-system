import { defineTool } from "eve/tools";
import { z } from "zod";

import { resolveCampaignForWrite } from "../lib/campaigns/access.ts";
import { factionStore } from "../lib/campaigns/factions.ts";
import { appendKeyEvent, appendLedgerRow, appendTranscriptEntry } from "../lib/campaigns/journal.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import { StoreError, type CharacterSheet, type Quest, type QuestDifficulty } from "../lib/campaigns/types.ts";
import {
  levelForXp,
  middleOf,
  questGoldRange,
  questXpPerCharacter,
} from "../lib/rewards.ts";

function findQuest(campaignId: string, questIdOrSlug: string): Quest {
  const needle = questIdOrSlug.toLowerCase();
  const quest = campaignStore.listQuests(campaignId).find(
    (candidate) =>
      candidate.id === questIdOrSlug ||
      candidate.slug.toLowerCase() === needle ||
      candidate.title.toLowerCase() === needle,
  );
  if (!quest) {
    throw new StoreError(`Квест «${questIdOrSlug}» не найден в кампании.`, "not_found");
  }
  return quest;
}

function partyLevel(characters: CharacterSheet[]): number {
  if (characters.length === 0) return 1;
  const sum = characters.reduce((acc, sheet) => acc + (sheet.level ?? 1), 0);
  return Math.max(1, Math.round(sum / characters.length));
}

/** Дельта репутации фракции за итог квеста по сложности. */
function factionStandingDelta(difficulty: QuestDifficulty, result: string): number {
  if (result === "completed") return difficulty === "hard" ? 2 : 1;
  if (result === "failed" || result === "abandoned") return difficulty === "hard" ? -2 : -1;
  return 0;
}

export default defineTool({
  description:
    "Complete (or fail/abandon) a quest: closes the quest, grants the rewards to the whole party " +
    "(XP to each member, gold split across the party, planned items) and records a key event. " +
    "Rewards come from the quest's rewardPlan; empty fields are computed from tables by quest " +
    "difficulty and party level. Reports level-ups found — call level_up for each affected character.",
  inputSchema: z.object({
    campaignSlug: z
      .string()
      .optional()
      .describe("Slug кампании. Обязателен для автоматических вызовов (летописец); интерактивно можно не указывать."),
    quest: z.string().min(1).max(150).describe("Название (или slug/id) квеста."),
    result: z
      .enum(["completed", "failed", "abandoned"])
      .describe("Итог квеста: completed — с наградами; failed/abandoned — без."),
    factionSlug: z
      .string()
      .min(1)
      .max(100)
      .optional()
      .describe("Фракция-квестодатель: итог квеста скорректирует её репутацию (completed → рост, failed/abandoned → падение)."),
    rewardPlan: z
      .object({
        xp: z.number().int().min(0).max(10_000_000).optional().describe("XP каждому участнику (перекрывает план)."),
        gold: z.number().int().min(0).max(1_000_000).optional().describe("Золото на всю партию (перекрывает план)."),
        items: z.array(z.string().max(120)).max(20).optional().describe("Предметы каждому участнику (перекрывает план)."),
        note: z
          .string()
          .max(300)
          .optional()
          .describe("Свободная часть награды: услуга NPC, репутация, сюжетный бонус."),
      })
      .optional()
      .describe("Переопределение награды на этот раз (для неожиданных итогов)."),
  }),
  execute(input, ctx) {
    try {
      const campaign = resolveCampaignForWrite(ctx.session.auth.current, input.campaignSlug);
      const quest = findQuest(campaign.id, input.quest);
      const factionSlug = input.factionSlug ?? quest.giverNpcSlug;
      if (quest.status === "completed") {
        throw new StoreError(
          `Квест «${quest.title}» уже завершён — награды выданы, повторное завершение невозможно.`,
          "conflict",
        );
      }
      const day = campaign.currentDay ?? 1;
      const characters = campaignStore.listCharacters(campaign.id);

      const rewards = {
        xp: input.rewardPlan?.xp ?? quest.rewardPlan?.xp,
        gold: input.rewardPlan?.gold ?? quest.rewardPlan?.gold,
        items: input.rewardPlan?.items ?? quest.rewardPlan?.items ?? [],
        note: input.rewardPlan?.note ?? quest.rewardPlan?.note,
      };

      const granted: Array<{ character: string; xp: number; gold: number; items: string[]; newLevel: number | null }> = [];
      let totalGold = 0;

      if (input.result === "completed") {
        const level = partyLevel(characters);
        const xp = rewards.xp ?? questXpPerCharacter(quest.difficulty, level);
        const gold = rewards.gold ?? middleOf(questGoldRange(quest.difficulty, level));
        for (const sheet of characters) {
          campaignStore.grantCharacter(campaign.id, sheet.name, {
            xp,
            gold: Math.floor(gold / Math.max(1, characters.length)),
            inventory: rewards.items.length > 0 ? rewards.items : undefined,
          });
          totalGold += Math.floor(gold / Math.max(1, characters.length));
          const nextLevel = levelForXp((sheet.xp ?? 0) + xp);
          granted.push({
            character: sheet.name,
            xp,
            gold: Math.floor(gold / Math.max(1, characters.length)),
            items: rewards.items,
            newLevel: nextLevel > (sheet.level ?? 1) ? nextLevel : null,
          });
        }

        // C3: запись в журнал экономики за квестовую награду.
        const rewardParts: string[] = [];
        if (totalGold > 0) rewardParts.push(`${totalGold} золотых`);
        if (rewards.items.length > 0) rewardParts.push(rewards.items.join(", "));
        if (rewardParts.length > 0) {
          appendLedgerRow(campaign.slug, {
            day,
            type: "found",
            itemOrGold: rewardParts.join(", "),
            note: `награда за квест «${quest.title}»`,
          }, `quest:${quest.id}:reward:${day}`);
        }
      }

      campaignStore.updateQuest(campaign.id, quest.id, { status: input.result });

      // C4: корректировка репутации фракции-квестодателя (если она заведена).
      const factionDelta = factionSlug ? factionStandingDelta(quest.difficulty, input.result) : 0;
      let factionChange: { faction: string; standing: number; delta: number } | undefined;
      if (factionSlug && factionDelta !== 0) {
        try {
          const faction = factionStore.adjustStanding(campaign.id, factionSlug, factionDelta);
          factionChange = { faction: faction.name, standing: faction.standing, delta: factionDelta };
        } catch {
          // Фракция не заведена — корректировку пропускаем (это не ошибка закрытия квеста).
        }
      }

      const label = input.result === "completed" ? "завершён" : input.result === "failed" ? "провален" : "брошен";
      appendKeyEvent(
        campaign.slug,
        day,
        `Квест «${quest.title}» ${label}.${rewards.note ? ` Бонус: ${rewards.note}` : ""}`,
        `quest:${quest.id}:${input.result}`,
      );
      appendTranscriptEntry(campaign.slug, day, {
        kind: "action",
        text: `Квест «${quest.title}» ${label}${input.result === "completed" ? `: партия получила ${granted.length > 0 ? granted[0].xp : 0} XP и ${granted[0]?.gold ?? 0} золота (каждому)` : ""}`,
        eventId: `evt:quest:${quest.id}:${input.result}`,
      });

      const levelUps = granted
        .filter((entry) => entry.newLevel !== null)
        .map((entry) => ({ character: entry.character, newLevel: entry.newLevel }));

      return {
        ok: true,
        quest: {
          title: quest.title,
          result: input.result,
          difficulty: quest.difficulty,
        },
        granted,
        totalGold,
        levelUps,
        factionChange: factionChange ?? null,
        note:
          input.result === "completed"
            ? granted.length === 0
              ? "Квест отмечен завершённым, но награды не выданы: в партии нет персонажей."
              : levelUps.length > 0
                ? `Награды выданы. ${levelUps.length} персонаж(ей) достиг(ли) нового уровня — вызови level_up для каждого: ${levelUps.map((entry) => entry.character).join(", ")}.`
                : "Награды выданы всей партии. Отрази результат в ответе игрокам."
            : `Квест отмечен как ${label}. Награды не выдавались.`,
      };
    } catch (error) {
      if (error instanceof StoreError) return { ok: false, error: error.message };
      throw error;
    }
  },
});
