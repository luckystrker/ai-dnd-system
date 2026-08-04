/**
 * Хранилище NPC кампании: отдельная папка npcs/ внутри папки кампании.
 * Frontmatter хранит профиль (роль, статус, отношения), тело файла —
 * память NPC: что игроки с ним сделали и что он об этом знает.
 */
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

import { buildDocument, splitFrontmatter } from "./frontmatter.ts";
import { campaignDataRoot, campaignStore, slugify } from "./store.ts";
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

export class MarkdownNpcStore {
  private readonly root: string;

  constructor(root: string) {
    this.root = root;
  }

  /** Создаёт или обновляет NPC (поиск по имени без учёта регистра). */
  upsertNpc(campaignIdOrSlug: string, input: NpcUpsertInput): NpcProfile & { memory: string } {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const existing = this.listNpcs(campaign.id).find(
      (npc) => npc.name.toLowerCase() === input.name.toLowerCase(),
    );
    const now = new Date().toISOString();
    const profile: NpcProfile = existing
      ? { ...existing }
      : {
          id: randomUUID(),
          campaignId: campaign.id,
          name: input.name,
          slug: this.uniqueSlug(campaign.slug, slugify(input.name)),
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

    let memory = existing ? this.readMemory(campaign.slug, existing.slug) : "";
    const append = input.memoryAppend?.trim();
    if (append) {
      const dayMark = input.memoryAppendDay !== undefined ? ` [День ${input.memoryAppendDay}]` : "";
      const line = `-${dayMark} ${append.replace(/\s*\n\s*/g, " ")}`;
      memory = memory ? `${memory}\n${line}` : line;
    }
    this.writeNpc(campaign.slug, profile, memory);
    return { ...profile, memory };
  }

  /** Полная карточка NPC вместе с памятью. */
  getNpc(campaignIdOrSlug: string, nameOrSlug: string): (NpcProfile & { memory: string }) | undefined {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const needle = nameOrSlug.toLowerCase();
    const profile = this.listNpcs(campaign.id).find(
      (npc) =>
        npc.id === nameOrSlug ||
        npc.slug.toLowerCase() === needle ||
        npc.name.toLowerCase() === needle,
    );
    if (!profile) return undefined;
    return { ...profile, memory: this.readMemory(campaign.slug, profile.slug) };
  }

  listNpcs(campaignIdOrSlug: string): NpcProfile[] {
    const campaign = this.findCampaign(campaignIdOrSlug);
    const dir = this.npcsDir(campaign.slug);
    if (!existsSync(dir)) return [];
    const profiles: NpcProfile[] = [];
    for (const entry of readdirSync(dir)) {
      if (!entry.endsWith(".md")) continue;
      try {
        profiles.push(docToNpc(readFileSync(join(dir, entry), "utf8")));
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

  private npcsDir(campaignSlug: string): string {
    return join(this.root, campaignSlug, "npcs");
  }

  private uniqueSlug(campaignSlug: string, base: string): string {
    const dir = this.npcsDir(campaignSlug);
    let slug = base || "npc";
    let counter = 2;
    while (existsSync(join(dir, `${slug}.md`))) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private readMemory(campaignSlug: string, npcSlug: string): string {
    const path = join(this.npcsDir(campaignSlug), `${npcSlug}.md`);
    if (!existsSync(path)) return "";
    return splitFrontmatter(readFileSync(path, "utf8")).body;
  }

  private writeNpc(campaignSlug: string, profile: NpcProfile, memory: string): void {
    const dir = this.npcsDir(campaignSlug);
    mkdirSync(dir, { recursive: true });
    const doc = buildDocument(npcToFrontmatter(profile), memory);
    writeFileSync(join(dir, `${profile.slug}.md`), doc, "utf8");
  }
}

function npcToFrontmatter(profile: NpcProfile): Record<string, unknown> {
  const relationships: Record<string, unknown> = {};
  for (const [name, relation] of Object.entries(profile.relationships)) {
    relationships[name] = { attitude: relation.attitude, ...(relation.notes ? { notes: relation.notes } : {}) };
  }
  return {
    id: profile.id,
    campaignId: profile.campaignId,
    name: profile.name,
    slug: profile.slug,
    role: profile.role,
    status: profile.status,
    location: profile.location,
    relationships,
    firstSeenDay: profile.firstSeenDay,
    lastSeenDay: profile.lastSeenDay,
    createdAt: profile.createdAt,
    updatedAt: profile.updatedAt,
  };
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : value === null || value === undefined ? "" : String(value);
}

function docToNpc(doc: string): NpcProfile {
  const { data } = splitFrontmatter(doc);
  const relationships: Record<string, NpcRelationship> = {};
  if (data.relationships && typeof data.relationships === "object") {
    for (const [name, value] of Object.entries(data.relationships as Record<string, unknown>)) {
      if (value && typeof value === "object") {
        const relation = value as Record<string, unknown>;
        relationships[name] = {
          attitude: typeof relation.attitude === "number" ? relation.attitude : 0,
          notes: relation.notes ? asString(relation.notes) : undefined,
        };
      }
    }
  }
  const status = data.status === "dead" || data.status === "unknown" ? data.status : "alive";
  return {
    id: asString(data.id),
    campaignId: asString(data.campaignId),
    name: asString(data.name),
    slug: asString(data.slug),
    role: data.role ? asString(data.role) : undefined,
    status,
    location: data.location ? asString(data.location) : undefined,
    relationships,
    firstSeenDay: typeof data.firstSeenDay === "number" ? data.firstSeenDay : undefined,
    lastSeenDay: typeof data.lastSeenDay === "number" ? data.lastSeenDay : undefined,
    createdAt: asString(data.createdAt),
    updatedAt: data.updatedAt ? asString(data.updatedAt) : undefined,
  };
}

export const npcStore: MarkdownNpcStore = new MarkdownNpcStore(campaignDataRoot());
