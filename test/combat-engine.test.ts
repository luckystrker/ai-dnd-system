import { test, describe } from "node:test";
import assert from "node:assert/strict";

import {
  emptyCombatOrder,
  nextCombatant,
  rollInitiative,
  weaponDamageDice,
  type CombatOrder,
} from "../agent/lib/engine/combat.ts";

/** Детерминированный генератор: задаёт выпавшие значения d20 (1..20). */
function sequence(...rolls: number[]): () => number {
  let index = 0;
  return () => ((rolls[index++] ?? 10) - 0.5) / 20;
}

describe("rollInitiative", () => {
  test("sorts combatants by total descending", () => {
    const order = rollInitiative(
      [
        { name: "Микроб", side: "party", bonus: 2 },
        { name: "Инокентий", side: "party", bonus: 2 },
        { name: "Тварь", side: "enemy", bonus: 4, hp: 20, ac: 12 },
      ],
      sequence(8, 3, 16),
    );
    assert.deepEqual(
      order.map((e) => e.name),
      ["Тварь", "Микроб", "Инокентий"],
    );
    assert.equal(order[0].total, 20);
    assert.equal(order[1].total, 10);
    assert.equal(order[2].total, 5);
  });

  test("keeps enemy hp and ac on entries", () => {
    const [enemy] = rollInitiative(
      [{ name: "Тварь", side: "enemy", hp: 30, ac: 14, bonus: 1 }],
      sequence(12),
    );
    assert.equal(enemy.hp, 30);
    assert.equal(enemy.ac, 14);
  });

  test("ties break by name", () => {
    const order = rollInitiative(
      [
        { name: "Бета", side: "party" },
        { name: "Альфа", side: "party" },
      ],
      sequence(10, 10),
    );
    assert.deepEqual(
      order.map((e) => e.name),
      ["Альфа", "Бета"],
    );
  });
});

describe("nextCombatant", () => {
  const order: CombatOrder = {
    started: true,
    round: 1,
    current: 0,
    acted: true,
    order: [
      { name: "Тварь", side: "enemy", bonus: 4, roll: 16, total: 20 },
      { name: "Микроб", side: "party", bonus: 2, roll: 8, total: 10 },
      { name: "Инокентий", side: "party", bonus: 2, roll: 3, total: 5 },
    ],
  };

  test("advances to the next combatant in order", () => {
    const next = nextCombatant(order, () => true);
    assert.equal(next.current, 1);
    assert.equal(next.round, 1);
  });

  test("resets the acted flag for the next combatant", () => {
    const next = nextCombatant(order, () => true);
    assert.equal(next.acted, false);
  });

  test("increments round when wrapping past the last combatant", () => {
    const last = { ...order, current: 2 };
    const next = nextCombatant(last, () => true);
    assert.equal(next.current, 0);
    assert.equal(next.round, 2);
  });

  test("skips dead enemies", () => {
    const next = nextCombatant(
      { ...order, current: 1 },
      (entry) => entry.name !== "Инокентий",
    );
    assert.equal(next.current, 0);
    assert.equal(next.round, 2);
  });

  test("skips party members at zero hp", () => {
    const next = nextCombatant(
      { ...order, current: 0, order: [...order.order, { name: "Труп", side: "party", hp: 0, bonus: 0, roll: 1, total: 1 }] },
      (entry) => entry.hp === undefined || entry.hp > 0,
    );
    assert.equal(next.current, 1);
  });

  test("leaves the order unchanged when nobody is alive", () => {
    const next = nextCombatant({ ...order, current: 0 }, () => false);
    assert.equal(next.current, 0);
    assert.equal(next.round, 1);
  });

  test("does nothing when combat has not started", () => {
    const idle = emptyCombatOrder();
    const next = nextCombatant(idle, () => true);
    assert.equal(next.current, -1);
    assert.equal(next.started, false);
  });
});

describe("emptyCombatOrder", () => {
  test("starts without an acted flag", () => {
    assert.equal(emptyCombatOrder().acted, false);
  });
});

describe("weaponDamageDice", () => {
  const inventory = ["короткий меч (1d6)", "факел", "лук длинный (1d8)", "зелье лечения (1d4)"];
  const weaponless = ["факел", "верёвка"];

  test("finds dice inside a named weapon", () => {
    assert.equal(weaponDamageDice(inventory, "меч"), "1d6");
    assert.equal(weaponDamageDice(inventory, "лук"), "1d8");
  });

  test("scans the whole inventory without a weapon name", () => {
    assert.equal(weaponDamageDice(inventory), "1d6");
  });

  test("ignores the weapon name when it has no dice spec", () => {
    assert.equal(weaponDamageDice(["магическая палочка"], "палочка"), undefined);
  });

  test("falls back to undefined without dice specs", () => {
    assert.equal(weaponDamageDice(weaponless), undefined);
    assert.equal(weaponDamageDice(undefined), undefined);
    assert.equal(weaponDamageDice(inventory, "нет такого"), undefined);
  });

  test("matches case-insensitively", () => {
    assert.equal(weaponDamageDice(["Короткий Меч (1d6)"], "меч"), "1d6");
  });
});
