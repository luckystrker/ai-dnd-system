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

  test("auto-generates slug ids from names", () => {
    const order = rollInitiative([{ name: "Гоблин Воин", side: "enemy", hp: 10, ac: 12 }], sequence(10));
    assert.equal(order[0].id, "гоблин-воин");
  });

  test("deduplicates identical slug ids with -2, -3 suffixes", () => {
    const order = rollInitiative(
      [
        { name: "Гоблин", side: "enemy", hp: 7, ac: 12 },
        { name: "гоблин", side: "enemy", hp: 7, ac: 12 },
        { name: "ГОБЛИН", side: "enemy", hp: 7, ac: 12 },
      ],
      sequence(5, 5, 5),
    );
    assert.deepEqual(
      order.map((e) => e.id).sort(),
      ["гоблин", "гоблин-2", "гоблин-3"],
    );
  });

  test("uses an explicit id when provided", () => {
    const order = rollInitiative([{ id: "party_1", name: "Варвар", side: "party" }], sequence(8));
    assert.equal(order[0].id, "party_1");
  });
});

describe("nextCombatant", () => {
  const order: CombatOrder = {
    started: true,
    round: 1,
    current: 0,
    acted: true,
    order: [
      { id: "тварь", name: "Тварь", side: "enemy", bonus: 4, roll: 16, total: 20 },
      { id: "микроб", name: "Микроб", side: "party", bonus: 2, roll: 8, total: 10 },
      { id: "инокентий", name: "Инокентий", side: "party", bonus: 2, roll: 3, total: 5 },
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
      { ...order, current: 0, order: [...order.order, { id: "труп", name: "Труп", side: "party", hp: 0, bonus: 0, roll: 1, total: 1 }] },
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

  test("clears dodging on the combatant whose turn begins", () => {
    const dodging = order.order.map((e, i) => (i === 1 ? { ...e, dodging: true } : e));
    const next = nextCombatant({ ...order, current: 0, order: dodging }, () => true);
    assert.equal(next.current, 1);
    assert.equal(next.order[1].dodging, false);
  });

  test("preserves dodging on others when their turn has not come", () => {
    const dodging = order.order.map((e) => ({ ...e, dodging: true }));
    // Ход Твари (0) → начинается ход Микроба (1); dodge Микроба должен сброситься,
    // dodge Инокентия (2) — ещё нет, должен сохраниться.
    const next = nextCombatant({ ...order, current: 0, order: dodging }, () => true);
    assert.equal(next.order[1].dodging, false);
    assert.equal(next.order[2].dodging, true);
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
