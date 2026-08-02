import { useEffect, useRef } from "react";

import { useChatStore } from "../stores/chatStore";
import { useCombatStore } from "../stores/combatStore";
import { useBoardStore } from "../stores/boardStore";
import { useRoomStore } from "../stores/roomStore";
import type { ChatMessage, WSMessage } from "../types/messages";
import type { CombatStateView } from "../types/combat";
import type { BoardView } from "../types/room";


const MAX_RECONNECT_ATTEMPTS = 3;


function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}


function asString(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}


function newMessage(
  sender: ChatMessage["sender"],
  text: string,
  type: ChatMessage["type"] = "message",
  senderName?: string,
): ChatMessage {
  return {
    id: crypto.randomUUID(),
    sender,
    senderName,
    text,
    type,
    timestamp: Date.now(),
  };
}


export function useWebSocket(code: string | null) {
  const websocketRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<number | null>(null);
  const reconnectAttemptsRef = useRef(0);
  const addMessage = useChatStore((state) => state.addMessage);
  const appendToken = useChatStore((state) => state.appendToken);
  const finalizeStream = useChatStore((state) => state.finalizeStream);
  const playerToken = useRoomStore((state) => state.playerToken);
  const setConnected = useRoomStore((state) => state.setConnected);
  const setCombat = useCombatStore((state) => state.setCombat);
  const setBoard = useBoardStore((state) => state.setBoard);

  useEffect(() => {
    if (!code) {
      return undefined;
    }

    let disposed = false;

    const connect = () => {
      if (disposed) {
        return;
      }

      const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
      const query = playerToken ? `?token=${encodeURIComponent(playerToken)}` : "";
      const url = `${protocol}//${window.location.host}/ws/room/${encodeURIComponent(code)}${query}`;
      const websocket = new WebSocket(url);
      websocketRef.current = websocket;

      websocket.onopen = () => {
        reconnectAttemptsRef.current = 0;
        setConnected(true);
      };

      websocket.onclose = () => {
        setConnected(false);
        if (disposed || reconnectAttemptsRef.current >= MAX_RECONNECT_ATTEMPTS) {
          return;
        }
        const delay = 500 * 2 ** reconnectAttemptsRef.current;
        reconnectAttemptsRef.current += 1;
        reconnectTimerRef.current = window.setTimeout(connect, delay);
      };

      websocket.onerror = () => {
        websocket.close();
      };

      websocket.onmessage = (event) => {
        let message: WSMessage;
        try {
          const parsed: unknown = JSON.parse(event.data);
          if (!isRecord(parsed) || typeof parsed.type !== "string") {
            return;
          }
          message = {
            type: parsed.type,
            payload: isRecord(parsed.payload) ? parsed.payload : {},
          };
        } catch {
          addMessage(newMessage("system", "Received an invalid server message."));
          return;
        }

        const payload = message.payload;
        switch (message.type) {
          case "dm_token": {
            const token = asString(payload.token);
            if (token) {
              appendToken(token);
            }
            break;
          }
          case "dm_complete":
            finalizeStream(asString(payload.full_text));
            break;
          case "player_message": {
            const text = asString(payload.text);
            if (text) {
              addMessage(newMessage("player", text, "message", asString(payload.sender)));
            }
            break;
          }
          case "voice_transcript": {
            const text = asString(payload.text);
            if (text) {
              addMessage(newMessage("player", text, "message", "Voice"));
            }
            break;
          }
          case "voice_audio": {
            const data = asString(payload.data);
            const mimeType = asString(payload.mime_type) || "audio/mpeg";
            if (data) {
              void new Audio(`data:${mimeType};base64,${data}`).play().catch(() => undefined);
            }
            break;
          }
          case "dice_roll": {
            const result = asString(payload.result);
            if (result) {
              addMessage(newMessage("system", result, "dice_roll"));
            }
            break;
          }
          case "player_joined": {
            const name = asString(payload.name) || "A player";
            addMessage(newMessage("system", `${name} joined the room`));
            break;
          }
          case "error": {
            const error = asString(payload.message) || "The server reported an error.";
            finalizeStream();
            addMessage(newMessage("system", error));
            break;
          }
          case "combat_started":
          case "combat_action":
          case "state_updated": {
            const state = isRecord(payload.state) ? payload.state : undefined;
            const combat = state && isRecord(state.combat) ? state.combat : undefined;
            if (combat) {
              setCombat(combat as unknown as CombatStateView);
            }
            break;
          }
          case "combat_ended":
            break;
          case "board_updated": {
            const state = isRecord(payload.state) ? payload.state : undefined;
            const board = state && isRecord(state.board) ? state.board : undefined;
            if (board) {
              setBoard(board as unknown as BoardView);
            }
            break;
          }
          case "pong":
            break;
          default:
            break;
        }
      };
    };

    connect();
    const pingInterval = window.setInterval(() => {
      if (websocketRef.current?.readyState === WebSocket.OPEN) {
        websocketRef.current.send(JSON.stringify({ type: "ping", payload: {} }));
      }
    }, 30_000);

    return () => {
      disposed = true;
      window.clearInterval(pingInterval);
      if (reconnectTimerRef.current !== null) {
        window.clearTimeout(reconnectTimerRef.current);
      }
      websocketRef.current?.close();
      websocketRef.current = null;
      setConnected(false);
    };
  }, [addMessage, appendToken, code, finalizeStream, playerToken, setBoard, setCombat, setConnected]);

  const sendMessage = (text: string) => {
    if (websocketRef.current?.readyState !== WebSocket.OPEN) {
      return false;
    }
    websocketRef.current.send(
      JSON.stringify({ type: "player_message", payload: { text } }),
    );
    return true;
  };

  const sendBoardMove = (tokenId: string, x: number, y: number) => {
    if (websocketRef.current?.readyState !== WebSocket.OPEN) {
      return false;
    }
    websocketRef.current.send(
      JSON.stringify({ type: "board_move", payload: { token_id: tokenId, x, y } }),
    );
    return true;
  };

  const sendVoiceAudio = async (audio: Blob) => {
    if (websocketRef.current?.readyState !== WebSocket.OPEN) {
      return false;
    }
    const bytes = new Uint8Array(await audio.arrayBuffer());
    let binary = "";
    const chunkSize = 0x8000;
    for (let index = 0; index < bytes.length; index += chunkSize) {
      binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize));
    }
    websocketRef.current.send(
      JSON.stringify({
        type: "voice_input",
        payload: { audio_base64: btoa(binary), mime_type: audio.type || "audio/webm" },
      }),
    );
    return true;
  };

  return { sendBoardMove, sendMessage, sendVoiceAudio };
}
