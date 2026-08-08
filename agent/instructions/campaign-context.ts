/**
 * Динамическая подгрузка памяти кампании в контекст каждого хода.
 *
 * На turn.started резолвим кампанию по чату звонящего и собираем компактный
 * блок: мета кампании, хроника прошлых дней, ключевые события, саммари и
 * хвост транскрипта текущего дня, ростер NPC и состояние партии.
 * Полный транскрипт при этом не читается — только дайджесты и хвосты.
 */
import { defineDynamic, defineInstructions } from "eve/instructions";

import { findCampaignForIdentity } from "../lib/campaigns/access.ts";
import { factionStore } from "../lib/campaigns/factions.ts";
import {
  listDayHeadlines,
  readCampaignSummary,
  readDayTail,
  splitKeyEvents,
} from "../lib/campaigns/journal.ts";
import { locationStore } from "../lib/campaigns/locations.ts";
import { npcStore } from "../lib/campaigns/npc.ts";
import { resolveCallerIdentity, type CallerIdentity } from "../lib/campaigns/session.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import type { Campaign, CampaignMember, CharacterSheet, NpcProfile, TimeOfDay } from "../lib/campaigns/types.ts";
import { renderWorldState } from "../lib/campaigns/world-state.ts";

const SUMMARY_CAP = 2000;
const KEY_EVENTS_CAP = 2000;
/** Кап на компактную хронику (по дням). ~60 симв/день → 40 дней в <2.5k символов. */
const HEADLINES_CAP = 2500;
const DAY_TAIL_LINES = 30;
const NPC_ROSTER_CAP = 20;
const ACTIVE_QUESTS_CAP = 10;
const OPEN_THREADS_CAP = 10;
const WORLD_STATE_CAP = 1500;
const FACTION_ROSTER_CAP = 15;
const MAP_CONNECTIONS_CAP = 10;

const ACTIVE_QUEST_STATUSES = new Set(["offered", "accepted", "active"]);

const DIFFICULTY_LABEL: Record<string, string> = {
  easy: "лёгкий",
  medium: "средний",
  hard: "сложный",
};

function activeQuestsSection(campaign: Campaign): string {
  const quests = campaignStore
    .listQuests(campaign.id)
    .filter((quest) => ACTIVE_QUEST_STATUSES.has(quest.status))
    .slice(0, ACTIVE_QUESTS_CAP);
  if (quests.length === 0) return "### Активные квесты\n(нет активных квестов)";
  const lines = quests.map((quest) => {
    const parts = [
      DIFFICULTY_LABEL[quest.difficulty] ?? quest.difficulty,
      quest.giverNpcSlug ? `от: ${quest.giverNpcSlug}` : null,
      quest.deadlineDay ? `дедлайн: день ${quest.deadlineDay}` : null,
    ].filter(Boolean);
    return `- ${quest.status}: «${quest.title}» — ${quest.objective}${parts.length > 0 ? ` (${parts.join("; ")})` : ""}`;
  });
  return "### Активные квесты\n" + lines.join("\n");
}

const THREAD_KIND_LABEL: Record<string, string> = {
  promise: "обещание",
  mystery: "тайна",
  debt: "долг",
  unresolved: "незавершённое",
};

function openThreadsSection(campaign: Campaign): string {
  const threads = campaignStore
    .listThreads(campaign.id)
    .filter((thread) => thread.status === "open")
    .slice(0, OPEN_THREADS_CAP);
  if (threads.length === 0) return "### Открытые нити\n(нет)";
  const lines = threads.map((thread) => {
    const kind = THREAD_KIND_LABEL[thread.kind] ?? thread.kind;
    return `- [${kind}] ${thread.text} (открыта в день ${thread.dayOpened})`;
  });
  return "### Открытые нити\n" + lines.join("\n");
}

function capTail(text: string, max: number): string {
  if (text.length <= max) return text;
  return `…(начало опущено)\n${text.slice(-max)}`;
}

/** Компактная хроника: одна строка на каждый день (headline). Необрезаемая —
 *  видна вся арка кампании. Если дней слишком много, обрезаем хвост. */
function headlinesSection(slug: string): string | undefined {
  const headlines = listDayHeadlines(slug);
  if (headlines.length === 0) return undefined;
  const lines = headlines.map((h) => `- День ${h.day}: ${h.headline}`);
  const joined = lines.join("\n");
  if (joined.length <= HEADLINES_CAP) return `### Хроника (по дням)\n${joined}`;
  return `### Хроника (по дням)\n${capTail(joined, HEADLINES_CAP)}`;
}

/** Ключевые события: permanent (без обрезки) + обычные (хвост). */
function keyEventsSection(slug: string): string | undefined {
  const { permanent, regular } = splitKeyEvents(slug);
  if (permanent.length === 0 && regular.length === 0) return undefined;
  const parts: string[] = [];
  if (permanent.length > 0) {
    parts.push("Важное (всегда):");
    parts.push(...permanent);
  }
  if (regular.length > 0) {
    const tail = capTail(regular.join("\n"), KEY_EVENTS_CAP);
    parts.push("События:");
    parts.push(tail);
  }
  return `### Ключевые события\n${parts.join("\n")}`;
}

