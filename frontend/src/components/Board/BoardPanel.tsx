import { useBoardStore } from "../../stores/boardStore";


interface BoardPanelProps {
  onMove: (tokenId: string, x: number, y: number) => boolean;
}


export default function BoardPanel({ onMove }: BoardPanelProps) {
  const board = useBoardStore((state) => state.board);
  const tokens = board ? Object.entries(board.tokens ?? {}) : [];

  const handleBoardClick = (event: React.MouseEvent<HTMLDivElement>) => {
    if (!tokens.length) {
      return;
    }
    const tokenId = tokens[0][0];
    const bounds = event.currentTarget.getBoundingClientRect();
    const x = Math.max(0, Math.min(1, (event.clientX - bounds.left) / bounds.width));
    const y = Math.max(0, Math.min(1, (event.clientY - bounds.top) / bounds.height));
    onMove(tokenId, x, y);
  };

  return (
    <section className="border-b border-white/10 bg-slate-950/40 px-5 py-5 sm:px-8">
      <div className="mx-auto w-full max-w-5xl">
        <div className="mb-3 flex items-end justify-between gap-3">
          <div>
            <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-indigo-300">Visual board</p>
            <h2 className="mt-1 text-sm font-medium text-white">The scene</h2>
          </div>
          <p className="text-[11px] text-slate-500">Click the board to move the first token</p>
        </div>
        <div
          className="relative aspect-[16/8] overflow-hidden rounded-2xl border border-white/10 bg-[linear-gradient(rgba(148,163,184,0.08)_1px,transparent_1px),linear-gradient(90deg,rgba(148,163,184,0.08)_1px,transparent_1px),#111827] bg-[size:10%_20%]"
          style={board?.map_url ? { backgroundImage: `linear-gradient(rgba(9,13,24,0.35),rgba(9,13,24,0.35)), url(${board.map_url})`, backgroundSize: "cover", backgroundPosition: "center" } : undefined}
          onClick={handleBoardClick}
        >
          {tokens.map(([id, token]) => (
            <div
              key={id}
              className={`absolute flex h-9 w-9 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border-2 text-[10px] font-semibold shadow-lg ${token.kind === "enemy" ? "border-rose-300 bg-rose-500/80 text-white" : "border-indigo-200 bg-indigo-500/90 text-white"}`}
              style={{ left: `${token.x * 100}%`, top: `${token.y * 100}%` }}
              title={token.label || id}
            >
              {(token.label || id).slice(0, 2).toUpperCase()}
            </div>
          ))}
          {!tokens.length && <p className="absolute inset-0 flex items-center justify-center text-sm text-slate-500">A map will appear when the scene is prepared.</p>}
        </div>
      </div>
    </section>
  );
}
