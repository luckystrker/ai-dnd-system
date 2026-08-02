export interface WSMessage {
  type: string;
  payload: Record<string, unknown>;
}

export type ChatSender = "player" | "dm" | "system";

export interface ChatMessage {
  id: string;
  sender: ChatSender;
  senderName?: string;
  text: string;
  type: "message" | "dice_roll";
  timestamp: number;
}
