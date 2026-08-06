/**
 * Резолв кампании и проверка прав для тулов.
 *
 * Интерактивные вызовы (от Telegram-пользователя) резолвят кампанию по
 * привязанному чату из auth; автоматические вызовы (летописец) передают
 * campaignSlug явно, потому что у субагента нет Telegram-auth.
 */
import { resolveCallerIdentity, type CallerIdentity, type ToolSessionContext, resolveToolCallerIdentity } from "./session.ts";
import { campaignStore } from "./store.ts";
import { StoreError, type Campaign } from "./types.ts";

/**
 * Кампания по личности: точное совпадение чата/топика, а для обычных реплаев
 * вне форум-топика — кампания, привязанная к чату без топика. Telegram ставит
 * message_thread_id любому реплаю (= id родителя), поэтому точное сравнение
 * для реплаев в не-форумных группах не проходит.
 */
export function findCampaignForIdentity(identity: CallerIdentity): Campaign | undefined {
  if (!identity.chatId) return undefined;
  const exact = campaignStore.findByBoundChat(identity.chatId, identity.messageThreadId);
  if (exact) return exact;
  if (identity.messageThreadId === undefined) return undefined;
  return campaignStore.findByBoundChat(identity.chatId, undefined);
}

/** Кампания по явному id/slug либо по чату, привязанному к звонящему. */
export function resolveCampaign(auth: unknown, idOrSlug?: string): Campaign | undefined {
  if (idOrSlug) return campaignStore.getCampaign(idOrSlug);
  const identity = resolveCallerIdentity(auth);
  if (!identity) return undefined;
  return findCampaignForIdentity(identity);
}

/** Кампания для записи: явный slug доверенной автоматизации или роль dm по auth. */
export function resolveCampaignForWrite(auth: unknown, explicitIdOrSlug?: string): Campaign {
  if (explicitIdOrSlug) {
    const campaign = campaignStore.getCampaign(explicitIdOrSlug);
    if (!campaign) {
      throw new StoreError(`Кампания «${explicitIdOrSlug}» не найдена.`, "not_found");
    }
    return campaign;
  }
  const identity = resolveCallerIdentity(auth);
  if (!identity) {
    throw new StoreError("Не удалось определить, кто вы.", "access_denied");
  }
  const campaign = resolveCampaign(auth);
  if (!campaign) {
    throw new StoreError("В этом чате нет привязанной кампании.", "not_found");
  }
  const member = campaign.members.find((entry) => entry.userId === identity.userId);
  if (!member || member.role !== "dm") {
    throw new StoreError("Это действие доступно только администратору (dm) кампании.", "access_denied");
  }
  return campaign;
}

/** Личность звонящего или ошибка доступа. */
export function requireIdentity(auth: unknown): CallerIdentity {
  const identity = resolveCallerIdentity(auth);
  if (!identity) {
    throw new StoreError("Не удалось определить, кто вы.", "access_denied");
  }
  return identity;
}

export interface CharacterActionAccess {
  allowed: boolean;
  /** Причина отказа для показа модели/игроку (когда allowed = false). */
  reason?: string;
}

/**
 * Может ли звонящий действовать от имени персонажа (броски, бой).
 *
 * Владелец персонажа и DM кампании могут всегда. Чужой персонаж — отказ.
 * Без личности (автоматизация/субагент), без привязанной кампании или без
 * сохранённого листа персонажа проверка пропускается: играть это не должно
 * ломаться там, где владение нельзя установить.
 */
export function canActForCharacter(ctx: ToolSessionContext | undefined, characterName: string): CharacterActionAccess {
  const identity = resolveToolCallerIdentity(ctx);
  if (!identity) return { allowed: true };
  const campaign = findCampaignForIdentity(identity);
  if (!campaign) return { allowed: true };
  const member = campaign.members.find((entry) => entry.userId === identity.userId);
  if (member?.role === "dm") return { allowed: true };
  const needle = characterName.trim().toLowerCase();
  const sheet = campaignStore.listCharacters(campaign.id).find(
    (candidate) => candidate.name.trim().toLowerCase() === needle,
  );
  if (!sheet || !sheet.ownerUserId) return { allowed: true };
  if (sheet.ownerUserId === identity.userId) return { allowed: true };
  return {
    allowed: false,
    reason:
      `Персонаж «${sheet.name}» принадлежит другому игроку. ` +
      "Кидать проверки и действовать за чужого персонажа может только его владелец или DM.",
  };
}
