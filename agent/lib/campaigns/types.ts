import { z } from "zod";

/** Роль участника кампании. */
export type MemberRole = "dm" | "player";

/** Длина кампании: короткая, средняя, длинная. */
export type CampaignLength = "short" | "medium" | "long";

/** Статус жизненного цикла кампании. */
export type CampaignStatus = "setup" | "active" | "finished";

export interface CampaignMember {
  userId: string;
  name?: string;
  username?: string;
  role: MemberRole;
}

export interface BoundChat {
  chatId: string;
  messageThreadId?: number;
}

export interface Campaign {
  id: string;
  title: string;
  slug: string;
  status: CampaignStatus;
  ownerUserId: string;
  length: CampaignLength;
  setting: string;
  theme: string;
  goal?: string;
  tone?: string;
  openingScene?: string;
  boundChat?: BoundChat;
  /** Текущий игровой день (1 после старта кампании). */
  currentDay?: number;
  members: CampaignMember[];
  createdAt: string;
}

/** Способность/заклинание персонажа: имя, описание, уровень получения. */
export interface CharacterAbility {
  name: string;
  description: string;
  /** Минимальный уровень персонажа для использования (у 1-го уровня — 1). */
  level?: number;
}

export interface CharacterSheet {
  id: string;
  campaignId: string;
  name: string;
  slug: string;
  ownerUserId: string;
  characterClass: string;
  race: string;
  level: number;
  stats: Record<string, number>;
  background?: string;
  motivation?: string;
  /** Внешность персонажа для иллюстраций сцен (на английском). */
  appearance?: string;
  dmNotes?: string;
  // Динамическое состояние персонажа (обновляется по ходу игры).
  hp?: number;
  maxHp?: number;
  conditions?: string[];
  inventory?: string[];
  abilities?: CharacterAbility[];
  gold?: number;
  xp?: number;
  location?: string;
  updatedAt?: string;
  createdAt: string;
}

/** Патч динамического состояния персонажа (для updateCharacter). */
export interface CharacterStatePatch {
  level?: number;
  hp?: number;
  maxHp?: number;
  conditions?: string[];
  inventory?: string[];
  abilities?: CharacterAbility[];
  gold?: number;
  xp?: number;
  location?: string;
}

/** Аддитивный патч для grantCharacter: значения прибавляются, а не заменяются. */
export interface CharacterGrantPatch {
  inventory?: string[];
  abilities?: CharacterAbility[];
  gold?: number;
  xp?: number;
  conditions?: string[];
}

/** Статус NPC в мире кампании. */
export type NpcStatus = "alive" | "dead" | "unknown";

/** Отношение NPC к одному персонажу/игроку: шкала -5 (враг) .. +5 (союзник). */
export interface NpcRelationship {
  attitude: number;
  notes?: string;
}

export interface NpcProfile {
  id: string;
  campaignId: string;
  name: string;
  slug: string;
  /** Роль/описание NPC в мире (трактирщик, капитан стражи и т.п.). */
  role?: string;
  status: NpcStatus;
  location?: string;
  /** Отношения NPC к персонажам партии, ключ — имя персонажа. */
  relationships: Record<string, NpcRelationship>;
  firstSeenDay?: number;
  lastSeenDay?: number;
  createdAt: string;
  updatedAt?: string;
}

/** Статус квеста: предложен, принят, в работе, завершён, провален, брошен. */
export type QuestStatus = "offered" | "accepted" | "active" | "completed" | "failed" | "abandoned";

/** Сложность квеста — основа для расчёта наград по таблицам. */
export type QuestDifficulty = "easy" | "medium" | "hard";

/** Запланированная награда за квест: таблицы применяются для пустых полей. */
export interface QuestRewardPlan {
  /** XP каждому участнику за квест (без значения — таблица по сложности). */
  xp?: number;
  /** Золото на всю партию (без значения — таблица по сложности). */
  gold?: number;
  /** Предметы (выдаются каждому участнику партии). */
  items?: string[];
  /** Свободная часть: услуга NPC, репутация, сюжетный бонус. */
  note?: string;
}

export interface Quest {
  id: string;
  campaignId: string;
  slug: string;
  title: string;
  /** Слаг NPC-квестодателя (если квест выдал NPC). */
  giverNpcSlug?: string;
  objective: string;
  difficulty: QuestDifficulty;
  rewardPlan?: QuestRewardPlan;
  status: QuestStatus;
  /** Игровой день дедлайна: мир не ждёт. */
  deadlineDay?: number;
  /** День, когда квест появился. */
  createdDay: number;
  createdAt: string;
  updatedAt?: string;
}

/** Тип открытой нити: обещание, тайна, долг, незавершённое. */
export type ThreadKind = "promise" | "mystery" | "debt" | "unresolved";

export interface OpenThread {
  id: string;
  campaignId: string;
  text: string;
  kind: ThreadKind;
  status: "open" | "resolved";
  /** Квест, к которому относится нить (если применимо). */
  linkedQuestId?: string;
  dayOpened: number;
  dayClosed?: number;
  createdAt: string;
  updatedAt?: string;
}

export const memberRoleSchema = z.enum(["dm", "player"]);
export const campaignLengthSchema = z.enum(["short", "medium", "long"]);
export const npcStatusSchema = z.enum(["alive", "dead", "unknown"]);

/** Входные данные для создания квеста. */
export interface NewQuestInput {
  title: string;
  giverNpcSlug?: string;
  objective: string;
  difficulty: QuestDifficulty;
  rewardPlan?: QuestRewardPlan;
  status?: QuestStatus;
  deadlineDay?: number;
}

/** Патч квеста (для updateQuest). */
export interface QuestPatch {
  title?: string;
  giverNpcSlug?: string;
  objective?: string;
  difficulty?: QuestDifficulty;
  rewardPlan?: QuestRewardPlan;
  status?: QuestStatus;
  deadlineDay?: number;
}

/** Входные данные для открытой нити. */
export interface NewThreadInput {
  text: string;
  kind?: ThreadKind;
  linkedQuestId?: string;
}

/** Максимальный размер партии (используется и в lib/memory.ts). */
export const MAX_PARTY = 6;

/** Ошибка хранилища с человекочитаемым сообщением, пригодным для показа модели. */
export type StoreErrorCode =
  | "not_found"
  | "access_denied"
  | "duplicate"
  | "conflict"
  | "party_full";

export class StoreError extends Error {
  readonly code: StoreErrorCode;

  constructor(message: string, code: StoreErrorCode = "conflict") {
    super(message);
    this.code = code;
  }
}
