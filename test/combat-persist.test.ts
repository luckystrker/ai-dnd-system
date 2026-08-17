import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";

import {
  deserializeCombatOrder,
  emptyCombatOrder,
  rollInitiative,
  serializeCombatOrder,
  type CombatantEntry,
  type CombatOrder,
} from "../agent/lib/engine/combat.ts";
import {
  clearCombatState,
  loadCombatState,
  saveCombatState,
} from "../agent/lib/campaigns/combat-store.ts";
import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";
import { SqliteCampaignStore } from "../agent/lib/campaigns/store-sqlite.ts";
import { tempDb } from "./helpers.ts";

// Фикстура: temp-файл БД (CAMPAIGN_DB_PATH до первого обращения) + кампания.
const { path, cleanup } = tempDb("combat-persist");
const store = new SqliteCampaignStore(path);
after(() => store.close());
after(cleanup);

const slug = "persist-campaign";
store.createCampaign(
  { title: "persist-campaign", length: "medium", setting: "Подземелье", theme: "боевик" },
  { userId: "u-dm" },
);

// Каждый тест начинает без снимка боя. Первый вызов создаёт таблицы
// (DDL выполняется лениво), затем чистим их.
beforeEach(() => {
  clearCombatState(slug);
});

/** Детерминированный генератор: d20-броски (значения 1..20). */
function sequence(...rolls: number[]): () => number {
  let i = 0;
  return () => ((rolls[i++] ?? 10) - 0.5) / 20;
}

function sampleOrder(): CombatOrder {
  const order: CombatantEntry[] = rollInitiative(
    [
      { name: "Герой", side: "party", hp: 14, ac: 16 },
      { name: "Гоблин", side: "enemy", hp: 7, ac: 12 },
    ],
    sequence(18, 9),
  );
  return { started: true, round: 2, current: 0, acted: false, order };
}

describe("serialize/deserialize combat order", () => {
  test("round-trips a started combat with hp/ac", () => {
    const original = sampleOrder();
    const restored = deserializeCombatOrder(JSON.parse(JSON.stringify(serializeCombatOrder(original))));
    assert.deepEqual(restored, original);
  });

  test("preserves dodging and optional fields", () => {
    const order: CombatOrder = {
      started: true,
      round: 1,
      current: 1,
      acted: true,
      order: [
        { id: "g1", name: "Гоблин", side: "enemy", bonus: 1, roll: 10, total: 11, hp: 5, ac: 12, dodging: true },
        { id: "hero", name: "Герой", side: "party", bonus: 2, roll: 15, total: 17, maxHp: 20 },
      ],
    };
    const restored = deserializeCombatOrder(JSON.parse(JSON.stringify(serializeCombatOrder(order))));
    assert.deepEqual(restored, restored);
    assert.equal(restored!.order[0].dodging, true);
    assert.equal(restored!.order[1].maxHp, 20);
  });

  test("returns null for invalid data", () => {
    assert.equal(deserializeCombatOrder(null), null);
    assert.equal(deserializeCombatOrder({ started: "yes" }), null);
    assert.equal(deserializeCombatOrder({ started: true, round: 1, current: 0, acted: false, order: "no" }), null);
    // Боец без обязательных полей
    assert.equal(
      deserializeCombatOrder({ started: true, round: 1, current: 0, acted: false, order: [{ id: "x" }] }),
      null,
    );
  });
});

describe("combat-store save/load/clear", () => {
  test("returns null when no snapshot exists", () => {
    assert.equal(loadCombatState(slug), null);
  });

  test("saves and loads a combat snapshot", () => {
    const combat = sampleOrder();
    const enemies = [{ id: "goblin", name: "Гоблин", hp: 5, ac: 12 }];
    saveCombatState(slug, { combat, enemies });
    const loaded = loadCombatState(slug);
    assert.deepEqual(loaded!.combat, combat);
    assert.deepEqual(loaded!.enemies, enemies);
  });

  test("load returns null for a combat that is not started", () => {
    const combat = { ...emptyCombatOrder() }; // started: false
    saveCombatState(slug, { combat, enemies: [{ id: "e", name: "E", hp: 1, ac: 10 }] });
    assert.equal(loadCombatState(slug), null);
  });

  test("load returns null when enemies array is empty", () => {
    saveCombatState(slug, { combat: sampleOrder(), enemies: [] });
    assert.equal(loadCombatState(slug), null);
  });

  test("clear removes the snapshot and is idempotent", () => {
    saveCombatState(slug, { combat: sampleOrder(), enemies: [{ id: "g", name: "G", hp: 1, ac: 10 }] });
    assert.ok(loadCombatState(slug) !== null);
    clearCombatState(slug);
    assert.equal(loadCombatState(slug), null);
    // Повторный clear не падает.
    clearCombatState(slug);
  });

  test("load tolerates corrupted snapshot JSON (returns null)", () => {
    saveCombatState(slug, { combat: sampleOrder(), enemies: [{ id: "g", name: "G", hp: 1, ac: 10 }] });
    const campaignId = store.getCampaign(slug)!.id;
    openCampaignDb().prepare("UPDATE combat_snapshot SET combat = 'not json' WHERE campaign_id = ?").run(campaignId);
    assert.equal(loadCombatState(slug), null);
  });

  test("returns null for an unknown campaign without throwing", () => {
    assert.equal(loadCombatState("no-such-campaign"), null);
    saveCombatState("no-such-campaign", { combat: sampleOrder(), enemies: [{ id: "e", name: "E", hp: 1, ac: 10 }] });
    assert.equal(loadCombatState("no-such-campaign"), null);
    clearCombatState("no-such-campaign");
  });
});
