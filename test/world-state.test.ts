import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";

import { readWorldState, renderWorldState, upsertWorldChange } from "../agent/lib/campaigns/world-state.ts";
import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";
import { SqliteCampaignStore } from "../agent/lib/campaigns/store-sqlite.ts";
import { tempDb } from "./helpers.ts";

// Фикстура: temp-файл БД (CAMPAIGN_DB_PATH до первого обращения) + кампания.
const { path, cleanup } = tempDb("world-state");
const store = new SqliteCampaignStore(path);
after(() => store.close());
after(cleanup);

const slug = "test-campaign";
store.createCampaign(
  { title: "test-campaign", length: "medium", setting: "Лес", theme: "приключение" },
  { userId: "u-dm" },
);

// Каждый тест начинает с пустого состояния мира. Первый вызов создаёт таблицы
// (DDL выполняется лениво), затем чистим их.
beforeEach(() => {
  readWorldState(slug);
  openCampaignDb().exec("DELETE FROM world_changes;");
});

describe("world-state", () => {
  test("upsertWorldChange записывает факт в категорию", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    const cats = readWorldState(slug);
    assert.deepEqual(cats.get("Погибшие"), ["- Ян (день 1)"]);
  });

  test("upsertWorldChange идемпотентен по категории+тексту (обновляет день)", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 5 });
    const cats = readWorldState(slug);
    assert.equal(cats.get("Погибшие")!.length, 1);
    assert.deepEqual(cats.get("Погибшие"), ["- Ян (день 5)"]);
  });

  test("upsertWorldChange хранит разные тексты в одной категории", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    upsertWorldChange(slug, { category: "Погибшие", text: "Марк", day: 2 });
    const cats = readWorldState(slug);
    assert.equal(cats.get("Погибшие")!.length, 2);
  });

  test("upsertWorldChange группирует по категориям раздельно", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    upsertWorldChange(slug, { category: "Изменения", text: "Мораг спасён", day: 2 });
    const cats = readWorldState(slug);
    assert.equal(cats.get("Погибшие")!.length, 1);
    assert.equal(cats.get("Изменения")!.length, 1);
  });

  test("upsertWorldChange с пустым текстом — no-op", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "   " });
    assert.equal(readWorldState(slug).size, 0);
  });

  test("readWorldState возвращает пустую карту для неизвестной кампании", () => {
    assert.equal(readWorldState("no-such-campaign").size, 0);
  });

  test("renderWorldState даёт читаемые секции категорий", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    upsertWorldChange(slug, { category: "Изменения", text: "Мораг благодарен" });
    const rendered = renderWorldState(slug);
    assert.match(rendered, /## Погибшие/);
    assert.match(rendered, /- Ян \(день 1\)/);
    assert.match(rendered, /## Изменения/);
    assert.match(rendered, /- Мораг благодарен/);
  });

  test("renderWorldState возвращает пустую строку, когда ничего не записано", () => {
    assert.equal(renderWorldState(slug), "");
    assert.equal(renderWorldState("no-such-campaign"), "");
  });

  test("дефолтная категория используется при пустой", () => {
    upsertWorldChange(slug, { category: "   ", text: "Что-то случилось" });
    const cats = readWorldState(slug);
    assert.ok(cats.has("Изменения"));
  });

  test("запись без дня рендерится без пометки", () => {
    upsertWorldChange(slug, { category: "Изменения", text: "Мораг благодарен" });
    assert.deepEqual(readWorldState(slug).get("Изменения"), ["- Мораг благодарен"]);
  });
});
