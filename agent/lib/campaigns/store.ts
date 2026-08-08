import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { randomUUID } from "node:crypto";

import { MAX_PARTY, StoreError, type BoundChat, type Campaign, type CampaignLength, type CampaignMember, type CharacterAbility, type CharacterGrantPatch, type CharacterSheet, type CharacterStatePatch, type MemberRole, type NewQuestInput, type NewThreadInput, type OpenThread, type Quest, type QuestDifficulty, type QuestPatch, type QuestRewardPlan, type QuestStatus, type ThreadKind, type TimeOfDay } from "./types.ts";
import { buildDocument, splitFrontmatter } from "./frontmatter.ts";
import { SqliteCampaignStore } from "./store-sqlite.ts";

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
  appearance?: string;
  /** Стартовое снаряжение (сохраняется в inventory). */
  equipment?: string[];
  abilities?: CharacterAbility[];
  gold?: number;
  maxHp?: number;
  hp?: number;
}

/** Реэкспорт типов квестов/нитей: они живут в types.ts рядом с сущностями. */
export type {
  NewQuestInput,
  QuestPatch,
  NewThreadInput,
} from "./types.ts";

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

/** Патч игрового времени/окружения (C2): все поля опциональны. */
export interface EnvironmentPatch {
  timeOfDay?: TimeOfDay;
  inGameDate?: string;
  weather?: string;
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
  findByBoundChat(chatId: string, messageThreadId?: number, options?: { anyStatus?: boolean }): Campaign | undefined;
  bindAndActivate(campaignId: string, actorUserId: string, chat: BoundChat): Campaign;
  advanceDay(campaignId: string, actorUserId: string): Campaign;
  /** Обновляет игровое время/окружение (C2). */
  setEnvironment(campaignId: string, patch: EnvironmentPatch): Campaign;
  addMember(campaignId: string, inviterUserId: string, member: NewMemberInput): Campaign;
  autoRegister(campaignId: string, user: NewMemberInput): Campaign;
  saveCharacter(campaignId: string, actorUserId: string, input: NewCharacterInput): CharacterSheet;
  updateCharacter(campaignIdOrSlug: string, nameOrSlug: string, patch: CharacterStatePatch): CharacterSheet;
  listCharacters(campaignId: string): CharacterSheet[];
  /** Аддитивное изменение персонажа: предметы/способности/золото/XP добавляются, а не заменяются. */
  grantCharacter(campaignIdOrSlug: string, nameOrSlug: string, patch: CharacterGrantPatch): CharacterSheet;
  /** Завершение кампании (только dm): статус finished, чат освобождается. Данные сохраняются. */
  finishCampaign(campaignId: string, actorUserId: string): Campaign;
  /** Создание квеста (доступ проверяется в тулах через resolveCampaignForWrite). */
  createQuest(campaignIdOrSlug: string, input: NewQuestInput): Quest;
  /** Обновление квеста по id/slug/названию. */
  updateQuest(campaignIdOrSlug: string, questIdOrSlug: string, patch: QuestPatch): Quest;
  /** Квесты кампании (все статусы, от новых к старым). */
  listQuests(campaignId: string): Quest[];
  /** Открытая нить (обещание/тайна/долг) в журнал кампании. */
  appendThread(campaignIdOrSlug: string, input: NewThreadInput): OpenThread;
  /** Закрытие нити по id (или по совпадению текста). */
  resolveThread(campaignIdOrSlug: string, threadIdOrText: string, day?: number): OpenThread;
  /** Все нити кампании. */
  listThreads(campaignId: string): OpenThread[];
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

/** Допустимый slug кампании/персонажа: только [a-z0-9], дефисы между сегментами. */
const SLUG_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

/**
 * Защита от path traversal: любой slug, используемый для построения путей ФС
 * (в т.ч. пришедший как idOrSlug от LLM-тула), должен быть безопасным.
 */
export function assertCampaignSlug(slug: string): void {
  if (typeof slug !== "string" || slug.length === 0 || slug.length > 60 || !SLUG_PATTERN.test(slug)) {
    throw new StoreError(`Недопустимый slug «${slug}».`, "not_found");
  }
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

  findByBoundChat(chatId: string, messageThreadId?: number, options?: { anyStatus?: boolean }): Campaign | undefined {
    return this.listCampaigns().find(
      (campaign) =>
        (options?.anyStatus === true || campaign.status === "active") &&
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
    campaign.timeOfDay = "morning";
    this.writeCampaign(campaign, this.readDescription(campaign));
    return campaign;
  }

  setEnvironment(campaignId: string, patch: EnvironmentPatch): Campaign {
    const campaign = this.mustGetCampaign(campaignId);
    if (patch.timeOfDay !== undefined) campaign.timeOfDay = patch.timeOfDay;
    if (patch.inGameDate !== undefined) campaign.inGameDate = patch.inGameDate;
    if (patch.weather !== undefined) campaign.weather = patch.weather;
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

  /**
   * Авто-вступление: любой написавший в привязанный чат становится игроком.
   * Без проверки роли DM и идемпотентно: существующий участник не меняется,
   * запрошенная роль dm принудительно снижается до player, при полной партии
   * (MAX_PARTY игроков) новый участник молча пропускается.
   */
  autoRegister(campaignId: string, user: NewMemberInput): Campaign {
    const campaign = this.mustGetCampaign(campaignId);
    if (campaign.members.some((existing) => existing.userId === user.userId)) {
      return campaign;
    }
    const players = campaign.members.filter((member) => member.role === "player").length;
    if (players >= MAX_PARTY) return campaign;
    campaign.members.push({
      userId: user.userId,
      name: user.name,
      username: user.username,
      role: "player",
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
      appearance: input.appearance,
      maxHp: input.maxHp,
      hp: input.hp ?? input.maxHp,
      inventory: input.equipment && input.equipment.length > 0 ? input.equipment : undefined,
      abilities: input.abilities && input.abilities.length > 0 ? input.abilities : undefined,
      gold: input.gold,
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
    if (patch.abilities !== undefined) sheet.abilities = patch.abilities;
    if (patch.gold !== undefined) sheet.gold = patch.gold;
    if (patch.xp !== undefined) sheet.xp = patch.xp;
    if (patch.location !== undefined) sheet.location = patch.location;
    sheet.updatedAt = new Date().toISOString();
    this.writeCharacter(campaign.slug, sheet);
    return sheet;
  }

  grantCharacter(campaignIdOrSlug: string, nameOrSlug: string, patch: CharacterGrantPatch): CharacterSheet {
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
    const result: CharacterSheet = { ...sheet };
    if (patch.inventory && patch.inventory.length > 0) {
      const existing = result.inventory ?? [];
      result.inventory = [...existing, ...patch.inventory];
    }
    if (patch.abilities && patch.abilities.length > 0) {
      const existing = result.abilities ?? [];
      const known = new Set(existing.map((ability) => ability.name.toLowerCase()));
      const fresh = patch.abilities.filter((ability) => !known.has(ability.name.toLowerCase()));
      result.abilities = [...existing, ...fresh];
    }
    if (patch.gold !== undefined) result.gold = (result.gold ?? 0) + patch.gold;
    if (patch.xp !== undefined) result.xp = (result.xp ?? 0) + patch.xp;
    if (patch.conditions && patch.conditions.length > 0) {
      const existing = result.conditions ?? [];
      result.conditions = [...new Set([...existing, ...patch.conditions])];
    }
    result.updatedAt = new Date().toISOString();
    this.writeCharacter(campaign.slug, result);
    return result;
  }

  finishCampaign(campaignId: string, actorUserId: string): Campaign {
    const campaign = this.mustGetCampaign(campaignId);
    this.requireRole(campaign, actorUserId, "dm");
    if (campaign.status !== "active" && campaign.status !== "setup") {
      throw new StoreError(`Кампания «${campaign.title}» уже завершена.`, "conflict");
    }
    campaign.status = "finished";
    campaign.boundChat = undefined;
    this.writeCampaign(campaign, this.readDescription(campaign));
    return campaign;
  }

  createQuest(campaignIdOrSlug: string, input: NewQuestInput): Quest {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    const existing = this.listQuests(campaign.id);
    if (existing.some((quest) => quest.title.toLowerCase() === input.title.toLowerCase())) {
      throw new StoreError(`Квест «${input.title}» уже есть в кампании.`, "duplicate");
    }
    const now = new Date().toISOString();
    const quest: Quest = {
      id: randomUUID(),
      campaignId: campaign.id,
      slug: this.uniqueQuestSlug(campaign.slug, slugify(input.title)),
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
    this.writeQuest(campaign.slug, quest);
    return quest;
  }

  updateQuest(campaignIdOrSlug: string, questIdOrSlug: string, patch: QuestPatch): Quest {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    const quest = this.findQuest(campaign.id, questIdOrSlug);
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
    this.writeQuest(campaign.slug, updated);
    return updated;
  }

  listQuests(campaignId: string): Quest[] {
    const campaign = this.mustGetCampaign(campaignId);
    const dir = this.questsDir(campaign.slug);
    if (!existsSync(dir)) return [];
    const quests: Quest[] = [];
    for (const entry of readdirSync(dir)) {
      if (!entry.endsWith(".md")) continue;
      try {
        quests.push(docToQuest(readFileSync(join(dir, entry), "utf8")));
      } catch {
        // Повреждённую карточку квеста пропускаем.
      }
    }
    return quests.sort((a, b) => b.createdAt.localeCompare(a.createdAt));
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
    const threads = this.readThreads(campaign.slug);
    threads.push(thread);
    this.writeThreads(campaign.slug, threads);
    return thread;
  }

  resolveThread(campaignIdOrSlug: string, threadIdOrText: string, day?: number): OpenThread {
    const campaign = this.mustGetCampaign(campaignIdOrSlug);
    const needle = threadIdOrText.toLowerCase();
    const threads = this.readThreads(campaign.slug);
    const open = threads.filter(
      (entry) => entry.status === "open" && (entry.id === threadIdOrText || entry.text.toLowerCase().includes(needle)),
    );
    if (open.length === 0) {
      throw new StoreError(`Открытая нить «${threadIdOrText}» не найдена.`, "not_found");
    }
    if (open.length > 1) {
      throw new StoreError(
        `Фрагмент «${threadIdOrText}» подходит к нескольким нитям: ${open.map((entry) => entry.text).join("; ")}. Уточни текст или передай id.`,
        "conflict",
      );
    }
    const thread = open[0];
    thread.status = "resolved";
    thread.dayClosed = day ?? campaign.currentDay ?? 1;
    thread.updatedAt = new Date().toISOString();
    this.writeThreads(campaign.slug, threads);
    return thread;
  }

  listThreads(campaignId: string): OpenThread[] {
    const campaign = this.mustGetCampaign(campaignId);
    return this.readThreads(campaign.slug).sort((a, b) =>
      a.createdAt.localeCompare(b.createdAt),
    );
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
    assertCampaignSlug(slug);
    return join(this.root, slug);
  }

  private charactersDir(campaignSlug: string): string {
    return join(this.campaignDir(campaignSlug), "characters");
  }

  private questsDir(campaignSlug: string): string {
    return join(this.campaignDir(campaignSlug), "quests");
  }

  private findQuest(campaignId: string, questIdOrSlug: string): Quest {
    const needle = questIdOrSlug.toLowerCase();
    const quest = this.listQuests(campaignId).find(
      (candidate) =>
        candidate.id === questIdOrSlug ||
        candidate.slug.toLowerCase() === needle ||
        candidate.title.toLowerCase() === needle,
    );
    if (!quest) {
      throw new StoreError(`Квест «${questIdOrSlug}» не найден в кампании.`, "not_found");
    }
    return quest;
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

  private uniqueQuestSlug(campaignSlug: string, base: string): string {
    const dir = this.questsDir(campaignSlug);
    let slug = base || "quest";
    let counter = 2;
    while (existsSync(join(dir, `${slug}.md`))) {
      slug = `${base}-${counter}`;
      counter += 1;
    }
    return slug;
  }

  private writeQuest(campaignSlug: string, quest: Quest): void {
    const dir = this.questsDir(campaignSlug);
    mkdirSync(dir, { recursive: true });
    const doc = buildDocument(questToFrontmatter(quest), quest.objective);
    writeFileSync(join(dir, `${quest.slug}.md`), doc, "utf8");
  }

  private threadsPath(campaignSlug: string): string {
    return join(this.campaignDir(campaignSlug), "threads.md");
  }

  private readThreads(campaignSlug: string): OpenThread[] {
    const path = this.threadsPath(campaignSlug);
    if (!existsSync(path)) return [];
    return docToThreads(readFileSync(path, "utf8"));
  }

  private writeThreads(campaignSlug: string, threads: OpenThread[]): void {
    const dir = this.campaignDir(campaignSlug);
    mkdirSync(dir, { recursive: true });
    const doc = buildDocument({ threads: threads.map(threadToFrontmatter) }, "");
    writeFileSync(this.threadsPath(campaignSlug), doc, "utf8");
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
    timeOfDay: campaign.timeOfDay,
    inGameDate: campaign.inGameDate,
    weather: campaign.weather,
    members: campaign.members,
    createdAt: campaign.createdAt,
  };
}

const TIME_OF_DAY_VALUES: TimeOfDay[] = ["morning", "day", "evening", "night"];
function parseTimeOfDay(value: unknown): TimeOfDay | undefined {
  return typeof value === "string" && (TIME_OF_DAY_VALUES as string[]).includes(value)
    ? (value as TimeOfDay)
    : undefined;
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
    timeOfDay: parseTimeOfDay(data.timeOfDay),
    inGameDate: data.inGameDate ? asString(data.inGameDate) : undefined,
    weather: data.weather ? asString(data.weather) : undefined,
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
    appearance: sheet.appearance,
    hp: sheet.hp,
    maxHp: sheet.maxHp,
    conditions: sheet.conditions,
    inventory: sheet.inventory,
    abilities: sheet.abilities,
    gold: sheet.gold,
    xp: sheet.xp,
    location: sheet.location,
    updatedAt: sheet.updatedAt,
    createdAt: sheet.createdAt,
  };
}

function questToFrontmatter(quest: Quest): Record<string, unknown> {
  return {
    id: quest.id,
    campaignId: quest.campaignId,
    title: quest.title,
    slug: quest.slug,
    giverNpcSlug: quest.giverNpcSlug,
    objective: quest.objective,
    difficulty: quest.difficulty,
    rewardPlan: quest.rewardPlan,
    status: quest.status,
    deadlineDay: quest.deadlineDay,
    createdDay: quest.createdDay,
    createdAt: quest.createdAt,
    updatedAt: quest.updatedAt,
  };
}

function docToQuest(doc: string): Quest {
  const { data } = splitFrontmatter(doc);
  const difficulty =
    data.difficulty === "easy" || data.difficulty === "hard" ? data.difficulty : "medium";
  const status = questStatusOf(data.status);
  return {
    id: asString(data.id),
    campaignId: asString(data.campaignId),
    title: asString(data.title),
    slug: asString(data.slug),
    giverNpcSlug: data.giverNpcSlug ? asString(data.giverNpcSlug) : undefined,
    objective: asString(data.objective),
    difficulty,
    rewardPlan: data.rewardPlan && typeof data.rewardPlan === "object"
      ? asRewardPlan(data.rewardPlan as Record<string, unknown>)
      : undefined,
    status,
    deadlineDay: typeof data.deadlineDay === "number" ? data.deadlineDay : undefined,
    createdDay: typeof data.createdDay === "number" ? data.createdDay : 1,
    createdAt: asString(data.createdAt),
    updatedAt: data.updatedAt ? asString(data.updatedAt) : undefined,
  };
}

function asRewardPlan(value: Record<string, unknown>): QuestRewardPlan | undefined {
  const plan: QuestRewardPlan = {};
  if (typeof value.xp === "number") plan.xp = value.xp;
  if (typeof value.gold === "number") plan.gold = value.gold;
  if (Array.isArray(value.items)) plan.items = value.items.map((item) => asString(item)).filter(Boolean);
  if (typeof value.note === "string" && value.note.trim() !== "") plan.note = value.note;
  return Object.keys(plan).length > 0 ? plan : undefined;
}

function questStatusOf(value: unknown): QuestStatus {
  const statuses: QuestStatus[] = ["offered", "accepted", "active", "completed", "failed", "abandoned"];
  return statuses.includes(value as QuestStatus) ? (value as QuestStatus) : "offered";
}

function threadToFrontmatter(thread: OpenThread): Record<string, unknown> {
  return {
    id: thread.id,
    campaignId: thread.campaignId,
    text: thread.text,
    kind: thread.kind,
    status: thread.status,
    linkedQuestId: thread.linkedQuestId,
    dayOpened: thread.dayOpened,
    dayClosed: thread.dayClosed,
    createdAt: thread.createdAt,
    updatedAt: thread.updatedAt,
  };
}

function docToThreads(doc: string): OpenThread[] {
  const { data } = splitFrontmatter(doc);
  if (!Array.isArray(data.threads)) return [];
  const threads: OpenThread[] = [];
  for (const item of data.threads) {
    if (!item || typeof item !== "object") continue;
    const record = item as Record<string, unknown>;
    const kinds: ThreadKind[] = ["promise", "mystery", "debt", "unresolved"];
    const kind = kinds.includes(record.kind as ThreadKind)
      ? (record.kind as ThreadKind)
      : "unresolved";
    threads.push({
      id: asString(record.id),
      campaignId: asString(record.campaignId),
      text: asString(record.text),
      kind,
      status: record.status === "resolved" ? "resolved" : "open",
      linkedQuestId: record.linkedQuestId ? asString(record.linkedQuestId) : undefined,
      dayOpened: typeof record.dayOpened === "number" ? record.dayOpened : 1,
      dayClosed: typeof record.dayClosed === "number" ? record.dayClosed : undefined,
      createdAt: asString(record.createdAt),
      updatedAt: record.updatedAt ? asString(record.updatedAt) : undefined,
    });
  }
  return threads;
}

function asStringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  return value.map((item) => asString(item));
}

function asAbilityArray(value: unknown): CharacterAbility[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const abilities: CharacterAbility[] = [];
  for (const item of value) {
    if (!item || typeof item !== "object") continue;
    const record = item as Record<string, unknown>;
    const name = asString(record.name);
    const description = asString(record.description);
    if (!name || !description) continue;
    const ability: CharacterAbility = { name, description };
    if (typeof record.level === "number") ability.level = record.level;
    abilities.push(ability);
  }
  return abilities.length > 0 ? abilities : undefined;
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
    appearance: data.appearance ? asString(data.appearance) : undefined,
    hp: typeof data.hp === "number" ? data.hp : undefined,
    maxHp: typeof data.maxHp === "number" ? data.maxHp : undefined,
    conditions: asStringArray(data.conditions),
    inventory: asStringArray(data.inventory),
    abilities: asAbilityArray(data.abilities),
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

/** Путь к SQLite-базе кампаний (переопределяется CAMPAIGN_DB_PATH). */
export function campaignDbPath(): string {
  return resolve(process.cwd(), process.env.CAMPAIGN_DB_PATH ?? "data/campaigns.db");
}

/**
 * Активное хранилище кампаний: SQLite по умолчанию, Markdown как откат
 * (CAMPAIGN_STORE=markdown) и источник данных для миграции.
 */
export function createCampaignStore(): CampaignStore {
  const kind = (process.env.CAMPAIGN_STORE ?? "sqlite").trim().toLowerCase();
  return kind === "markdown"
    ? new MarkdownCampaignStore(campaignDataRoot())
    : new SqliteCampaignStore(campaignDbPath());
}

/**
 * Единая точка доступа к хранилищу. Создаётся лениво — при первом обращении,
 * а не при импорте модуля: SQLite-стор в конструкторе сразу открывает БД,
 * а eve-рантайм вычисляет модули тулов в изолированном снапшоте на этапе
 * компиляции, где такое открытие падает. Markdown-стор конструктором ФС не
 * трогал, поэтому раньше этой проблемы не было.
 */
let campaignStoreInstance: CampaignStore | undefined;

function realizedStore(): CampaignStore {
  if (!campaignStoreInstance) campaignStoreInstance = createCampaignStore();
  return campaignStoreInstance;
}

export const campaignStore: CampaignStore = new Proxy({} as CampaignStore, {
  get(_target, prop, receiver) {
    const value = Reflect.get(realizedStore(), prop, receiver);
    return typeof value === "function" ? value.bind(realizedStore()) : value;
  },
});
