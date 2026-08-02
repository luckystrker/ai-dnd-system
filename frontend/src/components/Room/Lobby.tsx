import { FormEvent, useState } from "react";

import { useRoomStore } from "../../stores/roomStore";


interface LobbyProps {
  onJoin: (code: string) => void;
}


export default function Lobby({ onJoin }: LobbyProps) {
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [system, setSystem] = useState("dnd5e");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState<"create" | "join" | null>(null);
  const setRoom = useRoomStore((state) => state.setRoom);

  const createRoom = async () => {
    const displayName = name.trim();
    if (!displayName) {
      setError("Enter a display name first.");
      return;
    }

    setError("");
    setLoading("create");
    try {
      const response = await fetch("/room/create", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ display_name: displayName, system }),
      });
      const data: { code?: string; player_token?: string; detail?: string } = await response.json();
      if (!response.ok || !data.code || !data.player_token) {
        throw new Error(data.detail || "Could not create the room.");
      }
      localStorage.setItem("player_token", data.player_token);
      setRoom(data.code, data.player_token, displayName);
      onJoin(data.code);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "Could not create the room.");
    } finally {
      setLoading(null);
    }
  };

  const joinRoom = async () => {
    const displayName = name.trim();
    const roomCode = code.trim().toUpperCase();
    if (!displayName) {
      setError("Enter a display name first.");
      return;
    }
    if (roomCode.length !== 6) {
      setError("Enter a six-character room code.");
      return;
    }

    setError("");
    setLoading("join");
    try {
      const playerToken = localStorage.getItem("player_token") || undefined;
      const response = await fetch("/room/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          code: roomCode,
          display_name: displayName,
          player_token: playerToken,
        }),
      });
      const data: { room_id?: string; player_token?: string; detail?: string } = await response.json();
      if (!response.ok || !data.player_token) {
        throw new Error(data.detail || "Room not found.");
      }
      localStorage.setItem("player_token", data.player_token);
      setRoom(roomCode, data.player_token, displayName);
      onJoin(roomCode);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "Could not join the room.");
    } finally {
      setLoading(null);
    }
  };

  const handleJoinSubmit = (event: FormEvent) => {
    event.preventDefault();
    void joinRoom();
  };

  return (
    <main className="min-h-screen overflow-hidden bg-[#090d18] px-5 py-10 text-slate-100 sm:px-8">
      <div className="mx-auto flex min-h-[calc(100vh-5rem)] max-w-6xl items-center justify-center">
        <div className="grid w-full overflow-hidden rounded-3xl border border-white/10 bg-slate-900/80 shadow-2xl shadow-black/40 lg:grid-cols-[1.1fr_0.9fr]">
          <section className="relative hidden min-h-[560px] overflow-hidden border-r border-white/10 bg-[radial-gradient(circle_at_20%_15%,rgba(245,158,11,0.2),transparent_30%),radial-gradient(circle_at_80%_80%,rgba(79,70,229,0.22),transparent_35%),#101629] p-10 lg:flex lg:flex-col lg:justify-between">
            <div className="absolute -right-24 top-20 h-72 w-72 rounded-full border border-amber-300/10" />
            <div className="absolute -right-10 top-34 h-44 w-44 rounded-full border border-amber-300/10" />
            <div>
              <p className="mb-5 text-xs font-semibold uppercase tracking-[0.35em] text-amber-300">A living campaign</p>
              <h1 className="max-w-lg text-5xl font-semibold leading-[1.05] tracking-tight text-white">
                Your next story starts in the dark.
              </h1>
              <p className="mt-6 max-w-md text-base leading-7 text-slate-300">
                Gather your party, choose an action, and let the Dungeon Master shape the consequences in real time.
              </p>
            </div>
            <div className="flex items-center gap-3 text-sm text-slate-400">
              <span className="h-2 w-2 rounded-full bg-emerald-400 shadow-[0_0_12px_rgba(52,211,153,0.8)]" />
              Text adventures for up to six players
            </div>
          </section>

          <section className="flex min-h-[560px] flex-col justify-center p-6 sm:p-10">
            <div className="mb-10 lg:hidden">
              <p className="text-xs font-semibold uppercase tracking-[0.3em] text-amber-300">TTRPG Agent</p>
              <h1 className="mt-3 text-3xl font-semibold tracking-tight text-white">Enter the adventure.</h1>
            </div>
            <div className="mx-auto w-full max-w-md">
              <div className="mb-8">
                <p className="text-sm font-medium text-slate-400">Start a new session</p>
                <h2 className="mt-2 text-2xl font-semibold text-white">Open the table</h2>
              </div>

              <form onSubmit={handleJoinSubmit} className="space-y-4">
                <label className="block">
                  <span className="mb-2 block text-sm text-slate-300">Your name</span>
                  <input
                    className="w-full rounded-xl border border-white/10 bg-slate-950/70 px-4 py-3.5 text-white outline-none transition placeholder:text-slate-600 focus:border-amber-400/70 focus:ring-2 focus:ring-amber-400/10"
                    placeholder="Wandering hero"
                    maxLength={50}
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    autoFocus
                  />
                </label>

                <label className="block">
                  <span className="mb-2 block text-sm text-slate-300">Game system</span>
                  <select
                    className="w-full rounded-xl border border-white/10 bg-slate-950/70 px-4 py-3.5 text-white outline-none transition focus:border-indigo-400/70 focus:ring-2 focus:ring-indigo-400/10"
                    value={system}
                    onChange={(event) => setSystem(event.target.value)}
                  >
                    <option value="dnd5e">D&D 5e casual</option>
                    <option value="story">System-neutral story</option>
                  </select>
                </label>

                <button
                  type="button"
                  onClick={() => void createRoom()}
                  disabled={loading !== null}
                  className="w-full rounded-xl bg-amber-400 px-4 py-3.5 font-semibold text-slate-950 transition hover:bg-amber-300 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {loading === "create" ? "Creating..." : "Create a new room"}
                </button>

                <div className="flex items-center gap-3 py-2 text-xs uppercase tracking-[0.2em] text-slate-600">
                  <span className="h-px flex-1 bg-white/10" />
                  or join one
                  <span className="h-px flex-1 bg-white/10" />
                </div>

                <div className="flex flex-col gap-3 sm:flex-row">
                  <input
                    className="min-w-0 flex-1 rounded-xl border border-white/10 bg-slate-950/70 px-4 py-3.5 uppercase tracking-[0.25em] text-white outline-none transition placeholder:normal-case placeholder:tracking-normal placeholder:text-slate-600 focus:border-indigo-400/70 focus:ring-2 focus:ring-indigo-400/10"
                    placeholder="Room code"
                    maxLength={6}
                    value={code}
                    onChange={(event) => setCode(event.target.value.toUpperCase())}
                  />
                  <button
                    type="submit"
                    disabled={loading !== null}
                    className="rounded-xl border border-white/15 px-5 py-3.5 font-semibold text-slate-200 transition hover:border-white/30 hover:bg-white/5 disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    {loading === "join" ? "Joining..." : "Join"}
                  </button>
                </div>
              </form>

              {error && <p className="mt-5 rounded-xl border border-rose-400/20 bg-rose-400/10 px-4 py-3 text-sm text-rose-200">{error}</p>}
              <p className="mt-8 text-center text-xs leading-5 text-slate-600">No account required. Your reconnect token stays in this browser.</p>
            </div>
          </section>
        </div>
      </div>
    </main>
  );
}
