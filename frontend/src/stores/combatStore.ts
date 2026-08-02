import { create } from "zustand";

import type { CombatStateView } from "../types/combat";


interface CombatStore {
  combat: CombatStateView | null;
  setCombat: (combat: CombatStateView | null) => void;
  clear: () => void;
}


export const useCombatStore = create<CombatStore>((set) => ({
  combat: null,
  setCombat: (combat) => set({ combat }),
  clear: () => set({ combat: null }),
}));
