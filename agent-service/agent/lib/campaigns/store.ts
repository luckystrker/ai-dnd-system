import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { randomUUID } from "node:crypto";

import { MAX_PARTY, StoreError, type BoundChat, type Campaign, type CampaignLength, type CampaignMember, type CharacterSheet, type CharacterStatePatch, type MemberRole } from "./types.ts";
import { buildDocument, splitFrontmatter } from "./frontmatter.ts";

/** Входные данные для создания кампании (после опросника). */
export interface NewCampaignInput {
  title: string;
  length: CampaignLength;
  setting: string;
  theme: string;
  goal?: string;
  tone?: string;
  openingScene?: string;
  description?: string;
}

/** Входные данные для создания персонажа. */
export interface NewCharacterInput {
  name: string;
  characterClass: string;
  race: string;
  level?: number;
  stats?: Record<string, number>;
  background?: string;
  motivation?: string;
}

export interface NewMemberInput {
  userId: string;
  name?: string;
  username?: string;
  role?: MemberRole;
}

/** Данные создателя кампании; роль dm назначается автоматически. */
export interface NewOwnerInput {
  userId: string;
  name?: string;
  username?: string;
}

/**
 * Абстракция хранилища кампаний. Сейчас реализована на MD-файлах
 * (MarkdownCampaignStore); позже заменяется на SQLite/Postgres
 * без изменения тулов.
 */
export interface CampaignStore {
  createCampaign(input: NewCampaignInput, owner: NewOwnerInput): Campaign;
  getCampaign(idOrSlug: string): Campaign | undefined;
  listCampaigns(): Campaign[];
  listForUser(userId: string): Campaign[];
  findByBoundChat(chatId: string, messageThreadId?: number): Campaign | undefined;
  bindAndActivate(campaignId: string, actorUserId: string, chat: BoundChat): Campaign;
  advanceDay(campaignId: string, actorUserId: string): Campaign;
  addMember(campaignId: string, inviterUserId: string, member: NewMemberInput): Campaign;
  saveCharacter(campaignId: string, actorUserId: string, input: NewCharacterInput): CharacterSheet;
  updateCharacter(campaignIdOrSlug: string, nameOrSlug: string, patch: CharacterStatePatch): CharacterSheet;
  listCharacters(campaignId: string): CharacterSheet[];
}

const CYRILLIC_TO_LATIN: Record<string, string> = {
  а: "a", б: "b", в: "v", г: "g", д: "d", е: "e", ё: "e", ж: "zh", з: "z",
  и: "i", й: "y", к: "k", л: "l", м: "m", н: "n", о: "o", п: "p", р: "r",
  с: "s", т: "t", у: "u", ф: "f", х: "h", ц: "ts", ч: "ch", ш: "sh",
  щ: "sch", ъ: "", ы: "y", ь: "", э: "e", ю: "yu", я: "ya",
};

export function slugify(text: string): string {
  const transliterated = text
    .toLowerCase()
    .split("")
    .map((char) => CYRILLIC_TO_LATIN[char] ?? char)
    .join("");
  const slug = transliterated
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60)
    .replace(/-+$/g, "");
  return slug || "untitled";
}

export class MarkdownCampaignStore implements CampaignStore {
  private readonly root: string;

  constructor(root: string) {
    this.root = root;
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
    this.writeCampaign(campaign, input.description ?? "");
    return campaign;
  }

  getCampaign(idOrSlug: string): Campaign | undefined {
    return this.listCampaigns().find(
      (campaign) => campaign.id === idOrSlug || campaign.slug === idOrSlug,
    );
  }

