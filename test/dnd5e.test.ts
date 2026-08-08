import { test, describe } from "node:test";
import assert from "node:assert/strict";

import {
  abilityModifier,
  abilityScore,
  isLowStreak,
  LOW_STREAK_FLOOR,
  LOW_STREAK_WINDOW,
  makeLuckyRandom,
  parseDiceNotation,
  proficiencyBonus,
  resolveSkillAbility,
  rollDice,
  rollDiceNotation,
  skillCheck,
  type CheckResult,
} from "../agent/lib/engine/dnd5e.ts";

describe("rollDice", () => {
  test("sums the rolls", () => {
    const result = rollDice(6, 3, () => 0.5);
    assert.deepEqual(result.rolls, [4, 4, 4]);
    assert.equal(result.total, 12);
  });

  test("defaults to a single die", () => {
    const result = rollDice(6, undefined, () => 0.0);
    assert.equal(result.rolls.length, 1);
    assert.equal(result.total, 1);
  });

  test("lowest random yields all ones", () => {
    const result = rollDice(20, 2, () => 0.0);
    assert.deepEqual(result.rolls, [1, 1]);
    assert.equal(result.total, 2);
  });

  test("near-one random yields max faces", () => {
    const result = rollDice(20, 2, () => 0.999999);
    assert.deepEqual(result.rolls, [20, 20]);
  });

  test("rejects invalid sides", () => {
    assert.throws(() => rollDice(0), /at least one side/);
    assert.throws(() => rollDice(2.5), /at least one side/);
    assert.throws(() => rollDice(-6), /at least one side/);
  });

  test("rejects invalid counts", () => {
    assert.throws(() => rollDice(6, 0), /at least one/);
    assert.throws(() => rollDice(6, -1), /at least one/);
    assert.throws(() => rollDice(6, 1.5), /at least one/);
    assert.throws(() => rollDice(6, 101), /must not exceed 100/);
  });

  test("rolls stay within bounds for any source", () => {
    const result = rollDice(8, 100, () => Math.random());
    for (const roll of result.rolls) {
      assert.ok(roll >= 1 && roll <= 8, `roll ${roll} out of bounds`);
    }
  });
});

describe("skillCheck", () => {
  const stats = {
    strength: 16, // +3
    dexterity: 8, // -1
    wisdom: 14, // +2
  };

  test("computes modifier from ability score and success", () => {
    const result = skillCheck(stats, "perception", 12, null, () => 0.5);
    assert.equal(result.roll, 11);
    assert.equal(result.modifier, 2);
    assert.equal(result.total, 13);
    assert.equal(result.margin, 1);
    assert.equal(result.success, true);
    assert.equal(result.ability, "wis");
  });

  test("fails when below difficulty", () => {
    const result = skillCheck(stats, "strength", 20, null, () => 0.0);
    assert.equal(result.success, false);
    assert.equal(result.margin, -16);
  });

  test("natural 20 always succeeds even against impossible DC", () => {
    const result = skillCheck(stats, "strength", 100, null, () => 0.999999);
    assert.equal(result.roll, 20);
    assert.equal(result.naturalSuccess, true);
    assert.equal(result.success, true);
  });

  test("natural 1 always fails even against trivial DC", () => {
    const result = skillCheck(stats, "strength", 1, null, () => 0.0);
    assert.equal(result.roll, 1);
    assert.equal(result.naturalFailure, true);
    assert.equal(result.success, false);
  });

  test("advantage takes the higher roll", () => {
    const sequence = [() => 0.0, () => 0.999999];
    const result = skillCheck(stats, "strength", 10, true, () => sequence.shift()!());
    assert.equal(result.roll, 20);
  });

  test("disadvantage takes the lower roll", () => {
    const sequence = [() => 0.5, () => 0.0];
    const result = skillCheck(stats, "strength", 10, false, () => sequence.shift()!());
    assert.equal(result.roll, 1);
  });

  test("missing stats default to a score of 10 (modifier +0)", () => {
    const result = skillCheck({}, "athletics", 11, null, () => 0.5);
    assert.equal(result.modifier, 0);
    assert.equal(result.total, 11);
    assert.equal(result.success, true);
  });

  test("defaults missing ability to strength", () => {
    const result = skillCheck({ strength: 10 }, "unheard_of_skill", 10, null, () => 0.5);
    assert.equal(result.ability, "str");
  });

  test("string stats are coerced to numbers", () => {
    const result = skillCheck({ str: "20" }, "athletics", 15, null, () => 0.5);
    assert.equal(result.modifier, 5);
    assert.equal(result.total, 16);
  });

  test("non-numeric stats are ignored", () => {
    const result = skillCheck({ str: "banana" }, "athletics", 10, null, () => 0.5);
    assert.equal(result.modifier, 0);
  });
});

