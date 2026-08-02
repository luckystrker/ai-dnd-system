import { useState } from "react";

import Lobby from "./components/Room/Lobby";
import RoomView from "./components/Room/RoomView";


export default function App() {
  const [roomCode, setRoomCode] = useState<string | null>(null);

  if (!roomCode) {
    return <Lobby onJoin={setRoomCode} />;
  }

  return <RoomView code={roomCode} onLeave={() => setRoomCode(null)} />;
}
