/**
 * Резолв кампании и проверка прав для тулов.
 *
 * Интерактивные вызовы (от Telegram-пользователя) резолвят кампанию по
 * привязанному чату из auth; автоматические вызовы (летописец) передают
 * campaignSlug явно, потому что у субагента нет Telegram-auth.
 */
import { resolveCallerIdentity, type CallerIdentity } from "./session.ts";
import { campaignStore } from "./store.ts";
import { StoreError, type Campaign } from "./types.ts";

/** Кампания по явному id/slug либо по чату, привязанному к звонящему. */
export function resolveCampaign(auth: unknown, idOrSlug?: string): Campaign | undefined {
  if (idOrSlug) return campaignStore.getCampaign(idOrSlug);
  const identity = resolveCallerIdentity(auth);
  if (!identity?.chatId) return undefined;
  return campaignStore.findByBoundChat(identity.chatId, identity.messageThreadId);
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