describe("resolveSkillAbility", () => {
  const cases: Array<[string, string]> = [
    ["athletics", "str"],
    ["stealth", "dex"],
    ["perception", "wis"],
    ["persuasion", "cha"],
    ["arcana", "int"],
    ["athletic", "str"],
    ["Восприятие", "wis"],
    ["восприятие", "wis"],
    ["Восприятие (Perception)", "wis"],
    ["Ловкость рук", "dex"],
    ["Убеждение", "cha"],
    ["расследование", "int"],
    ["unknown", "str"],
    ["", "str"],
    ["  perception  ", "wis"],
  ];
  for (const [input, expected] of cases) {
    test(`maps ${JSON.stringify(input)} -> ${expected}`, () => {
      assert.equal(resolveSkillAbility(input), expected);
    });
  }
});

describe("abilityScore", () => {
  test("matches full and abbreviated names case-insensitively", () => {
    const stats = { strength: 15, DEX: 12, Wisdom: 9 };
    assert.equal(abilityScore(stats, "strength"), 15);
    assert.equal(abilityScore(stats, "STR"), 15);
    assert.equal(abilityScore(stats, "dexterity"), 12);
    assert.equal(abilityScore(stats, "dex"), 12);
    assert.equal(abilityScore(stats, "wisdom"), 9);
  });

  test("returns undefined for unknown abilities", () => {
    assert.equal(abilityScore({ strength: 15 }, "charisma"), undefined);
    assert.equal(abilityScore({}, "str"), undefined);
  });

  test("truncates fractional values", () => {
    assert.equal(abilityScore({ str: 15.7 }, "str"), 15);
  });
});

describe("abilityModifier", () => {
  test("computes the 5e modifier from the ability score", () => {
    assert.equal(abilityModifier({ strength: 15 }, "str"), 2);
    assert.equal(abilityModifier({ strength: 10 }, "str"), 0);
    assert.equal(abilityModifier({ strength: 8 }, "str"), -1);
    assert.equal(abilityModifier({ dexterity: 20 }, "dex"), 5);
  });

  test("defaults to a score of 10 when missing", () => {
    assert.equal(abilityModifier({}, "str"), 0);
  });
});

describe("proficiencyBonus", () => {
  test("follows the 5e progression", () => {
    assert.equal(proficiencyBonus(1), 2);
    assert.equal(proficiencyBonus(4), 2);
    assert.equal(proficiencyBonus(5), 3);
    assert.equal(proficiencyBonus(9), 4);
    assert.equal(proficiencyBonus(13), 5);
    assert.equal(proficiencyBonus(17), 6);
    assert.equal(proficiencyBonus(20), 6);
  });

  test("handles invalid levels", () => {
    assert.equal(proficiencyBonus(0), 2);
    assert.equal(proficiencyBonus(NaN), 2);
  });
});

describe("rollDice advantage", () => {
  test("advantage on d20 keeps the higher of two and records pairs", () => {
    const seq = [0.0, 0.999999];
    const result = rollDice(20, 1, () => seq.shift()!, true);
    assert.equal(result.rolls.length, 1);
    assert.equal(result.rolls[0], 20);
    assert.deepEqual(result.pairs, [[20, 1]]);
    assert.equal(result.total, 20);
  });

  test("disadvantage on d20 keeps the lower of two", () => {
    const seq = [0.5, 0.0];
    const result = rollDice(20, 1, () => seq.shift()!, false);
    assert.equal(result.rolls[0], 1);
    assert.deepEqual(result.pairs, [[1, 11]]);
  });

  test("advantage ignored for non-d20 dice", () => {
    const result = rollDice(6, 2, () => 0.5, true);
    assert.deepEqual(result.rolls, [4, 4]);
    assert.equal(result.pairs, undefined);
  });

  test("advantage null behaves like a normal roll with no pairs", () => {
    const result = rollDice(20, 2, () => 0.5, null);
    assert.deepEqual(result.rolls, [11, 11]);
    assert.equal(result.pairs, undefined);
  });
});

