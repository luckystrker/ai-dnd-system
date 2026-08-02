import { useEffect, useState } from "react";

import { useWebSocket } from "../../hooks/useWebSocket";
import { useChatStore } from "../../stores/chatStore";
import { useCombatStore } from "../../stores/combatStore";
import { useBoardStore } from "../../stores/boardStore";
import { useCharacterStore } from "../../stores/characterStore";
import { useRoomStore } from "../../stores/roomStore";
import BoardPanel from "../Board/BoardPanel";
import ChatWindow from "../Chat/ChatWindow";
import CharacterSheet from "../CharacterSheet/CharacterSheet";
import VoiceControls from "../Voice/VoiceControls";
import type { BoardView, CharacterView } from "../../types/room";
import CombatPanel from "../Combat/CombatPanel";


interface RoomViewProps {
  code: string;
  onLeave: () => void;
}


export default function RoomView({ code, onLeave }: RoomViewProps) {
  const { sendBoardMove, sendMessage, sendVoiceAudio } = useWebSocket(code);
  const connected = useRoomStore((state) => state.connected);
  const resetRoom = useRoomStore((state) => state.reset);
  const clearChat = useChatStore((state) => state.clear);
  const clearCombat = useCombatStore((state) => state.clear);
  const setBoard = useBoardStore((state) => state.setBoard);
  const clearBoard = useBoardStore((state) => state.clear);
  const setCharacters = useCharacterStore((state) => state.setCharacters);
  const clearCharacters = useCharacterStore((state) => state.clear);
  const [mode, setMode] = useState<"chat" | "visual">("chat");

  useEffect(() => {
    clearChat();
    clearCombat();
    clearBoard();
    clearCharacters();
  }, [clearBoard, clearCharacters, clearChat, clearCombat, code]);

  useEffect(() => {
    let active = true;
    void fetch(`/room/${encodeURIComponent(code)}/snapshot`)
      .then((response) => (response.ok ? response.json() : null))
      .then((snapshot: unknown) => {
        if (!active || !snapshot || typeof snapshot !== "object") {
          return;
        }
        const data = snapshot as { game_state?: { board?: unknown }; characters?: unknown };
        if (data.game_state?.board && typeof data.game_state.board === "object") {
          setBoard(data.game_state.board as BoardView);
        }
        if (Array.isArray(data.characters)) {
          setCharacters(data.characters as CharacterView[]);
        }
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [code, setBoard, setCharacters]);

  const handleLeave = () => {
    resetRoom();
    clearChat();
    clearCombat();
    clearBoard();
    clearCharacters();
    onLeave();
  };

  return (
    <main className="flex min-h-screen flex-col bg-[#090d18] text-slate-100">
      <header className="border-b border-white/10 bg-slate-950/70 px-5 py-4 sm:px-8">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-between gap-4">
          <div className="min-w-0">
            <p className="text-xs font-semibold uppercase tracking-[0.28em] text-amber-300">TTRPG Agent</p>
            <div className="mt-1 flex items-center gap-3">
              <h1 className="truncate text-lg font-semibold text-white">Room {code}</h1>
              <span className={`rounded-full px-2.5 py-1 text-[11px] font-medium ${connected ? "bg-emerald-400/10 text-emerald-300" : "bg-rose-400/10 text-rose-300"}`}>
                <span className="mr-1.5">{connected ? "*" : "-"}</span>
                {connected ? "Connected" : "Reconnecting"}
              </span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex rounded-lg border border-white/10 bg-slate-900 p-1 text-xs">
              <button onClick={() => setMode("chat")} className={`rounded-md px-2.5 py-1.5 ${mode === "chat" ? "bg-white/10 text-white" : "text-slate-500"}`}>Chat</button>
              <button onClick={() => setMode("visual")} className={`rounded-md px-2.5 py-1.5 ${mode === "visual" ? "bg-white/10 text-white" : "text-slate-500"}`}>Visual</button>
            </div>
            <VoiceControls onAudio={sendVoiceAudio} />
            <button onClick={handleLeave} className="shrink-0 rounded-lg px-3 py-2 text-sm text-slate-400 transition hover:bg-white/5 hover:text-white">Leave table</button>
          </div>
        </div>
      </header>
      <CombatPanel />
      {mode === "visual" && <><BoardPanel onMove={sendBoardMove} /><CharacterSheet /></>}
      <ChatWindow onSend={sendMessage} />
    </main>
  );
}
