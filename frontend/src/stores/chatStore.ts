import { create } from "zustand";

import type { ChatMessage } from "../types/messages";


interface ChatState {
  messages: ChatMessage[];
  streamingText: string;
  isStreaming: boolean;
  addMessage: (message: ChatMessage) => void;
  appendToken: (token: string) => void;
  finalizeStream: (fullText?: string) => void;
  clear: () => void;
}


export const useChatStore = create<ChatState>((set) => ({
  messages: [],
  streamingText: "",
  isStreaming: false,
  addMessage: (message) => set((state) => ({ messages: [...state.messages, message] })),
  appendToken: (token) =>
    set((state) => ({
      streamingText: state.streamingText + token,
      isStreaming: true,
    })),
  finalizeStream: (fullText) =>
    set((state) => {
      const text = fullText || state.streamingText;
      if (!text) {
        return { streamingText: "", isStreaming: false };
      }
      const message: ChatMessage = {
        id: crypto.randomUUID(),
        sender: "dm",
        senderName: "Dungeon Master",
        text,
        type: "message",
        timestamp: Date.now(),
      };
      return {
        messages: [...state.messages, message],
        streamingText: "",
        isStreaming: false,
      };
    }),
  clear: () => set({ messages: [], streamingText: "", isStreaming: false }),
}));
