/**
 * Хранилище кампаний на SQLite: реализация того же интерфейса CampaignStore,
 * что и MarkdownCampaignStore. Основной стор (один БД-файл вместо дерева
 * MD-файлов, индексный поиск по привязанному чату); MD-стор сохранён как
 * откат (CAMPAIGN_STORE=markdown) и как источник для миграции
 * (scripts/migrate-md-to-sqlite.ts).
 *
 * Транскрипты, саммари и NPC остаются в MD-файлах (journal.ts, npc.ts):
 * они вне интерфейса CampaignStore и используют slug кампании для путей.
 */
import { mkdirSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import { randomUUID } from "node:crypto";

import type BetterSqlite3 from "better-sqlite3";

/**
 * better-sqlite3 — нативный аддон: бандлер eve не может корректно собрать
 * .node-бинарник (ищет его относительно собственного выходного каталога).
 * Поэтому пакет не импортируется статически, а грузится в рантайме через
 * require из node_modules проекта. type-import выше на рантайм не влияет.
 */
const projectRequire = createRequire(resolve(process.cwd(), "package.json"));
let databaseCtor: typeof BetterSqlite3 | undefined;
function betterSqlite(): typeof BetterSqlite3 {
  if (!databaseCtor) {
    databaseCtor = projectRequire("better-sqlite3") as typeof BetterSqlite3;
  }
  return databaseCtor;
}

import { slugify } from "./store.ts";
import {
  MAX_PARTY,
  StoreError,
  type BoundChat,
  type Campaign,
  type CampaignLength,
  type CampaignMember,
  type CharacterAbility,
  type CharacterGrantPatch,
  type CharacterSheet,
  type CharacterStatePatch,
  type MemberRole,
  type NewQuestInput,
  type NewThreadInput,
  type OpenThread,
  type Quest,
  type QuestDifficulty,
  type QuestPatch,
  type QuestRewardPlan,
  type QuestStatus,
  type ThreadKind,
} from "./types.ts";
import type {
  CampaignStore,
  NewCampaignInput,
  NewCharacterInput,
  NewMemberInput,
  NewOwnerInput,
} from "./store.ts";

interface CampaignRow {
  id: string;
  slug: string;
  title: string;
  status: string;
  owner_user_id: string;
  length: string;
  setting: string;
  theme: string;
  goal: string | null;
  tone: string | null;
  opening_scene: string | null;
  description: string;
  bound_chat_id: string | null;
  bound_thread_id: number | null;
  current_day: number | null;
  created_at: string;
}

interface MemberRow {
  user_id: string;
  name: string | null;
  username: string | null;
  role: string;
}

interface CharacterRow {
  id: string;
  campaign_id: string;
  slug: string;
  name: string;
  owner_user_id: string;
  class: string;
  race: string;
  level: number;
  stats: string;
  background: string | null;
  motivation: string | null;
  appearance: string | null;
  hp: number | null;
  max_hp: number | null;
  conditions: string | null;
  inventory: string | null;
  abilities: string | null;
  gold: number | null;
  xp: number | null;
  location: string | null;
  updated_at: string | null;
  created_at: string;
}

interface QuestRow {
  id: string;
  campaign_id: string;
  slug: string;
  title: string;
  giver_npc_slug: string | null;
  objective: string;
  difficulty: string;
  reward_plan: string | null;
  status: string;
  deadline_day: number | null;
  created_day: number;
  created_at: string;
  updated_at: string | null;
}

interface ThreadRow {
  id: string;
  campaign_id: string;
  text: string;
  kind: string;
  status: string;
  linked_quest_id: string | null;
  day_opened: number;
  day_closed: number | null;
  created_at: string;
  updated_at: string | null;
}

const SCHEMA = `
CREATE TABLE IF NOT EXISTS campaigns (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'setup',
  owner_user_id TEXT NOT NULL,
  length TEXT NOT NULL DEFAULT 'medium',
  setting TEXT NOT NULL DEFAULT '',
  theme TEXT NOT NULL DEFAULT '',
  goal TEXT,
  tone TEXT,
  opening_scene TEXT,
  description TEXT NOT NULL DEFAULT '',
  bound_chat_id TEXT,
  bound_thread_id INTEGER,
  current_day INTEGER,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_campaigns_bound_chat
  ON campaigns (bound_chat_id, bound_thread_id);

CREATE TABLE IF NOT EXISTS campaign_members (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  name TEXT,
  username TEXT,
  role TEXT NOT NULL DEFAULT 'player',
  PRIMARY KEY (campaign_id, user_id)
);

CREATE TABLE IF NOT EXISTS characters (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  owner_user_id TEXT NOT NULL,
  class TEXT NOT NULL,
  race TEXT NOT NULL,
  level INTEGER NOT NULL DEFAULT 1,
  stats TEXT NOT NULL DEFAULT '{}',
  background TEXT,
  motivation TEXT,
  appearance TEXT,
  hp INTEGER,
  max_hp INTEGER,
  conditions TEXT,
  inventory TEXT,
  abilities TEXT,
  gold INTEGER,
  xp INTEGER,
  location TEXT,
  updated_at TEXT,
  created_at TEXT NOT NULL,
  UNIQUE (campaign_id, slug)
);

CREATE TABLE IF NOT EXISTS quests (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  title TEXT NOT NULL,
  giver_npc_slug TEXT,
  objective TEXT NOT NULL,
  difficulty TEXT NOT NULL DEFAULT 'medium',
  reward_plan TEXT,
  status TEXT NOT NULL DEFAULT 'offered',
  deadline_day INTEGER,
  created_day INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
);
CREATE INDEX IF NOT EXISTS idx_quests_status
  ON quests (campaign_id, status);

CREATE TABLE IF NOT EXISTS open_threads (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  text TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'unresolved',
  status TEXT NOT NULL DEFAULT 'open',
  linked_quest_id TEXT,
  day_opened INTEGER NOT NULL DEFAULT 1,
  day_closed INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_open_threads_status
  ON open_threads (campaign_id, status);
`;

function parseJsonRecord(value: string | null): Record<string, number> {
  if (!value) return {};
  try {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    const result: Record<string, number> = {};
    for (const [key, item] of Object.entries(parsed as Record<string, unknown>)) {
      if (typeof item === "number") result[key] = item;
    }
    return result;
  } catch {
    return {};
  }
}

function parseStringArray(value: string | null): string[] | undefined {
  if (!value) return undefined;
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed)) return undefined;
    return parsed.map((item) => String(item));
  } catch {
    return undefined;
  }
}

