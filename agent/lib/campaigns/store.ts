import {
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
  type TimeOfDay,
} from "./types.ts";
import { SqliteCampaignStore } from "./store-sqlite.ts";
import { campaignDbPath } from "./sqlite-db.ts";

/** Путь к SQLite-базе кампаний (переопределяется CAMPAIGN_DB_PATH). */
export { campaignDbPath };

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
 * Абстракция хранилища кампаний. Реализована на SQLite
 * (SqliteCampaignStore, data/campaigns.db) — это единственный стор.
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
 * Валидация slug (в т.ч. приходящего как idOrSlug от LLM-тула): только
 * безопасные символы, без path traversal.
 */
export function assertCampaignSlug(slug: string): void {
  if (typeof slug !== "string" || slug.length === 0 || slug.length > 60 || !SLUG_PATTERN.test(slug)) {
    throw new StoreError(`Недопустимый slug «${slug}».`, "not_found");
  }
}

/**
 * Активное хранилище кампаний — SQLite (data/campaigns.db). MD-стора больше
 * нет: вся память кампаний живёт в одной базе.
 */
export function createCampaignStore(): CampaignStore {
  return new SqliteCampaignStore(campaignDbPath());
}

/**
 * Единая точка доступа к хранилищу. Создаётся лениво — при первом обращении,
 * а не при импорте модуля: SQLite-стор в конструкторе сразу открывает БД,
 * а eve-рантайм вычисляет модули тулов в изолированном снапшоте на этапе
 * компиляции, где такое открытие падает.
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
