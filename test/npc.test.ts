import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";

import { SqliteNpcStore } from "../agent/lib/campaigns/npc.ts";
import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";
import { campaignStore } from "../agent/lib/campaigns/store.ts";
import { tempDb } from "./helpers.ts";

// Фикстура: temp-файл БД (CAMPAIGN_DB_PATH до первого обращения) + кампания.
const { cleanup } = tempDb("npc-store");
after(() => {
  // Закрываем соединение дефолтного стора, чтобы temp-папка удалилась
  // (Windows не даёт удалить файл с открытым соединением).
  (campaignStore as { close?: () => void }).close?.();
});
after(cleanup);

const store = new SqliteNpcStore();
const slug = "temnyy-les"; // slug of "Тёмный лес"
campaignStore.createCampaign(
  { title: "Тёмный лес", length: "medium", setting: "Лес", theme: "хоррор" },
  { userId: "u-dm" },
);

// Каждый тест начинает с пустого списка NPC. Первый вызов создаёт таблицы
// (DDL выполняется лениво), затем чистим их.
beforeEach(() => {
  store.listNpcs(slug);
  openCampaignDb().exec("DELETE FROM npcs;");
});

describe("SqliteNpcStore", () => {
  test("upsertNpc создаёт профиль со статусом alive и пустой памятью", () => {
    const npc = store.upsertNpc(slug, { name: "Трактирщик Борн", role: "трактирщик" });
    assert.equal(npc.name, "Трактирщик Борн");
    assert.equal(npc.status, "alive");
    assert.equal(npc.role, "трактирщик");
    assert.equal(npc.memory, "");
    assert.ok(npc.id);
    assert.ok(npc.createdAt);
  });

  test("upsertNpc дописывает строки памяти с метками дней", () => {
    store.upsertNpc(slug, { name: "Борн", memoryAppend: "Игроки купили комнату.", memoryAppendDay: 1 });
    store.upsertNpc(slug, { name: "Борн", memoryAppend: "Борн видел странный свет." });
    const npc = store.getNpc(slug, "Борн")!;
    assert.match(npc.memory, /\[День 1\] Игроки купили комнату\./);
    assert.match(npc.memory, /Борн видел странный свет\./);
  });

  test("upsertNpc не дублирует маркер дня, если memoryAppend уже содержит [День N]", () => {
    store.upsertNpc(slug, {
      name: "Борн",
      memoryAppend: "[День 1] Ночью Дэн вернулся в деревню.",
      memoryAppendDay: 1,
    });
    store.upsertNpc(slug, {
      name: "Борн",
      memoryAppend: "[День 2] [День 2] Кормак признался о голосе.",
      memoryAppendDay: 2,
    });
    const npc = store.getNpc(slug, "Борн")!;
    assert.ok(!npc.memory.includes("[День 1] [День 1]"), "не должно быть двойного маркера: " + npc.memory);
    assert.ok(!npc.memory.includes("[День 2] [День 2]"), "не должно быть двойного маркера: " + npc.memory);
    assert.match(npc.memory, /^\- \[День 1\] Ночью Дэн вернулся в деревню\./);
    assert.match(npc.memory, /\n\- \[День 2\] Кормак признался о голосе\./);
    assert.ok(npc.memory.split("\n").every((line) => (line.match(/\[День \d+\]/g) ?? []).length <= 1));
  });

  test("upsertNpc сохраняет встроенную дату, если memoryAppendDay не задан", () => {
    store.upsertNpc(slug, { name: "Борн", memoryAppend: "[День 3] Борн видел странный свет." });
    const npc = store.getNpc(slug, "Борн")!;
    assert.match(npc.memory, /\- \[День 3\] Борн видел странный свет\./);
    assert.ok(!npc.memory.includes("[День 3] [День 3]"));
  });

  test("upsertNpc обновляет существующего NPC по имени без учёта регистра", () => {
    store.upsertNpc(slug, { name: "Борн", status: "alive", firstSeenDay: 1 });
    const updated = store.upsertNpc(slug, { name: "борн", status: "dead", location: "морг" });
    assert.equal(updated.status, "dead");
    assert.equal(updated.location, "морг");
    assert.equal(updated.firstSeenDay, 1);
    assert.equal(store.listNpcs(slug).length, 1);
  });

  test("upsertNpc сливает отношения по именам персонажей", () => {
    store.upsertNpc(slug, { name: "Борн", relationships: { "Ария": { attitude: 2 } } });
    const npc = store.upsertNpc(slug, {
      name: "Борн",
      relationships: { "Ария": { attitude: 3, notes: "должник" }, "Гимли": { attitude: -1 } },
    });
    assert.deepEqual(npc.relationships["Ария"], { attitude: 3, notes: "должник" });
    assert.deepEqual(npc.relationships["Гимли"], { attitude: -1 });
  });

  test("getNpc находит по id, slug и имени; неизвестный возвращает undefined", () => {
    const npc = store.upsertNpc(slug, { name: "Капитан стражи" });
    assert.equal(store.getNpc(slug, npc.id)?.name, "Капитан стражи");
    assert.equal(store.getNpc(slug, npc.slug)?.name, "Капитан стражи");
    assert.equal(store.getNpc(slug, "капитан стражи")?.name, "Капитан стражи");
    assert.equal(store.getNpc(slug, "Незнакомец"), undefined);
  });

  test("коллизии базовых slug получают числовые суффиксы", () => {
    const first = store.upsertNpc(slug, { name: "Иван" });
    const second = store.upsertNpc(slug, { name: "ivan" });
    assert.equal(first.slug, "ivan");
    assert.equal(second.slug, "ivan-2");
    assert.equal(store.listNpcs(slug).length, 2);
  });

  test("lastMemoryLine возвращает последнюю строку памяти, обрезанную", () => {
    store.upsertNpc(slug, { name: "Борн", memoryAppend: "Видел героев.", memoryAppendDay: 1 });
    store.upsertNpc(slug, { name: "Борн", memoryAppend: "Герои спасли ему жизнь.", memoryAppendDay: 3 });
    const npc = store.getNpc(slug, "Борн")!;
    const line = store.lastMemoryLine(slug, npc.slug, 100);
    assert.ok(line.includes("спасли ему жизнь"));
    assert.ok(!line.includes("Видел героев"));
  });

  test("lastMemoryLine возвращает пустую строку для NPC без памяти", () => {
    const npc = store.upsertNpc(slug, { name: "Молчаливый" });
    assert.equal(store.lastMemoryLine(slug, npc.slug), "");
  });

  test("lastMemoryLine обрезает очень длинные строки", () => {
    const long = "А".repeat(300);
    const npc = store.upsertNpc(slug, { name: "Болтун", memoryAppend: long, memoryAppendDay: 1 });
    const line = store.lastMemoryLine(slug, npc.slug, 50);
    assert.ok(line.endsWith("…"));
    assert.ok(line.length <= 60);
  });
});