function parseAbilityArray(value: string | null): CharacterAbility[] | undefined {
  if (!value) return undefined;
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed)) return undefined;
    const abilities: CharacterAbility[] = [];
    for (const item of parsed) {
      if (!item || typeof item !== "object") continue;
      const record = item as Record<string, unknown>;
      const name = typeof record.name === "string" ? record.name : "";
      const description = typeof record.description === "string" ? record.description : "";
      if (!name || !description) continue;
      const ability: CharacterAbility = { name, description };
      if (typeof record.level === "number") ability.level = record.level;
      abilities.push(ability);
    }
    return abilities;
  } catch {
    return undefined;
  }
}

function parseQuestDifficulty(value: string): QuestDifficulty {
  return value === "easy" || value === "hard" ? value : "medium";
}

function parseQuestStatus(value: string): QuestStatus {
  const statuses: QuestStatus[] = ["offered", "accepted", "active", "completed", "failed", "abandoned"];
  return statuses.includes(value as QuestStatus) ? (value as QuestStatus) : "offered";
}

function parseThreadKind(value: string): ThreadKind {
  const kinds: ThreadKind[] = ["promise", "mystery", "debt", "unresolved"];
  return kinds.includes(value as ThreadKind) ? (value as ThreadKind) : "unresolved";
}

function parseRewardPlan(value: string): QuestRewardPlan | undefined {
  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    const plan: QuestRewardPlan = {};
    if (typeof parsed.xp === "number") plan.xp = parsed.xp;
    if (typeof parsed.gold === "number") plan.gold = parsed.gold;
    if (Array.isArray(parsed.items)) plan.items = parsed.items.map((item) => String(item)).filter(Boolean);
    if (typeof parsed.note === "string" && parsed.note.trim() !== "") plan.note = parsed.note;
    return Object.keys(plan).length > 0 ? plan : undefined;
  } catch {
    return undefined;
  }
}

export class SqliteCampaignStore implements CampaignStore {
  private readonly db: BetterSqlite3.Database;

  constructor(dbPath: string) {
    mkdirSync(dirname(dbPath), { recursive: true });
    this.db = new (betterSqlite())(dbPath);
    this.db.pragma("journal_mode = WAL");
    this.db.pragma("foreign_keys = ON");
    this.db.exec(SCHEMA);
    this.migrate();
  }

  /** Лёгкие миграции для БД, созданных до добавления новых колонок. */
  private migrate(): void {
    const columns = this.db.prepare("PRAGMA table_info(characters)").all() as { name: string }[];
    if (!columns.some((column) => column.name === "abilities")) {
      this.db.exec("ALTER TABLE characters ADD COLUMN abilities TEXT");
    }
  }

