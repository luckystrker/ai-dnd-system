export interface CombatantView {
  id: string;
  name: string;
  side: "party" | "enemy" | "neutral";
  hp: number;
  max_hp: number;
  ac: number;
  initiative: number;
  conditions: string[];
  is_boss: boolean;
}

export interface CombatStateView {
  combat_id: string;
  status: "active" | "victory" | "defeat" | "ended";
  round: number;
  turn_index: number;
  turn_order: string[];
  combatants: Record<string, CombatantView>;
}
