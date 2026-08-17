import { test, describe, after } from "node:test";
import assert from "node:assert/strict";

import { appendKeyEvent, appendTranscriptEntry } from "../agent/lib/campaigns/journal.ts";
import { SqliteFactionStore } from "../agent/lib/campaigns/factions.ts";
import { SqliteLocationStore } from "../agent/lib/campaigns/locations.ts";
import { SqliteNpcStore } from "../agent/lib/campaigns/npc.ts";
import { queryTerms, searchCampaignMemory } from "../agent/lib/campaigns/search.ts";
import { SqliteCampaignStore } from "../agent/lib/campaigns/store-sqlite.ts";
import { campaignStore } from "../agent/lib/campaigns/store.ts";
import { tempDb } from "./helpers.ts";

// Фикстура: temp-файл БД (CAMPAIGN_DB_PATH до первого обращения) + кампания
// с записями в нескольких источниках памяти.
const { path, cleanup } = tempDb("search");
const store = new SqliteCampaignStore(path);
after(() => {
  store.close();
  // Сторы NPC/локаций/фракций резолвят кампанию через ленивый синглтон
  // campaignStore — закрываем и его, чтобы temp-папка удалилась (Windows не
  // даёт удалить файл с открытым соединением).
  (campaignStore as { close?: () => void }).close?.();
});
after(cleanup);

const slug = "test-campaign";
const npcStore = new SqliteNpcStore();
const locationStore = new SqliteLocationStore();
const factionStore = new SqliteFactionStore();

store.createCampaign(
  { title: "test-campaign", length: "medium", setting: "Тёмный лес", theme: "приключение" },
  { userId: "u-dm" },
);
// Транскрипт дня 1 с упоминанием Каэля.
appendTranscriptEntry(slug, 1, { kind: "player", author: "hero", text: "Я говорю с Каэлем у часовни." });
appendTranscriptEntry(slug, 1, { kind: "dm", text: "Каэль обещает помочь, если принести тело Яна." });
// Транскрипт дня 3 — Каэль в более позднем дне.
appendTranscriptEntry(slug, 3, { kind: "dm", text: "Каэль снимает проклятие в часовне." });
// Ключевые события.
appendKeyEvent(slug, 1, "Каэль дал клятву.");
// Карточка NPC Каэль.
npcStore.upsertNpc(slug, {
  name: "Каэль",
  role: "Отшельник",
  memoryAppend: "Договорился с партией.",
  memoryAppendDay: 1,
});
// Локация и фракция (поиск по описанию).
locationStore.upsertLocation(slug, { name: "Часовня", description: "Разрушенная часовня на холме" });
factionStore.upsertFaction(slug, { name: "Гильдия купцов", description: "Торговцы из Морага" });

describe("queryTerms", () => {
  test("разбивает на нормализованные слова длиной >= 2 с дедупом", () => {
    assert.deepEqual(queryTerms("Каэль, ЧАСОВНЯ! часовня"), ["каэль", "часовня"]);
  });
  test("игнорирует одиночные символы и пунктуацию", () => {
    assert.deepEqual(queryTerms("а у K?"), []);
    assert.deepEqual(queryTerms("   "), []);
  });
});

describe("searchCampaignMemory", () => {
  test("находит совпадения по источникам с днём и меткой источника", () => {
    const hits = searchCampaignMemory(slug, "Каэль");
    assert.ok(hits.length >= 4, `expected >=4 hits, got ${hits.length}`);
    const sources = hits.map((h) => h.source);
    assert.ok(sources.some((s) => s === "транскрипт, день 1"), `missing day-1 transcript: ${sources}`);
    assert.ok(sources.some((s) => s === "транскрипт, день 3"), `missing day-3 transcript: ${sources}`);
    assert.ok(sources.some((s) => s === "ключевое событие, день 1"), `missing key event: ${sources}`);
    assert.ok(sources.some((s) => s === "NPC Каэль"), `missing NPC hit: ${sources}`);
    for (const hit of hits) {
      assert.ok(hit.snippet.length > 0);
      assert.ok(hit.snippet.toLowerCase().includes("каэль"), `snippet missing term: ${hit.snippet}`);
    }
  });

  test("сортирует по дню по убыванию: свежий день первым", () => {
    const hits = searchCampaignMemory(slug, "Каэль");
    const withDay = hits.filter((h) => h.day !== undefined);
    assert.ok(withDay.length > 0);
    assert.equal(withDay[0].day, 3);
    const days = withDay.map((h) => h.day!);
    for (let i = 1; i < days.length; i++) {
      assert.ok(days[i] <= days[i - 1], `day order broken: ${days.join(", ")}`);
    }
  });

  test("AND-логика: строка должна содержать каждый терм", () => {
    const hits = searchCampaignMemory(slug, "тело ян");
    assert.ok(hits.length >= 1);
    assert.ok(hits.some((h) => h.snippet.includes("тело")));
    for (const hit of hits) {
      assert.ok(hit.snippet.toLowerCase().includes("ян"), `snippet missing term: ${hit.snippet}`);
    }
  });

  test("ищет по описаниям локаций и фракций", () => {
    const chapel = searchCampaignMemory(slug, "часовня");
    assert.ok(chapel.some((h) => h.source === "локация Часовня"), `no location hit: ${JSON.stringify(chapel)}`);
    const morag = searchCampaignMemory(slug, "мораг");
    assert.ok(morag.some((h) => h.source === "фракция Гильдия купцов"), `no faction hit: ${JSON.stringify(morag)}`);
  });

  test("возвращает пустой список при отсутствии совпадений", () => {
    assert.deepEqual(searchCampaignMemory(slug, "несуществующееслово"), []);
  });

  test("возвращает пустой список для неизвестной кампании", () => {
    assert.deepEqual(searchCampaignMemory("no-such-campaign", "Каэль"), []);
  });

  test("учитывает опцию limit", () => {
    const hits = searchCampaignMemory(slug, "Каэль", { limit: 2 });
    assert.equal(hits.length, 2);
  });

  test("учитывает опцию perSource", () => {
    const hits = searchCampaignMemory(slug, "Каэль", { perSource: 1 });
    assert.equal(hits.filter((h) => h.source.startsWith("транскрипт")).length, 1);
  });

  test("запрос только из коротких слов ничего не ищет", () => {
    assert.deepEqual(searchCampaignMemory(slug, "а у"), []);
  });
});
