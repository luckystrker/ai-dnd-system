/**
 * Хранилище NPC кампании на SQLite: таблица npcs в базе кампаний
 * (campaigns.db). Профиль NPC (роль, статус, отношения) и его память —
 * что игроки с ним сделали и что он об этом знает — лежат в одной строке:
 * память в колонке memory (отрендеренные строки), отношения — JSON-колонкой.
 */
import { randomUUID } from "node:crypto";
import type BetterSqlite3 from "better-sqlite3";

import { openCampaignDb } from "./sqlite-db.ts";
import { campaignStore, slugify } from "./store.ts";
import { StoreError, type NpcProfile, type NpcRelationship, type NpcStatus } from "./types.ts";

/** Входные данные для создания/обновления NPC. */
export interface NpcUpsertInput {
  name: string;
  role?: string;
  status?: NpcStatus;
  location?: string;
  /** Отношения к персонажам: заменяет значения для указанных имён. */
  relationships?: Record<string, NpcRelationship>;
  firstSeenDay?: number;
  lastSeenDay?: number;
  /** Текст, который дописывается в память NPC (что произошло с NPC). */
  memoryAppend?: string;
  /** Игровой день для пометки memoryAppend. */
  memoryAppendDay?: number;
}

const SCHEMA = `
CREATE TABLE IF NOT EXISTS npcs (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  role TEXT,
  status TEXT NOT NULL DEFAULT 'alive',
  location TEXT,
  relationships TEXT NOT NULL DEFAULT '{}', -- JSON: имя персонажа -> {attitude, notes?}
  first_seen_day INTEGER,
  last_seen_day INTEGER,
  memory TEXT NOT NULL DEFAULT '',         -- отрендеренные строки памяти NPC
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
);
`;

interface NpcRow {
  id: string;
  campaign_id: string;
  slug: string;
  name: string;
  role: string | null;
  status: string;
  location: string | null;
  relationships: string;
  first_seen_day: number | null;
  last_seen_day: number | null;
  memory: string;
  created_at: string;
  updated_at: string | null;
}

// Ленивое открытие БД: handle и DDL создаются при первом обращении, а не
// в конструкторе и не на импорте модуля (eve-снапшот компиляции падает при
// открытии better-sqlite3 на этапе сборки тулов).
let dbHandle: BetterSqlite3.Database | undefined;
let schemaReady = false;
function db(): BetterSqlite3.Database {
  if (!dbHandle) dbHandle = openCampaignDb();
  if (!schemaReady) {
    dbHandle.exec(SCHEMA);
    schemaReady = true;
  }
  return dbHandle;
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : value === null || value === undefined ? "" : String(value);
}

/** Отношения как JSON-строка: имя персонажа -> {attitude, notes?}. */
function serializeRelationships(relationships: Record<string, NpcRelationship>): string {
  const record: Record<string, unknown> = {};
  for (const [name, relation] of Object.entries(relationships)) {
    record[name] = { attitude: relation.attitude, ...(relation.notes ? { notes: relation.notes } : {}) };
  }
  return JSON.stringify(record);
}

function deserializeRelationships(value: string): Record<string, NpcRelationship> {
  const relationships: Record<string, NpcRelationship> = {};
  try {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return relationships;
    for (const [name, item] of Object.entries(parsed as Record<string, unknown>)) {
      if (item && typeof item === "object") {
        const relation = item as Record<string, unknown>;
        relationships[name] = {
          attitude: typeof relation.attitude === "number" ? relation.attitude : 0,
          notes: relation.notes ? asString(relation.notes) : undefined,
        };
      }
    }
  } catch {
    // Повреждённый JSON игнорируем.
  }
  return relationships;
}

function statusOf(value: string): NpcStatus {
  return value === "dead" || value === "unknown" ? value : "alive";
}

function rowToNpc(row: NpcRow): NpcProfile {
  return {
    id: row.id,
    campaignId: row.campaign_id,
    name: row.name,
    slug: row.slug,
    role: row.role ?? undefined,
    status: statusOf(row.status),
    location: row.location ?? undefined,
    relationships: deserializeRelationships(row.relationships),
    firstSeenDay: row.first_seen_day ?? undefined,
    lastSeenDay: row.last_seen_day ?? undefined,
    createdAt: row.created_at,
    updatedAt: row.updated_at ?? undefined,
  };
}

export class SqliteNpcStore {
  /** Создаёт или обновляет NPC (поиск по имени без учёта регистра). */
  upsertNpc(campaignIdOrSlug: string, input: NpcUpsertInput): NpcProfile & { memory: string } {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const existing = this.findByName(campaign.id, input.name);
    const now = new Date().toISOString();
    const profile: NpcProfile = existing
      ? { ...existing }
      : {
          id: randomUUID(),
          campaignId: campaign.id,
          name: input.name,
          slug: this.uniqueSlug(campaign.id, slugify(input.name)),
          status: "alive",
          relationships: {},
          createdAt: now,
        };
    if (input.role !== undefined) profile.role = input.role;
    if (input.status !== undefined) profile.status = input.status;
    if (input.location !== undefined) profile.location = input.location;
    if (input.firstSeenDay !== undefined) profile.firstSeenDay = input.firstSeenDay;
    if (input.lastSeenDay !== undefined) profile.lastSeenDay = input.lastSeenDay;
    if (input.relationships) {
      for (const [name, relation] of Object.entries(input.relationships)) {
        profile.relationships[name] = relation;
      }
    }
    profile.updatedAt = now;

    let memory = existing ? existing.memory : "";
    const append = input.memoryAppend?.trim();
    if (append) {
      const dayMark = input.memoryAppendDay !== undefined ? ` [День ${input.memoryAppendDay}]` : "";
      const line = `-${dayMark} ${append.replace(/\s*\n\s*/g, " ")}`;
      memory = memory ? `${memory}\n${line}` : line;
    }
    this.writeNpc(profile, memory);
    return { ...profile, memory };
  }

