import { defineTool } from "eve/tools";
import { z } from "zod";

import { canActForCharacter, characterSheetFor } from "../lib/campaigns/access.ts";
import { clearCombatState } from "../lib/campaigns/combat-store.ts";
import { campaignStore } from "../lib/campaigns/store.ts";
import {
  emptyCombatOrder,
  nextCombatant,
  weaponDamageDice,
  type CombatOrder,
  type CombatantEntry,
} from "../lib/engine/combat.ts";
import { abilityModifier, combineAdvantage, environmentModifiersForAttack, proficiencyBonus, rollDice } from "../lib/engine/dnd5e.ts";
import { gameState, readGameState, type Enemy, type GameState, type PlayerCharacter } from "../lib/memory.ts";

/** Допустимые кости урона: до 3 костей, грани 4/6/8/10/12. */
const DAMAGE_DICE_PATTERN = /^(\d+)d(\d+)$/i;

function parseDamageDice(spec: string): { count: number; sides: number } {
  const match = DAMAGE_DICE_PATTERN.exec(spec.trim());
  if (!match) throw new Error(`Недопустимый урон "${spec}", ожидалось вида "1d8".`);
  const count = Number(match[1]);
  const sides = Number(match[2]);
  if (count < 1 || count > 3 || ![4, 6, 8, 10, 12].includes(sides)) {
    throw new Error(`Недопустимый урон "${spec}": максимум 3 кости, грани 4/6/8/10/12.`);
  }
  return { count, sides };
}

/** Эвристика дальнего оружия по названию: луки, арбалеты, пращи, дротики. */
const RANGED_WEAPON_HINTS = [
  "лук", "арбалет", "праща", "дротик", "bow", "crossbow", "sling", "dart", "blowgun",
];

function isRangedWeapon(weapon?: string): boolean {
  if (!weapon) return false;
  const w = weapon.toLowerCase();
  return RANGED_WEAPON_HINTS.some((hint) => w.includes(hint));
}

function combatOf(state: GameState): CombatOrder {
  return state.combat ?? emptyCombatOrder();
}

/** Slug кампании текущей сессии — для очистки сохранения боя при завершении. */
function campaignSlugOf(state: GameState): string | undefined {
  return state.campaignId ? campaignStore.getCampaign(state.campaignId)?.slug : undefined;
}

function notStarted(): string {
  return "Бой не начат. Сначала вызови initiative со всеми участниками (партия и враги с HP и КД).";
}

/** Находит персонажа партии по точному id, затем по нормализованному имени (толерантность к LLM). */
function findPartyMember(state: GameState, ref: string): PlayerCharacter | undefined {
  const byId = state.party.find((member) => member.id === ref);
  if (byId) return byId;
  const needle = ref.trim().toLowerCase();
  return state.party.find((member) => member.name.trim().toLowerCase() === needle);
}

/**
 * Находит врага по точному id, затем по нормализованному имени. Возвращает и
 * совпадения по имени, чтобы при неоднозначности (2+ врага с одинаковым именем)
 * требовать уточнения через id.
 */
function findEnemy(state: GameState, ref: string): { match?: Enemy; ambiguous: boolean } {
  const byId = state.enemies.find((enemy) => enemy.id === ref);
  if (byId) return { match: byId, ambiguous: false };
  const needle = ref.trim().toLowerCase();
  const byName = state.enemies.filter((enemy) => enemy.name.trim().toLowerCase() === needle);
  if (byName.length === 0) return { ambiguous: false };
  if (byName.length === 1) return { match: byName[0], ambiguous: false };
  return { ambiguous: true };
}

/** Строка текущего хода вида «Раунд 2. Ход: Инокентий (5).». */
function currentTurnText(combat: CombatOrder): string {
  const entry = combat.order[combat.current];
  if (!combat.started || combat.current < 0 || !entry) return "";
  const dodgeMark = entry.dodging ? " [уклоняется]" : "";
  return `Раунд ${combat.round}. Ход: ${entry.name} (${entry.total})${dodgeMark}.`;
}

function hpNote(entry: CombatantEntry, state: GameState): string {
  if (entry.side === "enemy") {
    const enemy = state.enemies.find((candidate) => candidate.id === entry.id);
    return `, HP ${enemy?.hp ?? entry.hp ?? "?"}, КД ${enemy?.ac ?? entry.ac ?? "?"}`;
  }
  return entry.hp !== undefined ? `, HP ${entry.hp}/${entry.maxHp ?? "?"}` : "";
}

