import { create } from "zustand";

import type { BoardView } from "../types/room";


interface BoardStore {
  board: BoardView | null;
  setBoard: (board: BoardView | null) => void;
  clear: () => void;
}


export const useBoardStore = create<BoardStore>((set) => ({
  board: null,
  setBoard: (board) => set({ board }),
  clear: () => set({ board: null }),
}));
