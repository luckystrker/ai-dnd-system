import { useCombatStore } from "../../stores/combatStore";


export default function CombatPanel() {
  const combat = useCombatStore((state) => state.combat);
  if (!combat) {
    return null;
  }

  const currentId = combat.turn_order[combat.turn_index];
  const combatants = combat.turn_order
    .map((id) => combat.combatants[id])
    .filter(Boolean);

  return (
    <section className="border-b border-white/10 bg-slate-950/50 px-5 py-3 sm:px-8">
      <div className="mx-auto w-full max-w-5xl">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-rose-300">Encounter</p>
            <p className="mt-1 text-sm font-medium text-white">Round {combat.round}</p>
          </div>
          <span className={`rounded-full px-2.5 py-1 text-[11px] font-medium ${combat.status === "active" ? "bg-rose-400/10 text-rose-200" : "bg-emerald-400/10 text-emerald-200"}`}>
            {combat.status}
          </span>
        </div>
        <div className="flex gap-2 overflow-x-auto pb-1">
          {combatants.map((combatant) => {
            const health = Math.max(0, Math.min(100, (combatant.hp / combatant.max_hp) * 100));
            const isCurrent = combatant.id === currentId && combat.status === "active";
            return (
              <div key={combatant.id} className={`min-w-36 rounded-xl border px-3 py-2 ${isCurrent ? "border-amber-300/50 bg-amber-300/10" : "border-white/10 bg-slate-900/70"}`}>
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-xs font-medium text-slate-200">{combatant.name}</span>
                  <span className={`text-[10px] uppercase ${combatant.side === "enemy" ? "text-rose-300" : "text-indigo-300"}`}>{combatant.side}</span>
                </div>
                <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-slate-700">
                  <div className={`h-full rounded-full ${combatant.side === "enemy" ? "bg-rose-400" : "bg-indigo-400"}`} style={{ width: `${health}%` }} />
                </div>
                <p className="mt-1 text-[10px] text-slate-500">{combatant.hp}/{combatant.max_hp} HP · AC {combatant.ac}</p>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
