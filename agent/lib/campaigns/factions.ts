/**
 * Хранилище фракций кампании на SQLite: таблица factions в базе кампаний
 * (campaigns.db). Профиль фракции (описание, standing) лежит в одной строке.
 *
 * standing — репутация партии у фракции: шкала -5 (враг) .. +5 (союзник).
 * Корректируется детерминированно из complete_quest.
 */
import { randomUUID } from "node:crypto";
import type BetterSqlite3 from "better-sqlite3";

import { openCampaignDb } from "./sqlite-db.ts";
import { campaignStore, slugify } from "./store.ts";
import { StoreError, type Faction, type UpsertFactionInput } from "./types.ts";

/** Клампинг репутации в диапазон -5 .. +5. */
export function clampStanding(value: number): number {
  return Math.max(-5, Math.min(5, Math.trunc(value)));
}

const SCHEMA = `
CREATE TABLE IF NOT EXISTS factions (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  standing INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
);
`;

interface FactionRow {
  id: string;
  campaign_id: string;
  slug: string;
  name: string;
  description: string | null;
  standing: number;
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

function rowToFaction(row: FactionRow): Faction {
  return {
    id: row.id,
    campaignId: row.campaign_id,
    name: row.name,
    slug: row.slug,
    description: row.description ?? undefined,
    standing: clampStanding(row.standing),
    createdAt: row.created_at,
    updatedAt: row.updated_at ?? undefined,
  };
}

export class SqliteFactionStore {
  /** Создаёт или обновляет фракцию (поиск по имени без учёта регистра). */
  upsertFaction(campaignIdOrSlug: string, input: UpsertFactionInput & { name: string }): Faction {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const existing = this.findByName(campaign.id, input.name);
    const now = new Date().toISOString();
    const profile: Faction = existing
      ? { ...existing }
      : {
          id: randomUUID(),
          campaignId: campaign.id,
          name: input.name,
          slug: this.uniqueSlug(campaign.id, slugify(input.name)),
          standing: 0,
          createdAt: now,
        };
    if (input.description !== undefined) profile.description = input.description;
    if (input.standing !== undefined) profile.standing = clampStanding(input.standing);
    profile.updatedAt = now;
    this.writeFaction(profile);
    return profile;
  }

  /**
   * Корректирует репутацию фракции на delta (с клампингом -5 .. +5).
   * Бросает StoreError(not_found), если фракция не найдена.
   */
  adjustStanding(campaignIdOrSlug: string, nameOrSlug: string, delta: number): Faction {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const faction = this.getFaction(campaign.id, nameOrSlug);
    if (!faction) {
      throw new StoreError(`Фракция «${nameOrSlug}» не найдена.`, "not_found");
    }
    faction.standing = clampStanding(faction.standing + delta);
    faction.updatedAt = new Date().toISOString();
    this.writeFaction(faction);
    return faction;
  }

  getFaction(campaignIdOrSlug: string, nameOrSlug: string): Faction | undefined {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const row = this.findRow(campaign.id, nameOrSlug);
    return row ? rowToFaction(row) : undefined;
  }

  listFactions(campaignIdOrSlug: string): Faction[] {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const rows = db().prepare(
      "SELECT * FROM factions WHERE campaign_id = ? ORDER BY created_at",
    ).all(campaign.id) as FactionRow[];
    return rows.map(rowToFaction);
  }

  // --- Внутренние помощники ---

  private findCampaign(campaignIdOrSlug: string): { id: string; slug: string } {
    const campaign = campaignStore.getCampaign(campaignIdOrSlug);
    if (!campaign) {
      throw new StoreError(`Кампания «${campaignIdOrSlug}» не найдена.`, "not_found");
    }
    return { id: campaign.id, slug: campaign.slug };
  }

  private findByName(campaignId: string, name: string): Faction | undefined {
    const needle = name.toLowerCase();
    const rows = db().prepare("SELECT * FROM factions WHERE campaign_id = ?").all(campaignId) as FactionRow[];
    const row = rows.find((candidate) => candidate.name.toLowerCase() === needle);
    return row ? rowToFaction(row) : undefined;
  }

  private findRow(campaignId: string, nameOrSlug: string): FactionRow | undefined {
    const needle = nameOrSlug.toLowerCase();
    const rows = db().prepare("SELECT * FROM factions WHERE campaign_id = ?").all(campaignId) as FactionRow[];
    return rows.find(
      (candidate) =>
        candidate.id === nameOrSlug ||
        candidate.slug.toLowerCase() === needle ||
        candidate.name.toLowerCase() === needle,
    );
  }

  private uniqueSlug(campaignId: string, base: string): string {
    const probe = db().prepare("SELECT 1 FROM factions WHERE campaign_id = ? AND slug = ?");
    let slug = base || "faction";
    let counter = 2;
    while (probe.get(campaignId, slug)) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private writeFaction(profile: Faction): void {
    db().prepare(
      `INSERT OR REPLACE INTO factions (
         id, campaign_id, slug, name, description, standing, created_at, updated_at
       ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
    ).run(
      profile.id,
      profile.campaignId,
      profile.slug,
      profile.name,
      profile.description ?? null,
      profile.standing,
      profile.createdAt,
      profile.updatedAt ?? null,
    );
  }
}

/** Ленивый синглтон: БД открывается при первом обращении к методу, а не на импорте. */
function lazySingleton(): SqliteFactionStore {
  let instance: SqliteFactionStore | undefined;
  return new Proxy({} as SqliteFactionStore, {
    get(_target, prop, receiver) {
      if (!instance) instance = new SqliteFactionStore();
      const value = Reflect.get(instance, prop, receiver);
      return typeof value === "function" ? value.bind(instance) : value;
    },
  });
}

export const factionStore: SqliteFactionStore = lazySingleton();
