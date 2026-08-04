import { gameState } from "../memory.ts";
import type { Campaign, CharacterSheet } from "./types.ts";

/**
 * Наполняет состояние игровой сессии из сохранённой кампании:
 * так ключевые факты кампании и состав партии оказываются в контексте игры.
 */
export function hydrateGameState(campaign: Campaign, characters: CharacterSheet[]): void {
  gameState.update((state) => ({
    ...state,
    started: true,
    campaignId: campaign.id,
    scene: state.scene ? state.scene : campaign.openingScene ?? "",
    party: characters.map((sheet) => ({
      id: sheet.id,
      name: sheet.name,
      stats: sheet.stats,
    })),
  }));
}
