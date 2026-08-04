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
  dmNotes?: string;
  // Динамическое состояние персонажа (обновляется по ходу игры).
  hp?: number;
  maxHp?: number;
  conditions?: string[];
  inventory?: string[];
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
  gold?: number;
  xp?: number;
  location?: string;
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

export const memberRoleSchema = z.enum(["dm", "player"]);
export const campaignLengthSchema = z.enum(["short", "medium", "long"]);
export const npcStatusSchema = z.enum(["alive", "dead", "unknown"]);

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
