import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";

import { SqliteLocationStore } from "../agent/lib/campaigns/locations.ts";
import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";
import { campaignStore } from "../agent/lib/campaigns/store.ts";
import { StoreError } from "../agent/lib/campaigns/types.ts";
import { tempDb } from "./helpers.ts";

// Фикстура: temp-файл БД (CAMPAIGN_DB_PATH до первого обращения) + кампания.
const { cleanup } = tempDb("locations");
after(() => {
  // Закрываем соединение дефолтного стора, чтобы temp-папка удалилась
  // (Windows не даёт удалить файл с открытым соединением).
  (campaignStore as { close?: () => void }).close?.();
});
after(cleanup);

const store = new SqliteLocationStore();
const slug = "temnyy-les"; // slug of "Тёмный лес"
campaignStore.createCampaign(
  { title: "Тёмный лес", length: "medium", setting: "Лес", theme: "хоррор" },
  { userId: "u-dm" },
);

// Каждый тест начинает с пустого списка локаций. Первый вызов создаёт таблицы
// (DDL выполняется лениво), затем чистим их.
beforeEach(() => {
  store.listLocations(slug);
  openCampaignDb().exec("DELETE FROM locations;");
});

describe("SqliteLocationStore", () => {
  test("upsertLocation создаёт локацию с пустыми connections и visitedDays", () => {
    const loc = store.upsertLocation(slug, { name: "Мораг", description: "Деревня у ручья", discoveredDay: 1 });
    assert.equal(loc.name, "Мораг");
    assert.equal(loc.description, "Деревня у ручья");
    assert.equal(loc.discoveredDay, 1);
    assert.deepEqual(loc.connections, []);
    assert.deepEqual(loc.visitedDays, []);
    assert.equal(loc.current, undefined);
    assert.ok(loc.id);
  });

  test("upsertLocation обновляет локацию по имени без учёта регистра", () => {
    store.upsertLocation(slug, { name: "Мораг", discoveredDay: 1 });
    const updated = store.upsertLocation(slug, {
      name: "мораг",
      description: "Полуразрушенная деревня",
      connections: [{ to: "Чёрный лес", via: "тропа" }],
    });
    assert.equal(updated.description, "Полуразрушенная деревня");
    assert.equal(updated.discoveredDay, 1);
    assert.equal(updated.connections.length, 1);
    assert.equal(updated.connections[0].to, "Чёрный лес");
    assert.equal(store.listLocations(slug).length, 1);
  });

  test("current-флаг эксклюзивен: установка у одной снимает с остальных", () => {
    store.upsertLocation(slug, { name: "Мораг", current: true });
    store.upsertLocation(slug, { name: "Чёрный лес", current: true });
    const all = store.listLocations(slug);
    const morag = all.find((l) => l.name === "Мораг")!;
    const forest = all.find((l) => l.name === "Чёрный лес")!;
    assert.equal(morag.current, false);
    assert.equal(forest.current, true);
    assert.equal(store.currentLocation(slug)?.name, "Чёрный лес");
  });

  test("markVisited добавляет день без дубликатов и с сортировкой", () => {
    const loc = store.upsertLocation(slug, { name: "Мораг", discoveredDay: 1 });
    store.markVisited(slug, loc.slug, 3);
    store.markVisited(slug, loc.slug, 1);
    store.markVisited(slug, loc.slug, 3); // дубликат
    const reloaded = store.getLocation(slug, loc.slug)!;
    assert.deepEqual(reloaded.visitedDays, [1, 3]);
  });

  test("getLocation находит по id, slug и имени; неизвестный возвращает undefined", () => {
    const loc = store.upsertLocation(slug, { name: "Развалины" });
    assert.equal(store.getLocation(slug, loc.id)?.name, "Развалины");
    assert.equal(store.getLocation(slug, loc.slug)?.name, "Развалины");
    assert.equal(store.getLocation(slug, "развалины")?.name, "Развалины");
    assert.equal(store.getLocation(slug, "Нигде"), undefined);
  });

  test("setCurrent бросает StoreError для неизвестной локации", () => {
    assert.throws(
      () => store.setCurrent(slug, "несуществующая"),
      (error: unknown) => error instanceof StoreError && error.code === "not_found",
    );
  });

  test("одинаковое имя (разный регистр) сливается в одну локацию", () => {
    const first = store.upsertLocation(slug, { name: "Лагерь", discoveredDay: 1 });
    const second = store.upsertLocation(slug, { name: "лагерь", description: "Тент у ручья" });
    assert.equal(first.slug, second.slug);
    assert.equal(store.listLocations(slug).length, 1);
    assert.equal(second.discoveredDay, 1);
    assert.equal(second.description, "Тент у ручья");
  });

  test("connections с опциональными полями переживают перезагрузку", () => {
    store.upsertLocation(slug, {
      name: "Перевал",
      connections: [
        { to: "Мораг", via: "тропа", discoveredDay: 2 },
        { to: "Чёрный лес" },
      ],
    });
    const reloaded = store.getLocation(slug, "Перевал")!;
    assert.equal(reloaded.connections.length, 2);
    assert.deepEqual(reloaded.connections[0], { to: "Мораг", via: "тропа", discoveredDay: 2 });
    assert.deepEqual(reloaded.connections[1], { to: "Чёрный лес" });
  });
});
