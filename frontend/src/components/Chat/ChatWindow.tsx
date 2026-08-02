import MessageInput from "./MessageInput";
import MessageList from "./MessageList";


export default function ChatWindow({ onSend }: { onSend: (text: string) => boolean }) {
  return (
    <div className="mx-auto flex w-full max-w-5xl flex-1 flex-col overflow-hidden">
      <MessageList />
      <MessageInput onSend={onSend} />
    </div>
  );
}