  createCampaign(input: NewCampaignInput, owner: NewOwnerInput): Campaign {
    const slug = this.uniqueCampaignSlug(slugify(input.title));
    const campaign: Campaign = {
      id: randomUUID(),
      title: input.title,
      slug,
      status: "setup",
      ownerUserId: owner.userId,
      length: input.length,
      setting: input.setting,
      theme: input.theme,
      goal: input.goal,
      tone: input.tone,
      openingScene: input.openingScene,
      members: [{ ...owner, role: "dm" }],
      createdAt: new Date().toISOString(),
    };
    this.insertCampaign(campaign, input.description ?? "");
    return campaign;
  }

  getCampaign(idOrSlug: string): Campaign | undefined {
    const row = this.db
      .prepare("SELECT * FROM campaigns WHERE id = ? OR slug = ? LIMIT 1")
      .get(idOrSlug, idOrSlug) as CampaignRow | undefined;
    return row ? this.campaignFromRow(row) : undefined;
  }

  listCampaigns(): Campaign[] {
    const rows = this.db
      .prepare("SELECT * FROM campaigns ORDER BY created_at DESC")
      .all() as CampaignRow[];
    return rows.map((row) => this.campaignFromRow(row));
  }

  listForUser(userId: string): Campaign[] {
    return this.listCampaigns().filter((campaign) =>
      campaign.members.some((member) => member.userId === userId),
    );
  }

  findByBoundChat(chatId: string, messageThreadId?: number, options?: { anyStatus?: boolean }): Campaign | undefined {
    // bound_thread_id IS ? — null-safe сравнение: кампания без топика
    // совпадает только с запросом без топика (как в MarkdownCampaignStore).
    const statusClause = options?.anyStatus === true ? "" : "AND status = 'active'";
    const row = this.db
      .prepare(`SELECT * FROM campaigns WHERE bound_chat_id = ? AND bound_thread_id IS ? ${statusClause} LIMIT 1`)
      .get(chatId, messageThreadId ?? null) as CampaignRow | undefined;
    return row ? this.campaignFromRow(row) : undefined;
  }

  bindAndActivate(campaignId: string, actorUserId: string, chat: BoundChat): Campaign {
    const campaign = this.mustGetCampaign(campaignId);
    this.requireRole(campaign, actorUserId, "dm");
    if (campaign.status === "active") {
      throw new StoreError(`Кампания «${campaign.title}» уже запущена.`, "conflict");
    }
    const existing = this.findByBoundChat(chat.chatId, chat.messageThreadId);
    if (existing && existing.id !== campaign.id) {
      throw new StoreError(
        `В этом чате уже идёт кампания «${existing.title}». Один чат/топик — одна активная кампания.`,
        "conflict",
      );
    }
    this.db
      .prepare(
        `UPDATE campaigns
         SET status = 'active', bound_chat_id = ?, bound_thread_id = ?, current_day = COALESCE(current_day, 1)
         WHERE id = ?`,
      )
      .run(chat.chatId, chat.messageThreadId ?? null, campaign.id);
    return this.mustGetCampaign(campaignId);
  }

  advanceDay(campaignId: string, actorUserId: string): Campaign {
    const campaign = this.mustGetCampaign(campaignId);
    this.requireRole(campaign, actorUserId, "dm");
    if (campaign.status !== "active") {
      throw new StoreError("Игровые дни можно двигать только в активной кампании.", "conflict");
    }
    this.db
      .prepare("UPDATE campaigns SET current_day = COALESCE(current_day, 1) + 1 WHERE id = ?")
      .run(campaign.id);
    return this.mustGetCampaign(campaignId);
  }

  addMember(campaignId: string, inviterUserId: string, member: NewMemberInput): Campaign {
    const campaign = this.mustGetCampaign(campaignId);
    this.requireRole(campaign, inviterUserId, "dm");
    const role = member.role ?? "player";
    if (role === "dm" && inviterUserId !== campaign.ownerUserId) {
      throw new StoreError(
        "Назначать администраторов (dm) может только владелец кампании.",
        "access_denied",
      );
    }
    if (campaign.members.some((existing) => existing.userId === member.userId)) {
      throw new StoreError("Этот пользователь уже участник кампании.", "duplicate");
    }
    this.insertMember(campaign.id, { ...member, role });
    return this.mustGetCampaign(campaignId);
  }

  /**
   * Вступление без проверки роли DM (используется явной командой /join):
   * идемпотентно, запрошенная роль dm принудительно снижается до player,
   * при полной партии (MAX_PARTY игроков) новый участник молча пропускается.
   */
  autoRegister(campaignId: string, user: NewMemberInput): Campaign {
    const campaign = this.mustGetCampaign(campaignId);
    if (campaign.members.some((existing) => existing.userId === user.userId)) {
      return campaign;
    }
    const players = campaign.members.filter((member) => member.role === "player").length;
    if (players >= MAX_PARTY) return campaign;
    this.insertMember(campaign.id, { ...user, role: "player" });
    return this.mustGetCampaign(campaignId);
  }

