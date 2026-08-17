import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";

import { SqliteFactionStore } from "../agent/lib/campaigns/factions.ts";
import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";
import { campaignStore } from "../agent/lib/campaigns/store.ts";
import { StoreError } from "../agent/lib/campaigns/types.ts";
import { tempDb } from "./helpers.ts";

// Фикстура: temp-файл БД (CAMPAIGN_DB_PATH до первого обращения) + кампания.
const { cleanup } = tempDb("factions");
after(() => {
  // Закрываем соединение дефолтного стора, чтобы temp-папка удалилась
  // (Windows не даёт удалить файл с открытым соединением).
  (campaignStore as { close?: () => void }).close?.();
});
after(cleanup);

const store = new SqliteFactionStore();
const slug = "temnyy-les"; // slug of "Тёмный лес"
campaignStore.createCampaign(
  { title: "Тёмный лес", length: "medium", setting: "Лес", theme: "хоррор" },
  { userId: "u-dm" },
);

// Каждый тест начинает с пустого списка фракций. Первый вызов создаёт таблицы
// (DDL выполняется лениво), затем чистим их.
beforeEach(() => {
  store.listFactions(slug);
  openCampaignDb().exec("DELETE FROM factions;");
});

describe("SqliteFactionStore", () => {
  test("upsertFaction создаёт фракцию с нейтральным standing", () => {
    const faction = store.upsertFaction(slug, { name: "Гильдия купцов", description: "Торговцы Морага" });
    assert.equal(faction.name, "Гильдия купцов");
    assert.equal(faction.standing, 0);
    assert.equal(faction.description, "Торговцы Морага");
    assert.ok(faction.id);
  });

  test("upsertFaction обновляет фракцию по имени без учёта регистра", () => {
    store.upsertFaction(slug, { name: "Орден клинка", standing: 1 });
    const updated = store.upsertFaction(slug, { name: "орден клинка", description: "Рыцари" });
    assert.equal(updated.standing, 1);
    assert.equal(updated.description, "Рыцари");
    assert.equal(store.listFactions(slug).length, 1);
  });

  test("adjustStanding клампит в -5..+5", () => {
    const faction = store.upsertFaction(slug, { name: "Клан гномов", standing: 4 });
    store.adjustStanding(slug, faction.slug, 3); // 4 + 3 → 5
    assert.equal(store.getFaction(slug, faction.slug)?.standing, 5);
    store.adjustStanding(slug, faction.slug, -20); // 5 - 20 → -5
    assert.equal(store.getFaction(slug, faction.slug)?.standing, -5);
  });

  test("adjustStanding бросает StoreError для неизвестной фракции", () => {
    assert.throws(
      () => store.adjustStanding(slug, "несуществующая", 1),
      (error: unknown) => error instanceof StoreError && error.code === "not_found",
    );
  });

  test("getFaction находит по id, slug и имени; неизвестный возвращает undefined", () => {
    const faction = store.upsertFaction(slug, { name: "Тайное общество" });
    assert.equal(store.getFaction(slug, faction.id)?.name, "Тайное общество");
    assert.equal(store.getFaction(slug, faction.slug)?.name, "Тайное общество");
    assert.equal(store.getFaction(slug, "тайное общество")?.name, "Тайное общество");
    assert.equal(store.getFaction(slug, "Незнакомцы"), undefined);
  });

  test("standing переживает перезагрузку", () => {
    store.upsertFaction(slug, { name: "Культ", standing: -3 });
    const reloaded = store.getFaction(slug, "Культ")!;
    assert.equal(reloaded.standing, -3);
  });
});
