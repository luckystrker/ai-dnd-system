import { create } from "zustand";

import type { CharacterView } from "../types/room";


interface CharacterStore {
  characters: CharacterView[];
  setCharacters: (characters: CharacterView[]) => void;
  clear: () => void;
}


export const useCharacterStore = create<CharacterStore>((set) => ({
  characters: [],
  setCharacters: (characters) => set({ characters }),
  clear: () => set({ characters: [] }),
}));
