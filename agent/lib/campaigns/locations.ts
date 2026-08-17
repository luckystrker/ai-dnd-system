/**
 * Хранилище локаций кампании на SQLite: таблица locations в базе кампаний
 * (campaigns.db). Профиль локации (описание, связи, дни посещения, current)
 * лежит в одной строке; connections и visitedDays — JSON-колонками.
 *
 * current-флаг один на кампанию: при установке current=true у одной локации
 * он снимается с остальных (партия может находиться только в одном месте).
 */
import { randomUUID } from "node:crypto";
import type BetterSqlite3 from "better-sqlite3";

import { openCampaignDb } from "./sqlite-db.ts";
import { campaignStore, slugify } from "./store.ts";
import { StoreError, type Location, type LocationConnection, type UpsertLocationInput } from "./types.ts";

const SCHEMA = `
CREATE TABLE IF NOT EXISTS locations (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  connections TEXT NOT NULL DEFAULT '[]', -- JSON LocationConnection[]
  discovered_day INTEGER,
  visited_days TEXT NOT NULL DEFAULT '[]', -- JSON number[]
  current INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
);
`;

interface LocationRow {
  id: string;
  campaign_id: string;
  slug: string;
  name: string;
  description: string | null;
  connections: string;
  discovered_day: number | null;
  visited_days: string;
  current: number;
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

/** Связи как JSON-строка: [{to, via?, discoveredDay?}]. */
function serializeConnections(connections: LocationConnection[]): string {
  const items = connections.map((connection) => ({
    to: connection.to,
    ...(connection.via ? { via: connection.via } : {}),
    ...(connection.discoveredDay !== undefined ? { discoveredDay: connection.discoveredDay } : {}),
  }));
  return JSON.stringify(items);
}

function deserializeConnections(value: string): LocationConnection[] {
  const connections: LocationConnection[] = [];
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed)) return connections;
    for (const item of parsed) {
      if (!item || typeof item !== "object") continue;
      const record = item as Record<string, unknown>;
      const to = typeof record.to === "string" ? record.to : "";
      if (!to) continue;
      const connection: LocationConnection = { to };
      if (typeof record.via === "string" && record.via) connection.via = record.via;
      if (typeof record.discoveredDay === "number") connection.discoveredDay = record.discoveredDay;
      connections.push(connection);
    }
  } catch {
    // Повреждённый JSON игнорируем.
  }
  return connections;
}

function deserializeVisitedDays(value: string): number[] {
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed)) return [];
    return parsed.map((item) => Number(item)).filter((item) => Number.isFinite(item));
  } catch {
    return [];
  }
}

function rowToLocation(row: LocationRow): Location {
  return {
    id: row.id,
    campaignId: row.campaign_id,
    name: row.name,
    slug: row.slug,
    description: row.description ?? undefined,
    connections: deserializeConnections(row.connections),
    discoveredDay: row.discovered_day ?? undefined,
    visitedDays: deserializeVisitedDays(row.visited_days),
    current: row.current === 1,
    createdAt: row.created_at,
    updatedAt: row.updated_at ?? undefined,
  };
}

export class SqliteLocationStore {
  /** Создаёт или обновляет локацию (поиск по имени без учёта регистра). */
  upsertLocation(campaignIdOrSlug: string, input: UpsertLocationInput & { name: string }): Location {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const existing = this.findByName(campaign.id, input.name);
    const now = new Date().toISOString();
    const profile: Location = existing
      ? { ...existing }
      : {
          id: randomUUID(),
          campaignId: campaign.id,
          name: input.name,
          slug: this.uniqueSlug(campaign.id, slugify(input.name)),
          connections: [],
          visitedDays: [],
          createdAt: now,
        };
    if (input.description !== undefined) profile.description = input.description;
    if (input.discoveredDay !== undefined) profile.discoveredDay = input.discoveredDay;
    if (input.connections !== undefined) profile.connections = input.connections;
    profile.updatedAt = now;

    this.writeLocation(profile);

    // current — один на кампанию: снимаем с остальных при установке true.
    if (input.current === true) {
      this.setCurrent(campaign.id, profile.slug);
      profile.current = true;
    } else if (input.current === false) {
      // Снятие только у этой локации.
      profile.current = false;
      this.writeLocation(profile);
    }
    return profile;
  }