  /** Полная карточка NPC вместе с памятью. */
  getNpc(campaignIdOrSlug: string, nameOrSlug: string): (NpcProfile & { memory: string }) | undefined {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const row = this.findRow(campaign.id, nameOrSlug);
    if (!row) return undefined;
    return { ...rowToNpc(row), memory: row.memory };
  }

  listNpcs(campaignIdOrSlug: string): NpcProfile[] {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const rows = db().prepare(
      "SELECT * FROM npcs WHERE campaign_id = ? ORDER BY created_at",
    ).all(campaign.id) as NpcRow[];
    return rows.map(rowToNpc);
  }

  // --- Внутренние помощники ---

  /**
   * Последняя осмысленная строка памяти NPC (для ростера в авто-блоке памяти),
   * обрезанная до maxChars. Память NPC в ростер раньше не попадала — только
   * метаданные; эта строка даёт DM подсказку, что NPC «помнит» о партии, без
   * необходимости звать get_npc для каждого. Пустая строка, если памяти нет.
   */
  lastMemoryLine(campaignIdOrSlug: string, npcSlug: string, maxChars = 100): string {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const needle = npcSlug.toLowerCase();
    const rows = db().prepare(
      "SELECT slug, memory FROM npcs WHERE campaign_id = ?",
    ).all(campaign.id) as { slug: string; memory: string }[];
    const row = rows.find((candidate) => candidate.slug.toLowerCase() === needle);
    const memory = row?.memory ?? "";
    const lines = memory
      .split("\n")
      .map((line) => line.trim())
      .filter((line) => line.startsWith("- ") || line.startsWith("* "));
    if (lines.length === 0) return "";
    const last = lines[lines.length - 1];
    const trimmed = last.length > maxChars ? `${last.slice(0, maxChars)}…` : last;
    return trimmed;
  }

  private findCampaign(campaignIdOrSlug: string): { id: string; slug: string } {
    const campaign = campaignStore.getCampaign(campaignIdOrSlug);
    if (!campaign) {
      throw new StoreError(`Кампания «${campaignIdOrSlug}» не найдена.`, "not_found");
    }
    return { id: campaign.id, slug: campaign.slug };
  }

  private findByName(campaignId: string, name: string): (NpcProfile & { memory: string }) | undefined {
    const needle = name.toLowerCase();
    const rows = db().prepare("SELECT * FROM npcs WHERE campaign_id = ?").all(campaignId) as NpcRow[];
    const row = rows.find((candidate) => candidate.name.toLowerCase() === needle);
    return row ? { ...rowToNpc(row), memory: row.memory } : undefined;
  }

  private findRow(campaignId: string, nameOrSlug: string): NpcRow | undefined {
    const needle = nameOrSlug.toLowerCase();
    const rows = db().prepare("SELECT * FROM npcs WHERE campaign_id = ?").all(campaignId) as NpcRow[];
    return rows.find(
      (candidate) =>
        candidate.id === nameOrSlug ||
        candidate.slug.toLowerCase() === needle ||
        candidate.name.toLowerCase() === needle,
    );
  }

  private uniqueSlug(campaignId: string, base: string): string {
    const probe = db().prepare("SELECT 1 FROM npcs WHERE campaign_id = ? AND slug = ?");
    let slug = base || "npc";
    let counter = 2;
    while (probe.get(campaignId, slug)) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private writeNpc(profile: NpcProfile, memory: string): void {
    db().prepare(
      `INSERT OR REPLACE INTO npcs (
         id, campaign_id, slug, name, role, status, location, relationships,
         first_seen_day, last_seen_day, memory, created_at, updated_at
       ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    ).run(
      profile.id,
      profile.campaignId,
      profile.slug,
      profile.name,
      profile.role ?? null,
      profile.status,
      profile.location ?? null,
      serializeRelationships(profile.relationships),
      profile.firstSeenDay ?? null,
      profile.lastSeenDay ?? null,
      memory,
      profile.createdAt,
      profile.updatedAt ?? null,
    );
  }
}

/** Ленивый синглтон: БД открывается при первом обращении к методу, а не на импорте. */
function lazySingleton(): SqliteNpcStore {
  let instance: SqliteNpcStore | undefined;
  return new Proxy({} as SqliteNpcStore, {
    get(_target, prop, receiver) {
      if (!instance) instance = new SqliteNpcStore();
      const value = Reflect.get(instance, prop, receiver);
      return typeof value === "function" ? value.bind(instance) : value;
    },
  });
}

export const npcStore: SqliteNpcStore = lazySingleton();