function npcRoster(npcs: NpcProfile[], campaignId: string): string {
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
    // Последняя строка памяти NPC — что он «помнит» о партии (подсказка для DM).
    const memory = npcStore.lastMemoryLine(campaignId, npc.slug);
    const memoryPart = memory ? `помнит: ${memory}` : null;
    return `- ${npc.name} — ${[...parts, memoryPart].filter(Boolean).join("; ")}`;
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

const TIME_OF_DAY_LABEL: Record<TimeOfDay, string> = {
  morning: "утро",
  day: "день",
  evening: "вечер",
  night: "ночь",
};

/** Строка игрового времени/окружения (C2) — в заголовок блока. */
function environmentLine(campaign: Campaign): string | undefined {
  const parts: string[] = [];
  if (campaign.timeOfDay) parts.push(TIME_OF_DAY_LABEL[campaign.timeOfDay]);
  const date = campaign.inGameDate ?? `день ${campaign.currentDay ?? 1}`;
  parts.push(date);
  if (campaign.weather) parts.push(campaign.weather);
  return parts.length > 0 ? `Игровое время: ${parts.join(", ")}` : undefined;
}

/** Секция карты (C1): текущая локация + куда можно пойти. */
function mapSection(campaign: Campaign): string | undefined {
  const current = locationStore.currentLocation(campaign.id);
  if (!current) return undefined;
  const lines = [`Сейчас: ${current.name}`];
  const connections = current.connections.slice(0, MAP_CONNECTIONS_CAP);
  if (connections.length > 0) {
    const conns = connections.map((conn) => {
      const via = conn.via ? ` через ${conn.via}` : "";
      return `${conn.to}${via}`;
    });
    lines.push(`Отсюда можно пройти: ${conns.join("; ")}`);
  }
  if (current.connections.length > MAP_CONNECTIONS_CAP) {
    lines.push(`…и ещё ${current.connections.length - MAP_CONNECTIONS_CAP} (list_locations)`);
  }
  return `### Карта\n${lines.join("\n")}`;
}

/** Секция состояния мира (C5): перезаписываемые актуальные факты. */
function worldStateSection(slug: string): string | undefined {
  const rendered = renderWorldState(slug);
  if (!rendered) return undefined;
  return `### Состояние мира\n${capTail(rendered, WORLD_STATE_CAP)}`;
}

/** Секция фракций (C4): список с текущим standing. */
function factionsSection(campaign: Campaign): string | undefined {
  const factions = factionStore.listFactions(campaign.id);
  if (factions.length === 0) return undefined;
  const lines = factions.slice(0, FACTION_ROSTER_CAP).map((faction) => {
    const standing = faction.standing > 0 ? `+${faction.standing}` : `${faction.standing}`;
    return `- ${faction.name} — репутация ${standing}`;
  });
  if (factions.length > FACTION_ROSTER_CAP) {
    lines.push(`- …и ещё ${factions.length - FACTION_ROSTER_CAP}`);
  }
  return `### Фракции\n${lines.join("\n")}`;
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
      environmentLine(campaign),
    ]
      .filter(Boolean)
      .join("; "),
  ];

  const speaker = speakerSection(identity, characters);
  if (speaker) sections.push(speaker);

  // Компактная хроника (одна строка на день) идёт первой — видна вся арка кампании.
  const headlines = headlinesSection(slug);
  if (headlines) sections.push(headlines);

  const summary = readCampaignSummary(slug);
  if (summary) {
    sections.push("### Хроника прошлых дней (детально)\n" + capTail(summary, SUMMARY_CAP));
  }

  const keyEvents = keyEventsSection(slug);
  if (keyEvents) sections.push(keyEvents);

  // C5: актуальное состояние мира (перезаписываемое) — грузится целиком, с капом.
  const worldState = worldStateSection(slug);
  if (worldState) sections.push(worldState);

  sections.push(activeQuestsSection(campaign));
  sections.push(openThreadsSection(campaign));

  // C4: фракции с текущим standing.
  const factions = factionsSection(campaign);
  if (factions) sections.push(factions);

  // C1: текущая локация и известные связи.
  const map = mapSection(campaign);
  if (map) sections.push(map);

  const currentDay = readDayTail(slug, day, DAY_TAIL_LINES);
  if (currentDay) {
    const dayParts = [`### Текущий день ${day}`];
    if (currentDay.headline) dayParts.push(`Шапка дня: ${currentDay.headline}`);
    if (currentDay.summary) dayParts.push(`Саммари дня: ${currentDay.summary}`);
    if (currentDay.entries.length > 0) {
      dayParts.push(`Последние события (последние ${currentDay.entries.length} записей транскрипта):`);
      dayParts.push(...currentDay.entries);
    }
    sections.push(dayParts.join("\n"));
  }

  sections.push("### NPC\n" + npcRoster(npcStore.listNpcs(campaign.id), campaign.id));
  sections.push("### Партия\n" + partyState(campaign, characters));

  sections.push(
    "Полный транскрипт дня — через read_day, карточка NPC — через get_npc. " +
      "Квесты и нити — через list_quests / list_open_threads. " +
      "Локации — list_locations, факты мира — list_world_state, " +
      "журнал лута — list_ledger. " +
      "Если нужен факт из прошлого, а день неизвестен — search_memory по ключевым словам. " +
      "Изменения состояния фиксируй через update_character / upsert_npc; новый день — advance_day; " +
      "время суток — advance_time; погода — set_weather; " +
      "перемещение партии — move_party; фракции — upsert_faction / adjust_standing.",
  );

  return sections.join("\n\n");
}

export default defineDynamic({
  events: {
    "turn.started": (_event, ctx) => {
      try {
        const identity = resolveCallerIdentity(ctx.session.auth.current);
        // findCampaignForIdentity делает точный матч чата/топика и откат к
        // кампании без топика: Telegram ставит message_thread_id любому
        // реплаю в супергруппе (= id сообщения, на которое ответили).
        const campaign = identity?.chatId ? findCampaignForIdentity(identity) : undefined;
        if (!identity || !campaign) return null;
        return defineInstructions({ markdown: buildMemoryBlock(campaign, identity) });
      } catch {
        // Память — дополнение к игре: при сбое чтения ход продолжается без неё.
        return null;
      }
    },
  },
});
