import { test } from "node:test";
import assert from "node:assert/strict";

import { emptyCombatOrder } from "../agent/lib/engine/combat.ts";
import { normalizeGameState } from "../agent/lib/memory.ts";

test("дефолты для старой сессии без новых полей (combat, diceHistory, journal)", () => {
  const raw = {
    started: true,
    campaignId: "c1",
    scene: "",
    party: [{ id: "p1", name: "Герой", stats: { str: 14 } }],
  };
  const s = normalizeGameState(raw as never);
  assert.deepEqual(s.enemies, []);
  assert.deepEqual(s.combat, emptyCombatOrder());
  assert.deepEqual(s.diceHistory, []);
  assert.deepEqual(s.journal, []);
  assert.equal(s.party[0].name, "Герой"); // валидный массив не подменён
});

test("валидные поля сохраняются как есть", () => {
  const combat = { started: true, round: 2, current: 0, acted: false, order: [] };
  const s = normalizeGameState({
    started: true,
    campaignId: "c2",
    scene: "лес",
    party: [],
    enemies: [{ id: "e", name: "E", hp: 5, ac: 12 }],
    combat,
    journal: ["j"],
    diceHistory: [1, 2],
  } as never);
  assert.equal(s.combat, combat);
  assert.deepEqual(s.diceHistory, [1, 2]);
  assert.equal(s.enemies.length, 1);
  assert.equal(s.scene, "лес");
});

test("невалидные значения полей заменяются дефолтами", () => {
  const s = normalizeGameState({
    started: "yes",
    campaignId: 42,
    party: null,
    enemies: "none",
    combat: undefined,
    journal: {},
    diceHistory: null,
  } as never);
  assert.equal(s.started, false);
  assert.equal(s.campaignId, undefined);
  assert.deepEqual(s.party, []);
  assert.deepEqual(s.enemies, []);
  assert.deepEqual(s.combat, emptyCombatOrder());
  assert.deepEqual(s.journal, []);
  assert.deepEqual(s.diceHistory, []);
});
