import { useEffect, useRef } from "react";

import { useChatStore } from "../../stores/chatStore";


export default function MessageList() {
  const messages = useChatStore((state) => state.messages);
  const streamingText = useChatStore((state) => state.streamingText);
  const isStreaming = useChatStore((state) => state.isStreaming);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streamingText]);

  return (
    <div className="flex-1 overflow-y-auto px-5 py-6 sm:px-8">
      {messages.length === 0 && !isStreaming && (
        <div className="flex min-h-[55vh] flex-col items-center justify-center text-center">
          <div className="mb-5 flex h-14 w-14 items-center justify-center rounded-2xl border border-amber-300/20 bg-amber-300/10 text-2xl text-amber-300">+</div>
          <p className="text-sm font-medium text-slate-300">The table is waiting.</p>
          <p className="mt-2 max-w-sm text-sm leading-6 text-slate-500">Describe what your character does. The Dungeon Master will reveal what happens next.</p>
        </div>
      )}

      <div className="space-y-5">
        {messages.map((message) => (
          <div key={message.id} className={message.sender === "player" ? "flex justify-end" : "flex justify-start"}>
            {message.type === "dice_roll" ? (
              <p className="rounded-lg border border-violet-400/20 bg-violet-400/10 px-3 py-2 text-center font-mono text-xs text-violet-200">{message.text}</p>
            ) : message.sender === "system" ? (
              <p className="text-center text-xs italic text-slate-500">{message.text}</p>
            ) : (
              <div className={`max-w-[88%] rounded-2xl px-4 py-3 sm:max-w-[75%] ${message.sender === "player" ? "rounded-br-md bg-indigo-500/20 text-indigo-50" : "rounded-bl-md border border-white/10 bg-slate-800/80 text-slate-100"}`}>
                {message.senderName && <p className="mb-1.5 text-[11px] font-semibold uppercase tracking-[0.16em] text-amber-300/80">{message.sender === "dm" ? message.senderName : "You"}</p>}
                <p className="whitespace-pre-wrap text-sm leading-6">{message.text}</p>
              </div>
            )}
          </div>
        ))}

        {isStreaming && (
          <div className="flex justify-start">
            <div className="max-w-[88%] rounded-2xl rounded-bl-md border border-white/10 bg-slate-800/80 px-4 py-3 text-slate-100 sm:max-w-[75%]">
              <p className="mb-1.5 text-[11px] font-semibold uppercase tracking-[0.16em] text-amber-300/80">Dungeon Master</p>
              <p className="whitespace-pre-wrap text-sm leading-6">{streamingText}<span className="ml-1 inline-block animate-pulse text-amber-300">|</span></p>
            </div>
          </div>
        )}
      </div>
      <div ref={bottomRef} />
    </div>
  );
}
