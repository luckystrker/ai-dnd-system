import { create } from "zustand";


interface RoomState {
  roomCode: string | null;
  playerToken: string | null;
  playerName: string;
  players: string[];
  connected: boolean;
  setRoom: (code: string, token: string, name: string) => void;
  setConnected: (value: boolean) => void;
  setPlayers: (players: string[]) => void;
  reset: () => void;
}


export const useRoomStore = create<RoomState>((set) => ({
  roomCode: null,
  playerToken: null,
  playerName: "",
  players: [],
  connected: false,
  setRoom: (roomCode, playerToken, playerName) =>
    set({ roomCode, playerToken, playerName }),
  setConnected: (connected) => set({ connected }),
  setPlayers: (players) => set({ players }),
  reset: () =>
    set({
      roomCode: null,
      playerToken: null,
      playerName: "",
      players: [],
      connected: false,
    }),
}));
