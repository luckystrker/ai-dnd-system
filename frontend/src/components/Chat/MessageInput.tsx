import { FormEvent, useState } from "react";


export default function MessageInput({ onSend }: { onSend: (text: string) => boolean }) {
  const [text, setText] = useState("");

  const handleSend = (event?: FormEvent) => {
    event?.preventDefault();
    const value = text.trim();
    if (!value || !onSend(value)) {
      return;
    }
    setText("");
  };

  return (
    <form onSubmit={handleSend} className="border-t border-white/10 bg-slate-950/70 px-5 py-4 sm:px-8">
      <div className="flex items-end gap-3 rounded-2xl border border-white/10 bg-slate-900 p-2 shadow-lg shadow-black/10 focus-within:border-indigo-400/50">
        <textarea
          className="max-h-32 min-h-11 flex-1 resize-none bg-transparent px-3 py-2.5 text-sm leading-5 text-white outline-none placeholder:text-slate-600"
          placeholder="What do you do?"
          rows={1}
          maxLength={4000}
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              handleSend();
            }
          }}
        />
        <button type="submit" disabled={!text.trim()} className="rounded-xl bg-amber-400 px-4 py-2.5 text-sm font-semibold text-slate-950 transition hover:bg-amber-300 disabled:cursor-not-allowed disabled:opacity-30">
          Send
        </button>
      </div>
      <p className="mt-2 px-2 text-[11px] text-slate-600">Press Enter to send. Shift + Enter for a new line.</p>
    </form>
  );
}
