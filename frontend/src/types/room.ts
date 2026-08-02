export interface BoardTokenView {
  x: number;
  y: number;
  label: string;
  kind: string;
}

export interface BoardView {
  map_url?: string;
  tokens: Record<string, BoardTokenView>;
}

export interface CharacterView {
  id: string;
  room_id: string;
  player_id: string;
  name: string;
  stats: Record<string, unknown>;
  inventory: unknown[];
}