function statusText(state: GameState): string {
  const combat = combatOf(state);
  if (!combat.started || combat.order.length === 0) return notStarted();
  const lines = combat.order.map((entry, index) => {
    const marker = index === combat.current ? (combat.acted ? "→ ✓" : "→") : "  ";
    const dodge = entry.dodging ? " [уклоняется]" : "";
    return `${marker}${index + 1}. ${entry.name} [${entry.id}] (${entry.total})${hpNote(entry, state)}${dodge}`;
  });
  const turn = currentTurnText(combat);
  return [`Порядок ходов:`, ...lines, turn].filter(Boolean).join("\n");
}

/** Жив ли участник: враг — пока его id есть в списке активных, партия — пока HP > 0 (без HP — жив). */
function aliveCheck(state: GameState): (entry: CombatantEntry) => boolean {
  return (entry) => {
    if (entry.side === "enemy") {
      return state.enemies.some((enemy) => enemy.id === entry.id);
    }
    return entry.hp === undefined || entry.hp > 0;
  };
}

/** Кости урона: оружие из инвентаря → инвентарь → явно указанная кость → 1d4 без оружия. */
function resolveDamageDice(
  inventory: readonly string[] | undefined,
  weapon: string | undefined,
  damageDice: string | undefined,
): { spec: string; source: string } {
  const fromWeapon = weapon ? weaponDamageDice(inventory, weapon) : undefined;
  if (fromWeapon) return { spec: fromWeapon, source: `оружие «${weapon}»` };
  const fromInventory = weaponDamageDice(inventory);
  if (fromInventory) return { spec: fromInventory, source: "инвентарь" };
  if (damageDice) return { spec: damageDice, source: "указано" };
  return { spec: "1d4", source: "без оружия (1d4)" };
}

/** Урон или лечение по персонажу партии в порядке ходов (матчинг id-first → name-fallback). */
function applyPartyDamage(
  state: GameState,
  combat: CombatOrder,
  ref: string,
  amount: number,
  isHeal: boolean,
): string {
  const needle = ref.trim().toLowerCase();
  const index = combat.order.findIndex(
    (entry) =>
      entry.side === "party" && (entry.id === ref || entry.name.trim().toLowerCase() === needle),
  );
  if (index === -1) {
    const party = combat.order
      .filter((entry) => entry.side === "party")
      .map((entry) => `${entry.name} [${entry.id}]`)
      .join(", ") || "(партия пуста)";
    return `Персонаж «${ref}» не найден в порядке ходов партии. Участники: ${party}.`;
  }
  const entry = combat.order[index];
  const member = state.party.find((m) => m.id === entry.id || m.name.trim().toLowerCase() === needle);
  const currentHp = entry.hp ?? member?.hp;
  if (currentHp === undefined) {
    return `У «${entry.name}» нет HP в порядке ходов. Перезапусти initiative, чтобы подтянуть хиты.`;
  }
  const nextHp = isHeal ? currentHp + amount : Math.max(0, currentHp - amount);
  const order = combat.order.map((candidate, i) => (i === index ? { ...candidate, hp: nextHp } : candidate));
  const party = state.party.map((m) => (m.id === entry.id || m.name.trim().toLowerCase() === needle ? { ...m, hp: nextHp } : m));
  gameState.update((s) => ({ ...s, party, combat: { ...combat, order } }));
  const label = isHeal ? "лечение" : "урон";
  const fall = nextHp === 0 ? " Персонаж без сознания!" : "";
  return `${entry.name}: ${label} ${amount} HP → ${nextHp}/${entry.maxHp ?? "?"}.${fall}`;
}

/** Текущий участник совпадает со ссылкой (id точно либо нормализованное имя). */
function isCurrent(combat: CombatOrder, ref: string): boolean {
  const entry = combat.order[combat.current];
  if (!entry) return false;
  return entry.id === ref || entry.name.trim().toLowerCase() === ref.trim().toLowerCase();
}

