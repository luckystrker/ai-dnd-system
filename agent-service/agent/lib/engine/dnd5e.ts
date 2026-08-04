export interface DiceResult {
  rolls: number[];
  total: number;
}

export interface CheckResult {
  roll: number;
  modifier: number;
  total: number;
  difficulty: number;
  success: boolean;
  margin: number;
}

export const SKILL_ABILITY_MAP: Record<string, string> = {
  athletics: "str",
  acrobatics: "dex",
  sleight_of_hand: "dex",
  stealth: "dex",
  arcana: "int",
  history: "int",
  investigation: "int",
  nature: "int",
  religion: "int",
  animal_handling: "wis",
  insight: "wis",
  medicine: "wis",
  perception: "wis",
  survival: "wis",
  deception: "cha",
  intimidation: "cha",
  performance: "cha",
  persuasion: "cha",
};

export type RandomSource = () => number;

function randomInt(sides: number, random: RandomSource): number {
  return Math.floor(random() * sides) + 1;
}

export function rollDice(
  sides: number,
  count = 1,
  random: RandomSource = Math.random,
): DiceResult {
  if (!Number.isInteger(sides) || sides < 1) {
    throw new Error("Dice must have at least one side");
  }
  if (!Number.isInteger(count) || count < 1) {
    throw new Error("Dice count must be at least one");
  }
  if (count > 100) {
    throw new Error("Dice count must not exceed 100");
  }
  const rolls = Array.from({ length: count }, () => randomInt(sides, random));
  return { rolls, total: rolls.reduce((sum, roll) => sum + roll, 0) };
}

export function skillCheck(
  stats: Record<string, unknown>,
  skill: string,
  difficulty: number,
  advantage: boolean | null = null,
  random: RandomSource = Math.random,
): CheckResult {
  const ability = SKILL_ABILITY_MAP[skill.trim().toLowerCase()] ?? "str";
  const rawScore = stats[ability];
  const parsedScore = typeof rawScore === "number" ? rawScore : Number(rawScore);
  const score = Number.isFinite(parsedScore) ? Math.trunc(parsedScore) : 10;
  const modifier = Math.floor((score - 10) / 2);
  const firstRoll = randomInt(20, random);
  const secondRoll = randomInt(20, random);
  const roll = advantage === true
    ? Math.max(firstRoll, secondRoll)
    : advantage === false
      ? Math.min(firstRoll, secondRoll)
      : firstRoll;
  const total = roll + modifier;
  return {
    roll,
    modifier,
    total,
    difficulty,
    success: total >= difficulty,
    margin: total - difficulty,
  };
}