describe("parseDiceNotation", () => {
  test("parses a single group with explicit count", () => {
    assert.deepEqual(parseDiceNotation("4d20"), { groups: [{ count: 4, sides: 20 }], modifier: 0 });
  });

  test("parses a single group with implicit count (d8)", () => {
    assert.deepEqual(parseDiceNotation("d8"), { groups: [{ count: 1, sides: 8 }], modifier: 0 });
  });

  test("parses multiple groups and a positive modifier", () => {
    assert.deepEqual(parseDiceNotation("2d6+1d8+3"), {
      groups: [
        { count: 2, sides: 6 },
        { count: 1, sides: 8 },
      ],
      modifier: 3,
    });
  });

  test("parses a negative modifier", () => {
    assert.deepEqual(parseDiceNotation("2d4-1"), {
      groups: [{ count: 2, sides: 4 }],
      modifier: -1,
    });
  });

  test("is case-insensitive and ignores spaces", () => {
    assert.deepEqual(parseDiceNotation(" 2D6 + 1D8 "), {
      groups: [
        { count: 2, sides: 6 },
        { count: 1, sides: 8 },
      ],
      modifier: 0,
    });
  });

  test("rejects empty notation", () => {
    assert.throws(() => parseDiceNotation(""), /Пустая нотация/);
    assert.throws(() => parseDiceNotation("   "), /Пустая нотация/);
  });

  test("rejects invalid characters", () => {
    assert.throws(() => parseDiceNotation("2d6+abc"), /Недопустимые символы/);
    assert.throws(() => parseDiceNotation("roll 1d20"), /Недопустимые символы/);
  });

  test("rejects zero count and zero sides", () => {
    assert.throws(() => parseDiceNotation("0d6"), /≥ 1/);
    assert.throws(() => parseDiceNotation("2d0"), /≥ 1/);
  });

  test("rejects too many dice and too many groups", () => {
    assert.throws(() => parseDiceNotation("101d6"), /Слишком много костей/);
    assert.throws(() => parseDiceNotation("1d6+1d6+1d6+1d6+1d6+1d6"), /Слишком много групп/);
  });
});

describe("rollDiceNotation", () => {
  test("sums multiple groups and a modifier", () => {
    // random()=0.5 → d6 face 4, d8 face 5
    const result = rollDiceNotation("2d6+1d8+3", { random: () => 0.5 });
    assert.equal(result.groups.length, 2);
    assert.equal(result.groups[0].subtotal, 8);
    assert.equal(result.groups[1].subtotal, 5);
    assert.equal(result.modifier, 3);
    assert.equal(result.total, 16);
  });

  test("advantage affects only d20 groups", () => {
    // seq for 2d20: pairs (1,20),(1,20) → keep 20 each; then d6 unaffected → 4
    const seq = [0.0, 0.999999, 0.0, 0.999999, 0.5];
    const result = rollDiceNotation("2d20+1d6", {
      random: () => seq.shift()!,
      advantage: true,
    });
    const d20 = result.groups[0];
    const d6 = result.groups[1];
    assert.deepEqual(d20.rolls, [20, 20]);
    assert.deepEqual(d20.pairs, [
      [20, 1],
      [20, 1],
    ]);
    assert.equal(d20.subtotal, 40);
    assert.equal(d6.rolls[0], 4);
    assert.equal(d6.pairs, undefined);
    assert.equal(result.total, 44);
  });

  test("negative modifier reduces total", () => {
    const result = rollDiceNotation("1d6-2", { random: () => 0.5 });
    assert.equal(result.total, 2);
  });
});

describe("makeLuckyRandom", () => {
  test("returns a value in [0, 1)", () => {
    const lucky = makeLuckyRandom(() => 0.7, false);
    const v = lucky();
    assert.ok(v >= 0 && v < 1, `lucky() = ${v} out of range`);
  });

  test("with recentLowStreak the d20 face is at least LOW_STREAK_FLOOR", () => {
    const lucky = makeLuckyRandom(() => 0.0, true);
    const face = Math.floor(lucky() * 20) + 1;
    assert.ok(face >= LOW_STREAK_FLOOR, `face ${face} below floor ${LOW_STREAK_FLOOR}`);
  });

  test("does not drop below a plain low roll when not in streak", () => {
    // r = 0.1 → face 2 (< threshold), reroll chance consumed: base()=0.0 < 0.5 →
    // second base()=0.9 → max(0.1, 0.9) = 0.9 → face 19 (>= original 2).
    const seq = [0.1, 0.0, 0.9];
    const lucky = makeLuckyRandom(() => seq.shift()!, false);
    const face = Math.floor(lucky() * 20) + 1;
    assert.ok(face >= 2, `face ${face} below plain low roll`);
  });
});

describe("isLowStreak", () => {
  test("true when the last window of rolls are all low", () => {
    assert.equal(isLowStreak([3, 5]), true);
    assert.equal(isLowStreak([2, 4, 6]), true);
  });

  test("false when a recent roll is not low", () => {
    assert.equal(isLowStreak([3, 12]), false);
  });

  test("false when there are fewer rolls than the window", () => {
    assert.equal(isLowStreak([3]), false);
    assert.equal(isLowStreak(new Array(LOW_STREAK_WINDOW - 1).fill(1)), false);
  });
});
