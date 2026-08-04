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
  createdAt: string;
}

export const memberRoleSchema = z.enum(["dm", "player"]);
export const campaignLengthSchema = z.enum(["short", "medium", "long"]);

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
