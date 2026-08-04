/**
 * Динамическая подгрузка памяти кампании в контекст каждого хода.
 *
 * На turn.started резолвим кампанию по чату звонящего и собираем компактный
 * блок: мета кампании, хроника прошлых дней, ключевые события, саммари и
 * хвост транскрипта текущего дня, ростер NPC и состояние партии.
 * Полный транскрипт при этом не читается — только дайджесты и хвосты.
 */
import { defineDynamic, defineInstructions } from "eve/instructions";

import {
  readCampaignSummary,
  readDayTail,
  readKeyEvents,
} from "../lib/campaigns/journal.ts";
import { npcStore } from "../lib/campaigns/npc.ts";
import { resolveCallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import type { Campaign, CharacterSheet, NpcProfile } from "../lib/campaigns/types.ts";

const SUMMARY_CAP = 2000;
const KEY_EVENTS_CAP = 2000;
const DAY_TAIL_LINES = 30;
const NPC_ROSTER_CAP = 20;

function capTail(text: string, max: number): string {
  if (text.length <= max) return text;
  return `…(начало опущено)\n${text.slice(-max)}`;
}

function npcRoster(npcs: NpcProfile[]): string {
  if (npcs.length === 0) return "(NPC пока не заведены)";
  const lines = npcs.slice(0, NPC_ROSTER_CAP).map((npc) => {
    const relations = Object.entries(npc.relationships)
      .map(([name, relation]) => `${name}: ${relation.attitude > 0 ? "+" : ""}${relation.attitude}`)
      .join(", ");
    const parts = [
      npc.role ?? "NPC",
      npc.status,
      npc.location ? `локация: ${npc.location}` : null,
      relations ? `отношения: ${relations}` : null,
    ].filter(Boolean);
    return `- ${npc.name} — ${parts.join("; ")}`;
  });
  if (npcs.length > NPC_ROSTER_CAP) lines.push(`- …и ещё ${npcs.length - NPC_ROSTER_CAP} (list_npcs / get_npc)`);
  return lines.join("\n");
}

function partyState(characters: CharacterSheet[]): string {
  if (characters.length === 0) return "(персонажи ещё не созданы)";
  return characters
    .map((sheet) => {
      const parts = [`${sheet.characterClass}, ур. ${sheet.level}`];
      if (sheet.hp !== undefined || sheet.maxHp !== undefined) {
        parts.push(`HP ${sheet.hp ?? "?"}/${sheet.maxHp ?? "?"}`);
      }
      if (sheet.conditions?.length) parts.push(`состояния: ${sheet.conditions.join(", ")}`);
      if (sheet.gold !== undefined) parts.push(`золото: ${sheet.gold}`);
      if (sheet.location) parts.push(`локация: ${sheet.location}`);
      return `- ${sheet.name} (${parts.join("; ")})`;
    })
    .join("\n");
}

function buildMemoryBlock(campaign: Campaign): string {
  const slug = campaign.slug;
  const day = campaign.currentDay ?? 1;

  const sections: string[] = [
    `## Память кампании «${campaign.title}» (игровой день ${day})`,
    [
      `Сеттинг: ${campaign.setting}`,
      `Тема: ${campaign.theme}`,
      campaign.goal ? `Цель: ${campaign.goal}` : null,
      campaign.tone ? `Тон: ${campaign.tone}` : null,
    ]
      .filter(Boolean)
      .join("; "),
  ];

  const summary = readCampaignSummary(slug);
  if (summary) {
    sections.push("### Хроника прошлых дней\n" + capTail(summary, SUMMARY_CAP));
  }

  const keyEvents = readKeyEvents(slug);
  if (keyEvents) {
    sections.push("### Ключевые события\n" + capTail(keyEvents, KEY_EVENTS_CAP));
  }

  const currentDay = readDayTail(slug, day, DAY_TAIL_LINES);
  if (currentDay) {
    const dayParts = [`### Текущий день ${day}`];
    if (currentDay.summary) dayParts.push(`Саммари дня: ${currentDay.summary}`);
    if (currentDay.entries.length > 0) {
      dayParts.push(`Последние события (последние ${currentDay.entries.length} записей транскрипта):`);
      dayParts.push(...currentDay.entries);
    }
    sections.push(dayParts.join("\n"));
  }

  sections.push("### NPC\n" + npcRoster(npcStore.listNpcs(campaign.id)));
  sections.push("### Партия\n" + partyState(campaignStore.listCharacters(campaign.id)));

  sections.push(
    "Полный транскрипт дня — через read_day, карточка NPC — через get_npc. " +
      "Изменения состояния фиксируй через update_character / upsert_npc; новый день — advance_day.",
  );

  return sections.join("\n\n");
}

export default defineDynamic({
  events: {
    "turn.started": (_event, ctx) => {
      try {
        const identity = resolveCallerIdentity(ctx.session.auth.current);
        const campaign = identity?.chatId
          ? campaignStore.findByBoundChat(identity.chatId, identity.messageThreadId)
          : undefined;
        if (!campaign) return null;
        return defineInstructions({ markdown: buildMemoryBlock(campaign) });
      } catch {
        // Память — дополнение к игре: при сбое чтения ход продолжается без неё.
        return null;
      }
    },
  },
});
