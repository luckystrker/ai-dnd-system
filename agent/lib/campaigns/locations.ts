/**
 * Хранилище локаций кампании (C1 — карта/локации): папка locations/ внутри
 * папки кампании, по образцу npc.ts. Frontmatter хранит профиль локации
 * (описание, связи, дни посещения, current), тело файла — свободные заметки.
 *
 * current-флаг один на кампанию: при установке current=true у одной локации
 * он снимается с остальных (партия может находиться только в одном месте).
 */
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

import { buildDocument, splitFrontmatter } from "./frontmatter.ts";
import { assertCampaignSlug, campaignDataRoot, campaignStore, slugify } from "./store.ts";
import { StoreError, type Location, type LocationConnection, type UpsertLocationInput } from "./types.ts";

export class MarkdownLocationStore {
  private readonly root: string;

  constructor(root: string) {
    this.root = root;
  }

  /** Создаёт или обновляет локацию (поиск по имени без учёта регистра). */
  upsertLocation(campaignIdOrSlug: string, input: UpsertLocationInput & { name: string }): Location {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const existing = this.listLocations(campaign.id).find(
      (location) => location.name.toLowerCase() === input.name!.toLowerCase(),
    );
    const now = new Date().toISOString();
    const profile: Location = existing
      ? { ...existing }
      : {
          id: randomUUID(),
          campaignId: campaign.id,
          name: input.name,
          slug: this.uniqueSlug(campaign.slug, slugify(input.name)),
          connections: [],
          visitedDays: [],
          createdAt: now,
        };
    if (input.description !== undefined) profile.description = input.description;
    if (input.discoveredDay !== undefined) profile.discoveredDay = input.discoveredDay;
    if (input.connections !== undefined) profile.connections = input.connections;
    profile.updatedAt = now;

    this.writeLocation(campaign.slug, profile);

    // current — один на кампанию: снимаем с остальных при установке true.
    if (input.current === true) {
      this.setCurrent(campaign.id, profile.slug);
      profile.current = true;
    } else if (input.current === false) {
      // Снятие только у этой локации.
      profile.current = false;
      this.writeLocation(campaign.slug, profile);
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
    for (const location of this.listLocations(campaign.id)) {
      const becomesCurrent = location.slug === target.slug;
      if (location.current !== becomesCurrent) {
        this.writeLocation(campaign.slug, { ...location, current: becomesCurrent });
      }
    }
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
    this.writeLocation(campaign.slug, location);
    return location;
  }

  getLocation(campaignIdOrSlug: string, nameOrSlug: string): Location | undefined {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const needle = nameOrSlug.toLowerCase();
    return this.listLocations(campaign.id).find(
      (location) =>
        location.id === nameOrSlug ||
        location.slug.toLowerCase() === needle ||
        location.name.toLowerCase() === needle,
    );
  }

  currentLocation(campaignIdOrSlug: string): Location | undefined {
    const campaign = this.findCampaign(campaignIdOrSlug);
    return this.listLocations(campaign.id).find((location) => location.current === true);
  }

  listLocations(campaignIdOrSlug: string): Location[] {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const dir = this.locationsDir(campaign.slug);
    if (!existsSync(dir)) return [];
    const profiles: Location[] = [];
    for (const entry of readdirSync(dir)) {
      if (!entry.endsWith(".md")) continue;
      try {
        profiles.push(docToLocation(readFileSync(join(dir, entry), "utf8")));
      } catch {
        // Повреждённый профиль пропускаем.
      }
    }
    return profiles.sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  }

  // --- Внутренние помощники ---

  private findCampaign(campaignIdOrSlug: string): { id: string; slug: string } {
    const campaign = campaignStore.getCampaign(campaignIdOrSlug);
    if (!campaign) {
      throw new StoreError(`Кампания «${campaignIdOrSlug}» не найдена.`, "not_found");
    }
    return { id: campaign.id, slug: campaign.slug };
  }

  private locationsDir(campaignSlug: string): string {
    assertCampaignSlug(campaignSlug);
    return join(this.root, campaignSlug, "locations");
  }

  private uniqueSlug(campaignSlug: string, base: string): string {
    const dir = this.locationsDir(campaignSlug);
    let slug = base || "location";
    let counter = 2;
    while (existsSync(join(dir, `${slug}.md`))) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private writeLocation(campaignSlug: string, profile: Location): void {
    const dir = this.locationsDir(campaignSlug);
    mkdirSync(dir, { recursive: true });
    const doc = buildDocument(locationToFrontmatter(profile), profile.description ?? "");
    writeFileSync(join(dir, `${profile.slug}.md`), doc, "utf8");
  }
}

function locationToFrontmatter(profile: Location): Record<string, unknown> {
  return {
    id: profile.id,
    campaignId: profile.campaignId,
    name: profile.name,
    slug: profile.slug,
    description: profile.description,
    connections: profile.connections,
    discoveredDay: profile.discoveredDay,
    visitedDays: profile.visitedDays,
    current: profile.current,
    createdAt: profile.createdAt,
    updatedAt: profile.updatedAt,
  };
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : value === null || value === undefined ? "" : String(value);
}

function asNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function parseConnections(value: unknown): LocationConnection[] {
  if (!Array.isArray(value)) return [];
  const connections: LocationConnection[] = [];
  for (const item of value) {
    if (!item || typeof item !== "object") continue;
    const record = item as Record<string, unknown>;
    const to = asString(record.to);
    if (!to) continue;
    const connection: LocationConnection = { to };
    if (record.via !== undefined) {
      const via = asString(record.via);
      if (via) connection.via = via;
    }
    const discoveredDay = asNumber(record.discoveredDay);
    if (discoveredDay !== undefined) connection.discoveredDay = discoveredDay;
    connections.push(connection);
  }
  return connections;
}

function docToLocation(doc: string): Location {
  const { data, body } = splitFrontmatter(doc);
  const visitedDays = Array.isArray(data.visitedDays)
    ? (data.visitedDays as unknown[]).map((item) => Number(item)).filter((item) => Number.isFinite(item))
    : [];
  return {
    id: asString(data.id),
    campaignId: asString(data.campaignId),
    name: asString(data.name),
    slug: asString(data.slug),
    description: body.trim() ? body.trim() : undefined,
    connections: parseConnections(data.connections),
    discoveredDay: asNumber(data.discoveredDay),
    visitedDays,
    current: data.current === true,
    createdAt: asString(data.createdAt),
    updatedAt: data.updatedAt ? asString(data.updatedAt) : undefined,
  };
}

export const locationStore: MarkdownLocationStore = new MarkdownLocationStore(campaignDataRoot());