  /** Устанавливает локацию текущей, снимая флаг с остальных. */
  setCurrent(campaignIdOrSlug: string, locationSlug: string): void {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const target = this.getLocation(campaign.id, locationSlug);
    if (!target) {
      throw new StoreError(`Локация «${locationSlug}» не найдена.`, "not_found");
    }
    const now = new Date().toISOString();
    db().prepare("UPDATE locations SET current = 0, updated_at = ? WHERE campaign_id = ?").run(now, campaign.id);
    db().prepare("UPDATE locations SET current = 1, updated_at = ? WHERE id = ?").run(now, target.id);
  }

  /** Отмечает день посещения (добавляет в visitedDays, без дубликатов). */
  markVisited(campaignIdOrSlug: string, locationSlug: string, day: number): Location | undefined {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const location = this.getLocation(campaign.id, locationSlug);
    if (!location) return undefined;
    if (!location.visitedDays.includes(day)) {
      location.visitedDays = [...location.visitedDays, day].sort((a, b) => a - b);
    }
    location.updatedAt = new Date().toISOString();
    this.writeLocation(location);
    return location;
  }

  getLocation(campaignIdOrSlug: string, nameOrSlug: string): Location | undefined {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const row = this.findRow(campaign.id, nameOrSlug);
    return row ? rowToLocation(row) : undefined;
  }

  currentLocation(campaignIdOrSlug: string): Location | undefined {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const row = db().prepare(
      "SELECT * FROM locations WHERE campaign_id = ? AND current = 1 LIMIT 1",
    ).get(campaign.id) as LocationRow | undefined;
    return row ? rowToLocation(row) : undefined;
  }

  listLocations(campaignIdOrSlug: string): Location[] {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const rows = db().prepare(
      "SELECT * FROM locations WHERE campaign_id = ? ORDER BY created_at",
    ).all(campaign.id) as LocationRow[];
    return rows.map(rowToLocation);
  }

  // --- Внутренние помощники ---

  private findCampaign(campaignIdOrSlug: string): { id: string; slug: string } {
    const campaign = campaignStore.getCampaign(campaignIdOrSlug);
    if (!campaign) {
      throw new StoreError(`Кампания «${campaignIdOrSlug}» не найдена.`, "not_found");
    }
    return { id: campaign.id, slug: campaign.slug };
  }

  private findByName(campaignId: string, name: string): Location | undefined {
    const needle = name.toLowerCase();
    const rows = db().prepare("SELECT * FROM locations WHERE campaign_id = ?").all(campaignId) as LocationRow[];
    const row = rows.find((candidate) => candidate.name.toLowerCase() === needle);
    return row ? rowToLocation(row) : undefined;
  }

  private findRow(campaignId: string, nameOrSlug: string): LocationRow | undefined {
    const needle = nameOrSlug.toLowerCase();
    const rows = db().prepare("SELECT * FROM locations WHERE campaign_id = ?").all(campaignId) as LocationRow[];
    return rows.find(
      (candidate) =>
        candidate.id === nameOrSlug ||
        candidate.slug.toLowerCase() === needle ||
        candidate.name.toLowerCase() === needle,
    );
  }

  private uniqueSlug(campaignId: string, base: string): string {
    const probe = db().prepare("SELECT 1 FROM locations WHERE campaign_id = ? AND slug = ?");
    let slug = base || "location";
    let counter = 2;
    while (probe.get(campaignId, slug)) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private writeLocation(profile: Location): void {
    db().prepare(
      `INSERT OR REPLACE INTO locations (
         id, campaign_id, slug, name, description, connections, discovered_day,
         visited_days, current, created_at, updated_at
       ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    ).run(
      profile.id,
      profile.campaignId,
      profile.slug,
      profile.name,
      profile.description ?? null,
      serializeConnections(profile.connections),
      profile.discoveredDay ?? null,
      JSON.stringify(profile.visitedDays),
      profile.current === true ? 1 : 0,
      profile.createdAt,
      profile.updatedAt ?? null,
    );
  }
}

/** Ленивый синглтон: БД открывается при первом обращении к методу, а не на импорте. */
function lazySingleton(): SqliteLocationStore {
  let instance: SqliteLocationStore | undefined;
  return new Proxy({} as SqliteLocationStore, {
    get(_target, prop, receiver) {
      if (!instance) instance = new SqliteLocationStore();
      const value = Reflect.get(instance, prop, receiver);
      return typeof value === "function" ? value.bind(instance) : value;
    },
  });
}

export const locationStore: SqliteLocationStore = lazySingleton();