  saveCharacter(campaignId: string, actorUserId: string, input: NewCharacterInput): CharacterSheet {
    const campaign = this.mustGetCampaign(campaignId);
    if (!campaign.members.some((member) => member.userId === actorUserId)) {
      throw new StoreError(
        "Персонажа может создать только участник кампании. Сначала нужно вступить в кампанию (команда /join или приглашение от DM).",
        "access_denied",
      );
    }
    const characters = this.listCharacters(campaign.id);
    if (characters.length >= MAX_PARTY) {
      throw new StoreError(`Партия заполнена (максимум ${MAX_PARTY} персонажей).`, "party_full");
    }
    if (characters.some((sheet) => sheet.name.toLowerCase() === input.name.toLowerCase())) {
      throw new StoreError(`Персонаж с именем ${input.name} уже есть в кампании.`, "duplicate");
    }
    const slug = this.uniqueCharacterSlug(campaign.id, slugify(input.name));
    const sheet: CharacterSheet = {
      id: randomUUID(),
      campaignId: campaign.id,
      name: input.name,
      slug,
      ownerUserId: actorUserId,
      characterClass: input.characterClass,
      race: input.race,
      level: input.level ?? 1,
      stats: input.stats ?? {},
      background: input.background,
      motivation: input.motivation,
      appearance: input.appearance,
      maxHp: input.maxHp,
      hp: input.hp ?? input.maxHp,
      inventory: input.equipment && input.equipment.length > 0 ? input.equipment : undefined,
      abilities: input.abilities && input.abilities.length > 0 ? input.abilities : undefined,
      gold: input.gold,
      createdAt: new Date().toISOString(),
    };
    this.insertCharacter(sheet);
    return sheet;
  }

  updateCharacter(campaignIdOrSlug: string, nameOrSlug: string, patch: CharacterStatePatch): CharacterSheet {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    const needle = nameOrSlug.toLowerCase();
    const row = this.listCharacterRows(campaign.id).find(
      (candidate) =>
        candidate.id === nameOrSlug ||
        candidate.slug.toLowerCase() === needle ||
        candidate.name.toLowerCase() === needle,
    );
    if (!row) {
      throw new StoreError(`Персонаж «${nameOrSlug}» не найден в кампании.`, "not_found");
    }
    const updatedAt = new Date().toISOString();
    this.db
      .prepare(
        `UPDATE characters SET
           level = ?, hp = ?, max_hp = ?, conditions = ?, inventory = ?,
           abilities = ?, gold = ?, xp = ?, location = ?, updated_at = ?
         WHERE id = ?`,
      )
      .run(
        patch.level ?? row.level,
        patch.hp ?? row.hp,
        patch.maxHp ?? row.max_hp,
        patch.conditions !== undefined ? JSON.stringify(patch.conditions) : row.conditions,
        patch.inventory !== undefined ? JSON.stringify(patch.inventory) : row.inventory,
        patch.abilities !== undefined ? JSON.stringify(patch.abilities) : row.abilities,
        patch.gold ?? row.gold,
        patch.xp ?? row.xp,
        patch.location ?? row.location,
        updatedAt,
        row.id,
      );
    return this.mustGetCharacter(row.id);
  }

  grantCharacter(campaignIdOrSlug: string, nameOrSlug: string, patch: CharacterGrantPatch): CharacterSheet {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    const needle = nameOrSlug.toLowerCase();
    const row = this.listCharacterRows(campaign.id).find(
      (candidate) =>
        candidate.id === nameOrSlug ||
        candidate.slug.toLowerCase() === needle ||
        candidate.name.toLowerCase() === needle,
    );
    if (!row) {
      throw new StoreError(`Персонаж «${nameOrSlug}» не найден в кампании.`, "not_found");
    }
    const inventory = parseStringArray(row.inventory) ?? [];
    const abilities = parseAbilityArray(row.abilities) ?? [];
    const conditions = parseStringArray(row.conditions) ?? [];
    if (patch.inventory) inventory.push(...patch.inventory);
    if (patch.abilities) {
      const known = new Set(abilities.map((ability) => ability.name.toLowerCase()));
      for (const ability of patch.abilities) {
        if (!known.has(ability.name.toLowerCase())) {
          abilities.push(ability);
          known.add(ability.name.toLowerCase());
        }
      }
    }
    if (patch.conditions) conditions.push(...patch.conditions.filter((entry) => !conditions.includes(entry)));
    const updatedAt = new Date().toISOString();
    this.db
      .prepare(
        `UPDATE characters SET
           inventory = ?, abilities = ?, conditions = ?, gold = ?, xp = ?, updated_at = ?
         WHERE id = ?`,
      )
      .run(
        JSON.stringify(inventory),
        JSON.stringify(abilities),
        JSON.stringify(conditions),
        (row.gold ?? 0) + (patch.gold ?? 0),
        (row.xp ?? 0) + (patch.xp ?? 0),
        updatedAt,
        row.id,
      );
    return this.mustGetCharacter(row.id);
  }

