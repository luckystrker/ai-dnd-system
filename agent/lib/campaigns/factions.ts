/**
 * Хранилище фракций кампании (C4 — фракции/отношения): папка factions/ внутри
 * папки кампании, по образцу npc.ts. Frontmatter хранит профиль (описание,
 * standing), тело файла — свободные заметки.
 *
 * standing — репутация партии у фракции: шкала -5 (враг) .. +5 (союзник).
 * Корректируется детерминированно из complete_quest.
 */
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

import { buildDocument, splitFrontmatter } from "./frontmatter.ts";
import { assertCampaignSlug, campaignDataRoot, campaignStore, slugify } from "./store.ts";
import { StoreError, type Faction, type UpsertFactionInput } from "./types.ts";

/** Клампинг репутации в диапазон -5 .. +5. */
function clampStanding(value: number): number {
  return Math.max(-5, Math.min(5, Math.trunc(value)));
}

export class MarkdownFactionStore {
  private readonly root: string;

  constructor(root: string) {
    this.root = root;
  }

  /** Создаёт или обновляет фракцию (поиск по имени без учёта регистра). */
  upsertFaction(campaignIdOrSlug: string, input: UpsertFactionInput & { name: string }): Faction {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const existing = this.listFactions(campaign.id).find(
      (faction) => faction.name.toLowerCase() === input.name!.toLowerCase(),
    );
    const now = new Date().toISOString();
    const profile: Faction = existing
      ? { ...existing }
      : {
          id: randomUUID(),
          campaignId: campaign.id,
          name: input.name,
          slug: this.uniqueSlug(campaign.slug, slugify(input.name)),
          standing: 0,
          createdAt: now,
        };
    if (input.description !== undefined) profile.description = input.description;
    if (input.standing !== undefined) profile.standing = clampStanding(input.standing);
    profile.updatedAt = now;
    this.writeFaction(campaign.slug, profile);
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
    this.writeFaction(campaign.slug, faction);
    return faction;
  }

  getFaction(campaignIdOrSlug: string, nameOrSlug: string): Faction | undefined {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const needle = nameOrSlug.toLowerCase();
    return this.listFactions(campaign.id).find(
      (faction) =>
        faction.id === nameOrSlug ||
        faction.slug.toLowerCase() === needle ||
        faction.name.toLowerCase() === needle,
    );
  }

  listFactions(campaignIdOrSlug: string): Faction[] {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const dir = this.factionsDir(campaign.slug);
    if (!existsSync(dir)) return [];
    const profiles: Faction[] = [];
    for (const entry of readdirSync(dir)) {
      if (!entry.endsWith(".md")) continue;
      try {
        profiles.push(docToFaction(readFileSync(join(dir, entry), "utf8")));
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

  private factionsDir(campaignSlug: string): string {
    assertCampaignSlug(campaignSlug);
    return join(this.root, campaignSlug, "factions");
  }

  private uniqueSlug(campaignSlug: string, base: string): string {
    const dir = this.factionsDir(campaignSlug);
    let slug = base || "faction";
    let counter = 2;
    while (existsSync(join(dir, `${slug}.md`))) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private writeFaction(campaignSlug: string, profile: Faction): void {
    const dir = this.factionsDir(campaignSlug);
    mkdirSync(dir, { recursive: true });
    const doc = buildDocument(factionToFrontmatter(profile), profile.description ?? "");
    writeFileSync(join(dir, `${profile.slug}.md`), doc, "utf8");
  }
}

function factionToFrontmatter(profile: Faction): Record<string, unknown> {
  return {
    id: profile.id,
    campaignId: profile.campaignId,
    name: profile.name,
    slug: profile.slug,
    description: profile.description,
    standing: profile.standing,
    createdAt: profile.createdAt,
    updatedAt: profile.updatedAt,
  };
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : value === null || value === undefined ? "" : String(value);
}

function docToFaction(doc: string): Faction {
  const { data, body } = splitFrontmatter(doc);
  const standing = typeof data.standing === "number" ? clampStanding(data.standing) : 0;
  return {
    id: asString(data.id),
    campaignId: asString(data.campaignId),
    name: asString(data.name),
    slug: asString(data.slug),
    description: body.trim() ? body.trim() : undefined,
    standing,
    createdAt: asString(data.createdAt),
    updatedAt: data.updatedAt ? asString(data.updatedAt) : undefined,
  };
}

export const factionStore: MarkdownFactionStore = new MarkdownFactionStore(campaignDataRoot());
