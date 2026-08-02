import { useCharacterStore } from "../../stores/characterStore";


export default function CharacterSheet() {
  const characters = useCharacterStore((state) => state.characters);

  return (
    <aside className="border-b border-white/10 bg-slate-950/30 px-5 py-5 sm:px-8">
      <div className="mx-auto w-full max-w-5xl">
        <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-amber-300">Party sheets</p>
        {characters.length === 0 ? (
          <p className="mt-2 text-sm text-slate-500">Create a character to see stats and inventory here.</p>
        ) : (
          <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {characters.map((character) => {
              const hp = typeof character.stats.hp === "number" ? character.stats.hp : undefined;
              const maxHp = typeof character.stats.max_hp === "number" ? character.stats.max_hp : undefined;
              return (
                <div key={character.id} className="rounded-xl border border-white/10 bg-slate-900/70 p-4">
                  <div className="flex items-center justify-between gap-3">
                    <h2 className="truncate text-sm font-semibold text-white">{character.name}</h2>
                    {hp !== undefined && maxHp !== undefined && <span className="text-xs text-rose-300">{hp}/{maxHp} HP</span>}
                  </div>
                  <div className="mt-3 flex flex-wrap gap-1.5">
                    {Object.entries(character.stats)
                      .filter(([key]) => !["hp", "max_hp"].includes(key))
                      .slice(0, 6)
                      .map(([key, value]) => <span key={key} className="rounded-md bg-white/5 px-2 py-1 text-[10px] uppercase tracking-wide text-slate-400">{key}: {String(value)}</span>)}
                  </div>
                  <p className="mt-3 truncate text-xs text-slate-500">{character.inventory.length ? `${character.inventory.length} inventory item${character.inventory.length === 1 ? "" : "s"}` : "Empty inventory"}</p>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </aside>
  );
}
