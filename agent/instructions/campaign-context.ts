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
import { resolveCallerIdentity, type CallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import type { Campaign, CampaignMember, CharacterSheet, NpcProfile } from "../lib/campaigns/types.ts";

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

/** Человекочитаемая метка участника: @username, имя или user id. */
function memberLabel(member: CampaignMember | undefined): string | undefined {
  if (!member) return undefined;
  if (member.username) return `@${member.username}`;
  return member.name ?? member.userId;
}

function partyState(campaign: Campaign, characters: CharacterSheet[]): string {
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
      const owner = campaign.members.find((member) => member.userId === sheet.ownerUserId);
      const ownerLabel = memberLabel(owner);
      if (ownerLabel) parts.push(`играет ${ownerLabel}`);
      return `- ${sheet.name} (${parts.join("; ")})`;
    })
    .join("\n");
}

/**
 * Кто пишет сейчас и кому принадлежат персонажи — якорь агентности:
 * модель не должна говорить и решать за чужих персонажей. Только для групп.
 */
function speakerSection(
  identity: CallerIdentity,
  characters: CharacterSheet[],
): string | undefined {
  if (!identity.chatType || identity.chatType === "private") return undefined;
  const speaker = identity.username ? `@${identity.username}` : identity.userId;
  const own = characters.filter((sheet) => sheet.ownerUserId === identity.userId).map((sheet) => sheet.name);
  const others = characters.filter((sheet) => sheet.ownerUserId !== identity.userId).map((sheet) => sheet.name);
  const lines = [`Сейчас пишет: ${speaker}`];
  if (own.length > 0) {
    lines.push(`Персонаж этого игрока: ${own.join(", ")} — он управляет только ${own.length > 1 ? "ими" : "им"}.`);
  }
  if (others.length > 0) {
    lines.push(`Не пиши, не действуй и не решай за: ${others.join(", ")} — это персонажи других игроков.`);
  }
  return lines.join("\n");
}

function buildMemoryBlock(campaign: Campaign, identity: CallerIdentity): string {
  const slug = campaign.slug;
  const day = campaign.currentDay ?? 1;
  const characters = campaignStore.listCharacters(campaign.id);

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

  const speaker = speakerSection(identity, characters);
  if (speaker) sections.push(speaker);

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
  sections.push("### Партия\n" + partyState(campaign, characters));

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
        if (!identity || !campaign) return null;
        return defineInstructions({ markdown: buildMemoryBlock(campaign, identity) });
      } catch {
        // Память — дополнение к игре: при сбое чтения ход продолжается без неё.
        return null;
      }
    },
  },
});