  finishCampaign(campaignId: string, actorUserId: string): Campaign {
    const campaign = this.mustGetCampaign(campaignId);
    this.requireRole(campaign, actorUserId, "dm");
    if (campaign.status !== "active" && campaign.status !== "setup") {
      throw new StoreError(`Кампания «${campaign.title}» уже завершена.`, "conflict");
    }
    this.db
      .prepare("UPDATE campaigns SET status = 'finished', bound_chat_id = NULL, bound_thread_id = NULL WHERE id = ?")
      .run(campaign.id);
    return this.mustGetCampaign(campaignId);
  }

  createQuest(campaignIdOrSlug: string, input: NewQuestInput): Quest {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    if (this.listQuests(campaign.id).some((quest) => quest.title.toLowerCase() === input.title.toLowerCase())) {
      throw new StoreError(`Квест «${input.title}» уже есть в кампании.`, "duplicate");
    }
    const now = new Date().toISOString();
    const quest: Quest = {
      id: randomUUID(),
      campaignId: campaign.id,
      slug: this.uniqueQuestSlug(campaign.id, slugify(input.title)),
      title: input.title,
      giverNpcSlug: input.giverNpcSlug,
      objective: input.objective,
      difficulty: input.difficulty,
      rewardPlan: input.rewardPlan,
      status: input.status ?? "offered",
      deadlineDay: input.deadlineDay,
      createdDay: campaign.currentDay ?? 1,
      createdAt: now,
    };
    this.insertQuest(quest);
    return quest;
  }

  updateQuest(campaignIdOrSlug: string, questIdOrSlug: string, patch: QuestPatch): Quest {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    const row = this.findQuestRow(campaign.id, questIdOrSlug);
    const quest = this.questFromRow(row);
    if (patch.status === "completed") {
      throw new StoreError(
        `Квест «${quest.title}» нельзя перевести в completed через update_quest — используй complete_quest: он выдаст награды.`,
        "conflict",
      );
    }
    if (patch.title !== undefined && patch.title.toLowerCase() !== quest.title.toLowerCase()) {
      if (this.listQuests(campaign.id).some((other) => other.id !== quest.id && other.title.toLowerCase() === patch.title!.toLowerCase())) {
        throw new StoreError(`Квест с названием «${patch.title}» уже есть в кампании.`, "duplicate");
      }
    }
    const updated: Quest = { ...quest };
    if (patch.title !== undefined) updated.title = patch.title;
    if (patch.giverNpcSlug !== undefined) updated.giverNpcSlug = patch.giverNpcSlug;
    if (patch.objective !== undefined) updated.objective = patch.objective;
    if (patch.difficulty !== undefined) updated.difficulty = patch.difficulty;
    if (patch.rewardPlan !== undefined) updated.rewardPlan = patch.rewardPlan;
    if (patch.status !== undefined) updated.status = patch.status;
    if (patch.deadlineDay !== undefined) updated.deadlineDay = patch.deadlineDay;
    updated.updatedAt = new Date().toISOString();
    this.db
      .prepare(
        `UPDATE quests SET
           title = ?, giver_npc_slug = ?, objective = ?, difficulty = ?,
           reward_plan = ?, status = ?, deadline_day = ?, updated_at = ?
         WHERE id = ?`,
      )
      .run(
        updated.title,
        updated.giverNpcSlug ?? null,
        updated.objective,
        updated.difficulty,
        updated.rewardPlan !== undefined ? JSON.stringify(updated.rewardPlan) : row.reward_plan,
        updated.status,
        updated.deadlineDay ?? null,
        updated.updatedAt,
        row.id,
      );
    return this.mustGetQuest(row.id);
  }

  listQuests(campaignId: string): Quest[] {
    const rows = this.db
      .prepare("SELECT * FROM quests WHERE campaign_id = ? ORDER BY created_at DESC")
      .all(campaignId) as QuestRow[];
    return rows.map((row) => this.questFromRow(row));
  }

