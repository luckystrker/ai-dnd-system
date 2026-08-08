import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { rmSync } from "node:fs";
import { join } from "node:path";

import { appendLedgerRow, readLedger, readLedgerRaw } from "../agent/lib/campaigns/journal.ts";
import { tempDir } from "./helpers.ts";

const { root, cleanup } = tempDir("ledger");
process.env.CAMPAIGN_DATA_DIR = root;
after(cleanup);

const slug = "test-campaign";

beforeEach(() => {
  rmSync(join(root, slug), { recursive: true, force: true });
});

describe("ledger", () => {
  test("appendLedgerRow writes a formatted line", () => {
    appendLedgerRow(slug, { day: 3, type: "found", itemOrGold: "серебряный кинжал", by: "Дэн", note: "в сундуке" });
    const raw = readLedgerRaw(slug);
    assert.match(raw, /- \[День 3\] найдено: серебряный кинжал \(Дэн\) — в сундуке/);
  });

  test("appendLedgerRow dedups by eventId", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "50 золотых" }, "evt:g1");
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "50 золотых" }, "evt:g1");
    assert.equal(readLedger(slug).length, 1);
  });

  test("appendLedgerRow without eventId does not dedup", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "10 золотых" });
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "10 золотых" });
    assert.equal(readLedger(slug).length, 2);
  });

  test("appendLedgerRow with empty item is a no-op", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "   " });
    assert.equal(readLedgerRaw(slug), "");
  });

  test("spent type uses the spent verb", () => {
    appendLedgerRow(slug, { day: 5, type: "spent", itemOrGold: "20 золотых", note: "на ночлег" });
    const raw = readLedgerRaw(slug);
    assert.match(raw, /- \[День 5\] потрачено: 20 золотых — на ночлег/);
  });

  test("readLedger filters by day", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "кинжал" });
    appendLedgerRow(slug, { day: 3, type: "found", itemOrGold: "меч" });
    assert.equal(readLedger(slug, { day: 3 }).length, 1);
    assert.match(readLedger(slug, { day: 3 })[0], /меч/);
  });

  test("readLedger filters by type", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "золото" });
    appendLedgerRow(slug, { day: 1, type: "spent", itemOrGold: "еда" });
    assert.equal(readLedger(slug, { type: "found" }).length, 1);
    assert.equal(readLedger(slug, { type: "spent" }).length, 1);
  });

  test("readLedger returns last maxLines", () => {
    for (let i = 1; i <= 5; i++) {
      appendLedgerRow(slug, { day: i, type: "found", itemOrGold: `предмет ${i}` });
    }
    const tail = readLedger(slug, undefined, 2);
    assert.equal(tail.length, 2);
    assert.match(tail[1], /предмет 5/);
  });

  test("readLedger returns empty when file missing", () => {
    assert.deepEqual(readLedger(slug), []);
  });
});
