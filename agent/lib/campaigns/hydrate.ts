import { gameState } from "../memory.ts";
import type { Campaign, CharacterSheet } from "./types.ts";
import { loadCombatState } from "./combat-store.ts";
import { emptyCombatOrder } from "../engine/combat.ts";

/**
 * Наполняет состояние игровой сессии из сохранённой кампании:
 * так ключевые факты кампании и состав партии оказываются в контексте игры.
 * Если в базе кампаний есть сохранённый бой (combat_snapshot), он
 * восстанавливается — порядок инициативы, враги и HP партии в бою
 * переживают рестарт сессии.
 */
export function hydrateGameState(campaign: Campaign, characters: CharacterSheet[]): void {
  const party = characters.map((sheet) => ({
    id: sheet.id,
    name: sheet.name,
    ownerUserId: sheet.ownerUserId,
    stats: sheet.stats,
    level: sheet.level,
    hp: sheet.hp,
    maxHp: sheet.maxHp,
  }));

  const saved = loadCombatState(campaign.slug);
  gameState.update((state) => {
    let nextParty = party;
    // Дефолты: durable-состояние старых сессий может не иметь полей combat/enemies
    // (поля GameState добавлялись со временем) — не персистим undefined обратно.
    let combat = state.combat ?? emptyCombatOrder();
    let enemies = Array.isArray(state.enemies) ? state.enemies : [];
    if (saved) {
      combat = saved.combat;
      enemies = saved.enemies;
      // HP партии, накопленный в бою, берём из боевого порядка (туда его кладёт
      // initiative/combat). Иначе партия стартует с HP из листа.
      const hpById = new Map(saved.combat.order.filter((e) => e.side === "party").map((e) => [e.id, e.hp]));
      nextParty = party.map((member) =>
        hpById.has(member.id) && hpById.get(member.id) !== undefined
          ? { ...member, hp: hpById.get(member.id) }
          : member,
      );
    }
    return {
      ...state,
      started: true,
      campaignId: campaign.id,
      scene: state.scene ? state.scene : campaign.openingScene ?? "",
      party: nextParty,
      combat,
      enemies,
    };
  });
}