export default defineTool({
  description:
    "Resolve turn-based combat tracked by the initiative tool. " +
    "actions: attack — the CURRENT combatant of the party attacks an enemy by its id or name " +
    "(d20 + ability modifier + proficiency vs enemy AC; natural 20 crits with double damage, natural 1 misses; " +
    "damage dice come from the weapon in the character's inventory); only one attack per turn, the turn then " +
    "advances; pass the id (from initiative) for an exact match — names are a tolerant fallback; " +
    "damage — apply damage (or heal) to a party member's HP in the turn order by id or name (persist with update_character); " +
    "dodge — the current party combatant takes the Dodge action: incoming attacks against them have disadvantage " +
    "until their next turn (roll the enemy's attack with roll_dice advantage='disadvantage'); the turn advances; " +
    "next — advance to the next combatant without an attack (after enemy turns, movement, Dash/Disengage, etc.); " +
    "status — show the current order, HP and whose turn it is; " +
    "end — end the combat and clear all enemies.",
  inputSchema: z
    .object({
      action: z.enum(["attack", "damage", "dodge", "next", "status", "end"]),
      attacker: z
        .string()
        .min(1)
        .max(100)
        .optional()
        .describe("id (точно) или имя участника партии — рекомендуются id из initiative."),
      enemy: z
        .string()
        .min(1)
        .max(100)
        .optional()
        .describe("id (точно) или имя врага — рекомендуются id из initiative."),
      target: z
        .string()
        .min(1)
        .max(100)
        .optional()
        .describe("id (точно) или имя персонажа партии для урона/лечения."),
      amount: z.number().int().min(1).max(1000).optional(),
      heal: z.boolean().optional().default(false),
      attack_stat: z
        .enum(["str", "dex"])
        .default("str")
        .describe("Атрибут атаки: str — сила, dex — ловкость (finesse-оружие)."),
      bonus: z
        .number()
        .int()
        .min(0)
        .max(5)
        .default(0)
        .describe("Дополнительный бонус к атаке (магическое оружие, заклинания); не заменяет характеристики."),
      weapon: z
        .string()
        .min(1)
        .max(100)
        .optional()
        .describe("Имя оружия из инвентаря персонажа; урон берётся из его описания (например, «меч (1d8)»)."),
      damage_dice: z
        .string()
        .min(1)
        .max(20)
        .optional()
        .describe("Запасная спецификация урона, если в инвентаре её нет: до 3 костей, грани 4/6/8/10/12."),
    })
    .refine((value) => value.action !== "attack" || Boolean(value.attacker), {
      message: "attacker is required when action is attack",
      path: ["attacker"],
    })
    .refine((value) => value.action !== "attack" || Boolean(value.enemy), {
      message: "enemy is required when action is attack",
      path: ["enemy"],
    })
    .refine((value) => value.action !== "dodge" || Boolean(value.attacker), {
      message: "attacker is required when action is dodge",
      path: ["attacker"],
    })
    .refine((value) => value.action !== "damage" || Boolean(value.target), {
      message: "target is required when action is damage",
      path: ["target"],
    })
    .refine((value) => value.action !== "damage" || value.amount !== undefined, {
      message: "amount is required when action is damage",
      path: ["amount"],
    }),
  execute(
    { action, attacker, enemy, target: damageTarget, amount, heal, attack_stat, bonus, weapon, damage_dice },
    ctx,
  ) {
    const state = readGameState();
    const combat = combatOf(state);

    if (action === "status") return statusText(state);

    if (!combat.started || combat.order.length === 0) {
      return notStarted();
    }

    if (action === "end") {
      const slug = campaignSlugOf(state);
      gameState.update((s) => ({ ...s, enemies: [], combat: emptyCombatOrder() }));
      if (slug) clearCombatState(slug);
      return "Бой окончен: порядок ходов и враги сброшены.";
    }

    if (action === "next") {
      const advanced = nextCombatant(combat, aliveCheck(state));
      gameState.update((s) => ({ ...s, combat: advanced }));
      const turn = currentTurnText(advanced);
      return turn ? `Ход переходит дальше. ${turn}` : notStarted();
    }

    if (action === "damage") {
      return applyPartyDamage(state, combat, damageTarget!, amount!, heal === true);
    }

    if (action === "dodge") {
      const currentEntry = combat.order[combat.current];
      if (!currentEntry) return notStarted();
      if (currentEntry.side !== "party") {
        return `Уклоняться может только персонаж партии. Сейчас ход ${currentEntry.name}.`;
      }
      if (!isCurrent(combat, attacker!)) {
        return `Сейчас ход ${currentEntry.name} — Dodge может использовать только участник, чей ход наступил.`;
      }
      const access = canActForCharacter(ctx, currentEntry.name);
      if (!access.allowed) {
        return access.reason ?? "Действие от имени этого персонажа запрещено.";
      }
      if (combat.acted) {
        return "Участник уже действовал в этом ходу. Заверши ход через combat action=next.";
      }
      const dodgedOrder = combat.order.map((entry, i) =>
        i === combat.current ? { ...entry, dodging: true } : entry,
      );
      const advanced = nextCombatant({ ...combat, order: dodgedOrder }, aliveCheck(state));
      gameState.update((s) => ({ ...s, combat: advanced }));
      const turn = currentTurnText(advanced);
      const base = `${currentEntry.name} уклоняется (Dodge): до его следующего хода атаки по нему идут с помехой.`;
      return turn ? `${base}\n${turn}` : base;
    }

    // action === "attack"
    const partyMember = findPartyMember(state, attacker!);
    if (!partyMember) {
      const party = state.party.map((entry) => `${entry.name} [${entry.id}]`).join(", ") || "(партия пуста)";
      return `Персонаж «${attacker}» не найден в партии. Участники партии: ${party}.`;
    }
    const access = canActForCharacter(ctx, partyMember.name);
    if (!access.allowed) {
      return access.reason ?? "Действие от имени этого персонажа запрещено.";
    }

    // Атаковать может только участник, чей ход наступил, и только раз за ход.
    const currentEntry = combat.order[combat.current];
    if (!currentEntry) return notStarted();
    if (!isCurrent(combat, attacker!)) {
      return `Сейчас ход ${currentEntry.name} — атаковать может только участник, чей ход наступил.`;
    }
    if (combat.acted) {
      return "Участник уже атаковал в этом ходу. Заверши ход через combat action=next.";
    }

    const found = findEnemy(state, enemy!);
    if (found.ambiguous) {
      const active = state.enemies.map((e) => `${e.name} [${e.id}] (HP ${e.hp}, КД ${e.ac})`).join(", ");
      return `Несколько врагов подходят под «${enemy}». Уточни цель по id: ${active}.`;
    }
    const target = found.match;
    if (!target) {
      const active = state.enemies
        .map((candidate) => `${candidate.name} [${candidate.id}] (${candidate.hp} HP, КД ${candidate.ac})`)
        .join(", ");
      return `Нет врага «${enemy}». Активные враги: ${active}.`;
    }

    // Бонус атаки из характеристик и уровня персонажа (не из воздуха).
    const sheet = characterSheetFor(ctx, partyMember.name);
    const stats = sheet?.stats ?? partyMember.stats ?? {};
    const level = sheet?.level ?? partyMember.level ?? 1;
    const inventory = sheet?.inventory;
    const modifier = abilityModifier(stats, attack_stat);
    const proficiency = proficiencyBonus(level);

    // Влияние окружения (C2): сильный ветер/шторм → помеха на дальние атаки.
    const isRanged = isRangedWeapon(weapon);
    const campaign = campaignSlugOf(state)
      ? campaignStore.getCampaign(campaignSlugOf(state)!)
      : undefined;
    const env = environmentModifiersForAttack(
      { timeOfDay: campaign?.timeOfDay, weather: campaign?.weather },
      isRanged ? "ranged" : "melee",
    );
    const attackAdvantage = combineAdvantage(env.advantage);

    const rollResult = rollDice(20, 1, Math.random, attackAdvantage);
    const roll = rollResult.total;
    const isCrit = roll === 20;
    const isFumble = roll === 1;
    const totalBonus = modifier + proficiency + bonus;
    const hit = isCrit || (!isFumble && roll + totalBonus >= target.ac);

    const dice = resolveDamageDice(inventory, weapon, damage_dice);
    const { count, sides } = parseDamageDice(dice.spec);
    const damage = hit ? (isCrit ? 2 : 1) * rollDice(sides, count).total : 0;

    const advMark = attackAdvantage === false ? " (помеха)" : attackAdvantage === true ? " (преимущество)" : "";
    const envNote = env.reasons.length > 0 ? ` [${env.reasons.join("; ")}]` : "";
    let resultLine: string;
    let defeated = false;
    if (hit) {
      const remaining = Math.max(0, target.hp - damage);
      defeated = remaining === 0;
      const hpNote = defeated
        ? `${target.name} повержен!`
        : `У ${target.name} осталось ${remaining} HP.`;
      const critMark = isCrit ? " — КРИТ!" : "";
      resultLine = `${attacker} атакует ${target.name}${dice.source !== "указано" ? ` (${dice.spec}, ${dice.source})` : ` (${dice.spec})`}${advMark}: d20 = ${roll}${critMark} + ${totalBonus} против КД ${target.ac} → попадание, урон ${damage}. ${hpNote}${envNote}`;
    } else {
      const fumbleMark = isFumble ? " (натуральная 1)" : "";
      resultLine = `${attacker} атакует ${target.name}${advMark}: d20 = ${roll} + ${totalBonus} против КД ${target.ac} → промах${fumbleMark}.${envNote}`;
    }

    let nextEnemies = state.enemies;
    if (hit) {
      if (defeated) {
        nextEnemies = state.enemies.filter((candidate) => candidate.id !== target.id);
      } else {
        nextEnemies = state.enemies.map((candidate) =>
          candidate.id === target.id ? { ...candidate, hp: Math.max(0, target.hp - damage) } : candidate,
        );
      }
    }

    if (hit && nextEnemies.length === 0) {
      const slug = campaignSlugOf(state);
      gameState.update((s) => ({ ...s, enemies: [], combat: emptyCombatOrder() }));
      if (slug) clearCombatState(slug);
      return `${resultLine}\nВсе враги повержены. Бой окончен.`;
    }

    const advanced = nextCombatant(combat, aliveCheck({ ...state, enemies: nextEnemies }));
    gameState.update((s) => ({ ...s, enemies: nextEnemies, combat: advanced }));
    const turn = currentTurnText(advanced);
    return turn ? `${resultLine}\n${turn}` : resultLine;
  },
});
