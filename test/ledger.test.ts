import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";

import { appendLedgerRow, readLedger, readLedgerRaw } from "../agent/lib/campaigns/journal.ts";
import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";
import { SqliteCampaignStore } from "../agent/lib/campaigns/store-sqlite.ts";
import { tempDb } from "./helpers.ts";

// Фикстура: temp-файл БД (CAMPAIGN_DB_PATH до первого обращения) + кампания.
const { path, cleanup } = tempDb("ledger");
const store = new SqliteCampaignStore(path);
after(() => store.close());
after(cleanup);

const slug = "test-campaign";
store.createCampaign(
  { title: "test-campaign", length: "medium", setting: "s", theme: "t" },
  { userId: "u-dm" },
);

// Каждый тест начинает с пустого журнала экономики. Первый вызов журнальной
// функции создаёт таблицы (DDL выполняется лениво), затем чистим их.
beforeEach(() => {
  readLedger(slug);
  openCampaignDb().exec("DELETE FROM ledger_rows;");
});

describe("ledger", () => {
  test("appendLedgerRow пишет отформатированную строку", () => {
    appendLedgerRow(slug, { day: 3, type: "found", itemOrGold: "серебряный кинжал", by: "Дэн", note: "в сундуке" });
    const raw = readLedgerRaw(slug);
    assert.match(raw, /- \[День 3\] найдено: серебряный кинжал \(Дэн\) — в сундуке/);
  });

  test("appendLedgerRow дедуплицирует по eventId", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "50 золотых" }, "evt:g1");
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "50 золотых" }, "evt:g1");
    assert.equal(readLedger(slug).length, 1);
  });

  test("appendLedgerRow без eventId не дедуплицирует", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "10 золотых" });
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "10 золотых" });
    assert.equal(readLedger(slug).length, 2);
  });

  test("appendLedgerRow с пустым предметом — no-op", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "   " });
    assert.equal(readLedgerRaw(slug), "");
  });

  test("spent-тип использует глагол потрачено", () => {
    appendLedgerRow(slug, { day: 5, type: "spent", itemOrGold: "20 золотых", note: "на ночлег" });
    const raw = readLedgerRaw(slug);
    assert.match(raw, /- \[День 5\] потрачено: 20 золотых — на ночлег/);
  });

  test("readLedger фильтрует по дню", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "кинжал" });
    appendLedgerRow(slug, { day: 3, type: "found", itemOrGold: "меч" });
    assert.equal(readLedger(slug, { day: 3 }).length, 1);
    assert.match(readLedger(slug, { day: 3 })[0], /меч/);
  });

  test("readLedger фильтрует по типу", () => {
    appendLedgerRow(slug, { day: 1, type: "found", itemOrGold: "золото" });
    appendLedgerRow(slug, { day: 1, type: "spent", itemOrGold: "еда" });
    assert.equal(readLedger(slug, { type: "found" }).length, 1);
    assert.equal(readLedger(slug, { type: "spent" }).length, 1);
  });

  test("readLedger возвращает последние maxLines", () => {
    for (let i = 1; i <= 5; i++) {
      appendLedgerRow(slug, { day: i, type: "found", itemOrGold: `предмет ${i}` });
    }
    const tail = readLedger(slug, undefined, 2);
    assert.equal(tail.length, 2);
    assert.match(tail[1], /предмет 5/);
  });

  test("readLedger возвращает пустой список без записей", () => {
    assert.deepEqual(readLedger(slug), []);
  });
});