  listCampaigns(): Campaign[] {
    const campaigns: Campaign[] = [];
    if (!existsSync(this.root)) return campaigns;
    for (const entry of readdirSync(this.root, { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      const path = join(this.root, entry.name, "campaign.md");
      if (!existsSync(path)) continue;
      try {
        campaigns.push(docToCampaign(readFileSync(path, "utf8")));
      } catch {
        // Повреждённый файл пропускаем, чтобы не ронять весь список.
      }
    }
    return campaigns.sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  }

  listForUser(userId: string): Campaign[] {
    return this.listCampaigns().filter((campaign) =>
      campaign.members.some((member) => member.userId === userId),
    );
  }

  findByBoundChat(chatId: string, messageThreadId?: number): Campaign | undefined {
    return this.listCampaigns().find(
      (campaign) =>
        campaign.status === "active" &&
        campaign.boundChat?.chatId === chatId &&
        campaign.boundChat.messageThreadId === messageThreadId,
    );
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
    campaign.status = "active";
    campaign.boundChat = chat;
    if (campaign.currentDay === undefined) campaign.currentDay = 1;
    this.writeCampaign(campaign, this.readDescription(campaign));
    return campaign;
  }

  advanceDay(campaignId: string, actorUserId: string): Campaign {
    const campaign = this.mustGetCampaign(campaignId);
    this.requireRole(campaign, actorUserId, "dm");
    if (campaign.status !== "active") {
      throw new StoreError("Игровые дни можно двигать только в активной кампании.", "conflict");
    }
    campaign.currentDay = (campaign.currentDay ?? 1) + 1;
    this.writeCampaign(campaign, this.readDescription(campaign));
    return campaign;
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
    campaign.members.push({
      userId: member.userId,
      name: member.name,
      username: member.username,
      role,
    });
    this.writeCampaign(campaign, this.readDescription(campaign));
    return campaign;
  }

  saveCharacter(campaignId: string, actorUserId: string, input: NewCharacterInput): CharacterSheet {
    const campaign = this.mustGetCampaign(campaignId);
    if (!campaign.members.some((member) => member.userId === actorUserId)) {
      throw new StoreError(
        "Персонажа может создать только участник кампании. Сначала нужно вступить в кампанию (приглашение от DM).",
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
    const slug = this.uniqueCharacterSlug(campaign.slug, slugify(input.name));
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
      createdAt: new Date().toISOString(),
    };
    this.writeCharacter(campaign.slug, sheet);
    return sheet;
  }

  updateCharacter(campaignIdOrSlug: string, nameOrSlug: string, patch: CharacterStatePatch): CharacterSheet {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    const needle = nameOrSlug.toLowerCase();
    const sheet = this.listCharacters(campaign.id).find(
      (candidate) =>
        candidate.id === nameOrSlug ||
        candidate.slug.toLowerCase() === needle ||
        candidate.name.toLowerCase() === needle,
    );
    if (!sheet) {
      throw new StoreError(`Персонаж «${nameOrSlug}» не найден в кампании.`, "not_found");
    }
    if (patch.level !== undefined) sheet.level = patch.level;
    if (patch.hp !== undefined) sheet.hp = patch.hp;
    if (patch.maxHp !== undefined) sheet.maxHp = patch.maxHp;
    if (patch.conditions !== undefined) sheet.conditions = patch.conditions;
    if (patch.inventory !== undefined) sheet.inventory = patch.inventory;
    if (patch.gold !== undefined) sheet.gold = patch.gold;
    if (patch.xp !== undefined) sheet.xp = patch.xp;
    if (patch.location !== undefined) sheet.location = patch.location;
    sheet.updatedAt = new Date().toISOString();
    this.writeCharacter(campaign.slug, sheet);
    return sheet;
  }

  listCharacters(campaignId: string): CharacterSheet[] {
    const campaign = this.mustGetCampaign(campaignId);
    const dir = this.charactersDir(campaign.slug);
    if (!existsSync(dir)) return [];
    const sheets: CharacterSheet[] = [];
    for (const entry of readdirSync(dir)) {
      if (!entry.endsWith(".md")) continue;
      try {
        sheets.push(docToCharacter(readFileSync(join(dir, entry), "utf8")));
      } catch {
        // Повреждённый лист пропускаем.
      }
    }
    return sheets.sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  }

  // --- Внутренние помощники ---

  private mustGetCampaign(idOrSlug: string): Campaign {
    const campaign = this.getCampaign(idOrSlug);
    if (!campaign) {
      throw new StoreError(`Кампания «${idOrSlug}» не найдена.`, "not_found");
    }
    return campaign;
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

  private campaignDir(slug: string): string {
    return join(this.root, slug);
  }

  private charactersDir(campaignSlug: string): string {
    return join(this.campaignDir(campaignSlug), "characters");
  }

  private uniqueCampaignSlug(base: string): string {
    let slug = base;
    let counter = 2;
    while (existsSync(this.campaignDir(slug))) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private uniqueCharacterSlug(campaignSlug: string, base: string): string {
    const dir = this.charactersDir(campaignSlug);
    let slug = base;
    let counter = 2;
    while (existsSync(join(dir, `${slug}.md`))) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private readDescription(campaign: Campaign): string {
    const path = join(this.campaignDir(campaign.slug), "campaign.md");
    if (!existsSync(path)) return "";
    return splitFrontmatter(readFileSync(path, "utf8")).body;
  }

  private writeCampaign(campaign: Campaign, description: string): void {
    const dir = this.campaignDir(campaign.slug);
    mkdirSync(join(dir, "characters"), { recursive: true });
    const doc = buildDocument(campaignToFrontmatter(campaign), description);
    writeFileSync(join(dir, "campaign.md"), doc, "utf8");
  }

  private writeCharacter(campaignSlug: string, sheet: CharacterSheet): void {
    const dir = this.charactersDir(campaignSlug);
    mkdirSync(dir, { recursive: true });
    const doc = buildDocument(characterToFrontmatter(sheet), sheet.background ?? "");
    writeFileSync(join(dir, `${sheet.slug}.md`), doc, "utf8");
  }
}

function campaignToFrontmatter(campaign: Campaign): Record<string, unknown> {
  return {
    id: campaign.id,
    title: campaign.title,
    slug: campaign.slug,
    status: campaign.status,
    ownerUserId: campaign.ownerUserId,
    length: campaign.length,
    setting: campaign.setting,
    theme: campaign.theme,
    goal: campaign.goal,
    tone: campaign.tone,
    openingScene: campaign.openingScene,
    boundChat: campaign.boundChat,
    currentDay: campaign.currentDay,
    members: campaign.members,
    createdAt: campaign.createdAt,
  };
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : value === null || value === undefined ? "" : String(value);
}

function docToCampaign(doc: string): Campaign {
  const { data, body } = splitFrontmatter(doc);
  void body; // описание хранится в теле и читается отдельно
  const members = Array.isArray(data.members)
    ? (data.members as Record<string, unknown>[]).map((member) => ({
        userId: asString(member.userId),
        name: member.name ? asString(member.name) : undefined,
        username: member.username ? asString(member.username) : undefined,
        role: (member.role === "dm" ? "dm" : "player") as MemberRole,
      }))
    : [];
  const boundChat = data.boundChat as Record<string, unknown> | null | undefined;
  return {
    id: asString(data.id),
    title: asString(data.title),
    slug: asString(data.slug),
    status: (data.status as Campaign["status"]) ?? "setup",
    ownerUserId: asString(data.ownerUserId),
    length: (data.length as CampaignLength) ?? "medium",
    setting: asString(data.setting),
    theme: asString(data.theme),
    goal: data.goal ? asString(data.goal) : undefined,
    tone: data.tone ? asString(data.tone) : undefined,
    openingScene: data.openingScene ? asString(data.openingScene) : undefined,
    boundChat: boundChat
      ? {
          chatId: asString(boundChat.chatId),
          messageThreadId:
            typeof boundChat.messageThreadId === "number"
              ? boundChat.messageThreadId
              : undefined,
        }
      : undefined,
    currentDay: typeof data.currentDay === "number" ? data.currentDay : undefined,
    members,
    createdAt: asString(data.createdAt),
  };
}

function characterToFrontmatter(sheet: CharacterSheet): Record<string, unknown> {
  return {
    id: sheet.id,
    campaignId: sheet.campaignId,
    name: sheet.name,
    slug: sheet.slug,
    ownerUserId: sheet.ownerUserId,
    class: sheet.characterClass,
    race: sheet.race,
    level: sheet.level,
    stats: sheet.stats,
    motivation: sheet.motivation,
    hp: sheet.hp,
    maxHp: sheet.maxHp,
    conditions: sheet.conditions,
    inventory: sheet.inventory,
    gold: sheet.gold,
    xp: sheet.xp,
    location: sheet.location,
    updatedAt: sheet.updatedAt,
    createdAt: sheet.createdAt,
  };
}

function asStringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  return value.map((item) => asString(item));
}

function docToCharacter(doc: string): CharacterSheet {
  const { data, body } = splitFrontmatter(doc);
  const stats: Record<string, number> = {};
  if (data.stats && typeof data.stats === "object") {
    for (const [key, value] of Object.entries(data.stats as Record<string, unknown>)) {
      if (typeof value === "number") stats[key] = value;
    }
  }
  return {
    id: asString(data.id),
    campaignId: asString(data.campaignId),
    name: asString(data.name),
    slug: asString(data.slug),
    ownerUserId: asString(data.ownerUserId),
    characterClass: asString(data.class),
    race: asString(data.race),
    level: typeof data.level === "number" ? data.level : 1,
    stats,
    background: body || undefined,
    motivation: data.motivation ? asString(data.motivation) : undefined,
    hp: typeof data.hp === "number" ? data.hp : undefined,
    maxHp: typeof data.maxHp === "number" ? data.maxHp : undefined,
    conditions: asStringArray(data.conditions),
    inventory: asStringArray(data.inventory),
    gold: typeof data.gold === "number" ? data.gold : undefined,
    xp: typeof data.xp === "number" ? data.xp : undefined,
    location: data.location ? asString(data.location) : undefined,
    updatedAt: data.updatedAt ? asString(data.updatedAt) : undefined,
    createdAt: asString(data.createdAt),
  };
}

/** Корневая папка данных кампаний (переопределяется CAMPAIGN_DATA_DIR). */
export function campaignDataRoot(): string {
  return resolve(process.cwd(), process.env.CAMPAIGN_DATA_DIR ?? "data/campaigns");
}

export const campaignStore: CampaignStore = new MarkdownCampaignStore(campaignDataRoot());