  appendThread(campaignIdOrSlug: string, input: NewThreadInput): OpenThread {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    const now = new Date().toISOString();
    const thread: OpenThread = {
      id: randomUUID(),
      campaignId: campaign.id,
      text: input.text.trim(),
      kind: input.kind ?? "unresolved",
      status: "open",
      linkedQuestId: input.linkedQuestId,
      dayOpened: campaign.currentDay ?? 1,
      createdAt: now,
    };
    this.db
      .prepare(
        `INSERT INTO open_threads (
           id, campaign_id, text, kind, status, linked_quest_id, day_opened, created_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .run(
        thread.id,
        thread.campaignId,
        thread.text,
        thread.kind,
        thread.status,
        thread.linkedQuestId ?? null,
        thread.dayOpened,
        thread.createdAt,
      );
    return thread;
  }

  resolveThread(campaignIdOrSlug: string, threadIdOrText: string, day?: number): OpenThread {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    const needle = threadIdOrText.toLowerCase();
    const rows = this.db
      .prepare("SELECT * FROM open_threads WHERE campaign_id = ? AND status = 'open'")
      .all(campaign.id) as ThreadRow[];
    const open = rows.filter(
      (candidate) => candidate.id === threadIdOrText || candidate.text.toLowerCase().includes(needle),
    );
    if (open.length === 0) {
      throw new StoreError(`Открытая нить «${threadIdOrText}» не найдена.`, "not_found");
    }
    if (open.length > 1) {
      throw new StoreError(
        `Фрагмент «${threadIdOrText}» подходит к нескольким нитям: ${open.map((row) => row.text).join("; ")}. Уточни текст или передай id.`,
        "conflict",
      );
    }
    const row = open[0];
    this.db
      .prepare("UPDATE open_threads SET status = 'resolved', day_closed = ?, updated_at = ? WHERE id = ?")
      .run(day ?? campaign.currentDay ?? 1, new Date().toISOString(), row.id);
    return this.mustGetThread(row.id);
  }

  listThreads(campaignId: string): OpenThread[] {
    const rows = this.db
      .prepare("SELECT * FROM open_threads WHERE campaign_id = ? ORDER BY created_at")
      .all(campaignId) as ThreadRow[];
    return rows.map((row) => this.threadFromRow(row));
  }

  listCharacters(campaignId: string): CharacterSheet[] {
    return this.listCharacterRows(campaignId).map((row) => this.sheetFromRow(row));
  }

  /** Описание кампании (тело campaign.md в MD-сторе) — для полноты миграции. */
  readDescription(campaignIdOrSlug: string): string {
    const row = this.db
      .prepare("SELECT description FROM campaigns WHERE id = ? OR slug = ? LIMIT 1")
      .get(campaignIdOrSlug, campaignIdOrSlug) as { description: string } | undefined;
    return row?.description ?? "";
  }

  // --- Upsert'ы для миграции из MD (scripts/migrate-md-to-sqlite.ts) ---

  upsertCampaign(campaign: Campaign, description: string): void {
    this.db
      .prepare(
        `INSERT INTO campaigns (
           id, slug, title, status, owner_user_id, length, setting, theme, goal, tone,
           opening_scene, description, bound_chat_id, bound_thread_id, current_day, created_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT(id) DO UPDATE SET
           slug = excluded.slug, title = excluded.title, status = excluded.status,
           owner_user_id = excluded.owner_user_id, length = excluded.length,
           setting = excluded.setting, theme = excluded.theme, goal = excluded.goal,
           tone = excluded.tone, opening_scene = excluded.opening_scene,
           description = excluded.description, bound_chat_id = excluded.bound_chat_id,
           bound_thread_id = excluded.bound_thread_id, current_day = excluded.current_day,
           created_at = excluded.created_at`,
      )
      .run(
        campaign.id,
        campaign.slug,
        campaign.title,
        campaign.status,
        campaign.ownerUserId,
        campaign.length,
        campaign.setting,
        campaign.theme,
        campaign.goal ?? null,
        campaign.tone ?? null,
        campaign.openingScene ?? null,
        description,
        campaign.boundChat?.chatId ?? null,
        campaign.boundChat?.messageThreadId ?? null,
        campaign.currentDay ?? null,
        campaign.createdAt,
      );
    this.db.prepare("DELETE FROM campaign_members WHERE campaign_id = ?").run(campaign.id);
    for (const member of campaign.members) {
      this.insertMember(campaign.id, member);
    }
  }

  upsertCharacter(sheet: CharacterSheet): void {
    this.insertCharacter(sheet, { upsert: true });
  }

  close(): void {
    this.db.close();
  }

  // --- Внутренние помощники ---

  private mustGetCampaign(idOrSlug: string): Campaign {
    const campaign = this.getCampaign(idOrSlug);
    if (!campaign) {
      throw new StoreError(`Кампания «${idOrSlug}» не найдена.`, "not_found");
    }
    return campaign;
  }

  private mustGetCharacter(characterId: string): CharacterSheet {
    const row = this.db
      .prepare("SELECT * FROM characters WHERE id = ?")
      .get(characterId) as CharacterRow | undefined;
    if (!row) {
      throw new StoreError(`Персонаж «${characterId}» не найден в кампании.`, "not_found");
    }
    return this.sheetFromRow(row);
  }

  private requireRole(campaign: Campaign, userId: string, role: MemberRole): void {
    const member = campaign.members.find((entry) => entry.userId === userId);
    if (!member || member.role !== role) {
      throw new StoreError(
        "Это действие доступно только администратору (dm) кампании.",
        "access_denied",
      );
    }
  }

  private campaignFromRow(row: CampaignRow): Campaign {
    const memberRows = this.db
      .prepare("SELECT user_id, name, username, role FROM campaign_members WHERE campaign_id = ? ORDER BY rowid")
      .all(row.id) as MemberRow[];
    return {
      id: row.id,
      title: row.title,
      slug: row.slug,
      status: (row.status as Campaign["status"]) ?? "setup",
      ownerUserId: row.owner_user_id,
      length: (row.length as CampaignLength) ?? "medium",
      setting: row.setting,
      theme: row.theme,
      goal: row.goal ?? undefined,
      tone: row.tone ?? undefined,
      openingScene: row.opening_scene ?? undefined,
      boundChat: row.bound_chat_id
        ? { chatId: row.bound_chat_id, messageThreadId: row.bound_thread_id ?? undefined }
        : undefined,
      currentDay: row.current_day ?? undefined,
      members: memberRows.map((member) => ({
        userId: member.user_id,
        name: member.name ?? undefined,
        username: member.username ?? undefined,
        role: (member.role === "dm" ? "dm" : "player") as MemberRole,
      })),
      createdAt: row.created_at,
    };
  }

  private sheetFromRow(row: CharacterRow): CharacterSheet {
    return {
      id: row.id,
      campaignId: row.campaign_id,
      name: row.name,
      slug: row.slug,
      ownerUserId: row.owner_user_id,
      characterClass: row.class,
      race: row.race,
      level: row.level,
      stats: parseJsonRecord(row.stats),
      background: row.background ?? undefined,
      motivation: row.motivation ?? undefined,
      appearance: row.appearance ?? undefined,
      hp: row.hp ?? undefined,
      maxHp: row.max_hp ?? undefined,
      conditions: parseStringArray(row.conditions),
      inventory: parseStringArray(row.inventory),
      abilities: parseAbilityArray(row.abilities),
      gold: row.gold ?? undefined,
      xp: row.xp ?? undefined,
      location: row.location ?? undefined,
      updatedAt: row.updated_at ?? undefined,
      createdAt: row.created_at,
    };
  }

  private questFromRow(row: QuestRow): Quest {
    return {
      id: row.id,
      campaignId: row.campaign_id,
      slug: row.slug,
      title: row.title,
      giverNpcSlug: row.giver_npc_slug ?? undefined,
      objective: row.objective,
      difficulty: parseQuestDifficulty(row.difficulty),
      rewardPlan: row.reward_plan ? parseRewardPlan(row.reward_plan) : undefined,
      status: parseQuestStatus(row.status),
      deadlineDay: row.deadline_day ?? undefined,
      createdDay: row.created_day,
      createdAt: row.created_at,
      updatedAt: row.updated_at ?? undefined,
    };
  }

  private threadFromRow(row: ThreadRow): OpenThread {
    return {
      id: row.id,
      campaignId: row.campaign_id,
      text: row.text,
      kind: parseThreadKind(row.kind),
      status: row.status === "resolved" ? "resolved" : "open",
      linkedQuestId: row.linked_quest_id ?? undefined,
      dayOpened: row.day_opened,
      dayClosed: row.day_closed ?? undefined,
      createdAt: row.created_at,
      updatedAt: row.updated_at ?? undefined,
    };
  }

  private mustGetQuest(questId: string): Quest {
    const row = this.db.prepare("SELECT * FROM quests WHERE id = ?").get(questId) as QuestRow | undefined;
    if (!row) {
      throw new StoreError(`Квест не найден (id «${questId}»).`, "not_found");
    }
    return this.questFromRow(row);
  }

  private mustGetThread(threadId: string): OpenThread {
    const row = this.db
      .prepare("SELECT * FROM open_threads WHERE id = ?")
      .get(threadId) as ThreadRow | undefined;
    if (!row) {
      throw new StoreError(`Нить не найдена (id «${threadId}»).`, "not_found");
    }
    return this.threadFromRow(row);
  }

  private findQuestRow(campaignId: string, questIdOrSlug: string): QuestRow {
    const needle = questIdOrSlug.toLowerCase();
    const rows = this.db
      .prepare("SELECT * FROM quests WHERE campaign_id = ?")
      .all(campaignId) as QuestRow[];
    const row = rows.find(
      (candidate) =>
        candidate.id === questIdOrSlug ||
        candidate.slug.toLowerCase() === needle ||
        candidate.title.toLowerCase() === needle,
    );
    if (!row) {
      throw new StoreError(`Квест «${questIdOrSlug}» не найден в кампании.`, "not_found");
    }
    return row;
  }

  private insertQuest(quest: Quest): void {
    this.db
      .prepare(
        `INSERT INTO quests (
           id, campaign_id, slug, title, giver_npc_slug, objective, difficulty,
           reward_plan, status, deadline_day, created_day, created_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .run(
        quest.id,
        quest.campaignId,
        quest.slug,
        quest.title,
        quest.giverNpcSlug ?? null,
        quest.objective,
        quest.difficulty,
        quest.rewardPlan !== undefined ? JSON.stringify(quest.rewardPlan) : null,
        quest.status,
        quest.deadlineDay ?? null,
        quest.createdDay,
        quest.createdAt,
      );
  }

  private uniqueQuestSlug(campaignId: string, base: string): string {
    const probe = this.db.prepare("SELECT 1 FROM quests WHERE campaign_id = ? AND slug = ?");
    let slug = base || "quest";
    let counter = 2;
    while (probe.get(campaignId, slug)) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private listCharacterRows(campaignId: string): CharacterRow[] {
    return this.db
      .prepare("SELECT * FROM characters WHERE campaign_id = ? ORDER BY created_at")
      .all(campaignId) as CharacterRow[];
  }

  private insertCampaign(campaign: Campaign, description: string): void {
    this.db
      .prepare(
        `INSERT INTO campaigns (
           id, slug, title, status, owner_user_id, length, setting, theme, goal, tone,
           opening_scene, description, bound_chat_id, bound_thread_id, current_day, created_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .run(
        campaign.id,
        campaign.slug,
        campaign.title,
        campaign.status,
        campaign.ownerUserId,
        campaign.length,
        campaign.setting,
        campaign.theme,
        campaign.goal ?? null,
        campaign.tone ?? null,
        campaign.openingScene ?? null,
        description,
        campaign.boundChat?.chatId ?? null,
        campaign.boundChat?.messageThreadId ?? null,
        campaign.currentDay ?? null,
        campaign.createdAt,
      );
    for (const member of campaign.members) {
      this.insertMember(campaign.id, member);
    }
  }

  private insertMember(campaignId: string, member: CampaignMember): void {
    this.db
      .prepare(
        "INSERT INTO campaign_members (campaign_id, user_id, name, username, role) VALUES (?, ?, ?, ?, ?)",
      )
      .run(campaignId, member.userId, member.name ?? null, member.username ?? null, member.role);
  }

  private insertCharacter(sheet: CharacterSheet, options?: { upsert?: boolean }): void {
    const conflict = options?.upsert === true
      ? `ON CONFLICT(id) DO UPDATE SET
           campaign_id = excluded.campaign_id, slug = excluded.slug, name = excluded.name,
           owner_user_id = excluded.owner_user_id, class = excluded.class, race = excluded.race,
           level = excluded.level, stats = excluded.stats, background = excluded.background,
           motivation = excluded.motivation, appearance = excluded.appearance, hp = excluded.hp,
           max_hp = excluded.max_hp, conditions = excluded.conditions, inventory = excluded.inventory,
           abilities = excluded.abilities, gold = excluded.gold, xp = excluded.xp,
           location = excluded.location, updated_at = excluded.updated_at, created_at = excluded.created_at`
      : "";
    this.db
      .prepare(
        `INSERT INTO characters (
           id, campaign_id, slug, name, owner_user_id, class, race, level, stats, background,
           motivation, appearance, hp, max_hp, conditions, inventory, abilities, gold, xp, location,
           updated_at, created_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ${conflict}`,
      )
      .run(
        sheet.id,
        sheet.campaignId,
        sheet.slug,
        sheet.name,
        sheet.ownerUserId,
        sheet.characterClass,
        sheet.race,
        sheet.level,
        JSON.stringify(sheet.stats),
        sheet.background ?? null,
        sheet.motivation ?? null,
        sheet.appearance ?? null,
        sheet.hp ?? null,
        sheet.maxHp ?? null,
        sheet.conditions !== undefined ? JSON.stringify(sheet.conditions) : null,
        sheet.inventory !== undefined ? JSON.stringify(sheet.inventory) : null,
        sheet.abilities !== undefined ? JSON.stringify(sheet.abilities) : null,
        sheet.gold ?? null,
        sheet.xp ?? null,
        sheet.location ?? null,
        sheet.updatedAt ?? null,
        sheet.createdAt,
      );
  }

  private uniqueCampaignSlug(base: string): string {
    const probe = this.db.prepare("SELECT 1 FROM campaigns WHERE slug = ?");
    let slug = base;
    let counter = 2;
    while (probe.get(slug)) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private uniqueCharacterSlug(campaignId: string, base: string): string {
    const probe = this.db.prepare("SELECT 1 FROM characters WHERE campaign_id = ? AND slug = ?");
    let slug = base;
    let counter = 2;
    while (probe.get(campaignId, slug)) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }
}
