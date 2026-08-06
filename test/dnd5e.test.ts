import { test, describe } from "node:test";
import assert from "node:assert/strict";

import {
  abilityScore,
  resolveSkillAbility,
  rollDice,
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
