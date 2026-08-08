import { test, describe } from "node:test";
import assert from "node:assert/strict";

import {
  combineAdvantage,
  environmentModifiersForAttack,
  environmentModifiersForCheck,
} from "../agent/lib/engine/dnd5e.ts";

describe("combineAdvantage (5e advantage/disadvantage rule)", () => {
  test("returns null when all layers are null/undefined", () => {
    assert.equal(combineAdvantage(null, undefined, null), null);
    assert.equal(combineAdvantage(), null);
  });

  test("returns the only non-null layer", () => {
    assert.equal(combineAdvantage(null, true), true);
    assert.equal(combineAdvantage(false, null), false);
  });

  test("advantage + disadvantage cancel to null", () => {
    assert.equal(combineAdvantage(true, false), null);
    assert.equal(combineAdvantage(false, true), null);
  });

  test("two advantages stay advantage", () => {
    assert.equal(combineAdvantage(true, true), true);
  });

  test("two disadvantages stay disadvantage", () => {
    assert.equal(combineAdvantage(false, false), false);
  });

  test("ignores undefined layers mixed with booleans", () => {
    assert.equal(combineAdvantage(undefined, true, undefined), true);
  });
});

describe("environmentModifiersForCheck", () => {
  test("night gives disadvantage on sight-based checks (Perception)", () => {
    const mod = environmentModifiersForCheck({ timeOfDay: "night" }, "Perception");
    assert.equal(mod.advantage, false);
    assert.ok(mod.reasons.some((r) => r.includes("ночь")));
  });

  test("night gives advantage on Stealth", () => {
    const mod = environmentModifiersForCheck({ timeOfDay: "night" }, "Stealth");
    assert.equal(mod.advantage, true);
    assert.ok(mod.reasons.some((r) => r.includes("скрытность")));
  });

  test("night + fog on Perception: two disadvantages combine to disadvantage", () => {
    const mod = environmentModifiersForCheck({ timeOfDay: "night", weather: "туман" }, "Perception");
    assert.equal(mod.advantage, false);
    assert.ok(mod.reasons.length >= 1);
  });

  test("day with clear weather gives no modifier", () => {
    const mod = environmentModifiersForCheck({ timeOfDay: "day", weather: "ясно" }, "Investigation");
    assert.equal(mod.advantage, null);
    assert.equal(mod.reasons.length, 0);
  });

  test("fog gives disadvantage on Survival (visual)", () => {
    const mod = environmentModifiersForCheck({ weather: "туман" }, "Survival");
    assert.equal(mod.advantage, false);
  });

  test("accepts Russian skill names", () => {
    const mod = environmentModifiersForCheck({ timeOfDay: "night" }, "Восприятие");
    assert.equal(mod.advantage, false);
  });

  test("no effect on non-visual checks at night", () => {
    const mod = environmentModifiersForCheck({ timeOfDay: "night" }, "Athletics");
    assert.equal(mod.advantage, null);
  });

  test("empty environment gives no modifier", () => {
    const mod = environmentModifiersForCheck({}, "Perception");
    assert.equal(mod.advantage, null);
    assert.equal(mod.reasons.length, 0);
  });
});

describe("environmentModifiersForAttack", () => {
  test("strong wind gives disadvantage on ranged attacks", () => {
    const mod = environmentModifiersForAttack({ weather: "сильный ветер" }, "ranged");
    assert.equal(mod.advantage, false);
    assert.ok(mod.reasons.some((r) => r.includes("ветер")));
  });

  test("storm gives disadvantage on ranged attacks", () => {
    const mod = environmentModifiersForAttack({ weather: "шторм" }, "ranged");
    assert.equal(mod.advantage, false);
  });

  test("no effect on melee attacks even in strong wind", () => {
    const mod = environmentModifiersForAttack({ weather: "сильный ветер" }, "melee");
    assert.equal(mod.advantage, null);
  });

  test("no effect on ranged attacks in clear weather", () => {
    const mod = environmentModifiersForAttack({ weather: "ясно" }, "ranged");
    assert.equal(mod.advantage, null);
  });
});
