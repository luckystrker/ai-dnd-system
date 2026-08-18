import { test, describe, after } from "node:test";
import assert from "node:assert/strict";

import { appendKeyEvent, appendLedgerRow, appendTranscriptEntry } from "../agent/lib/campaigns/journal.ts";
import { SqliteFactionStore } from "../agent/lib/campaigns/factions.ts";
import { SqliteLocationStore } from "../agent/lib/campaigns/locations.ts";
import { SqliteNpcStore } from "../agent/lib/campaigns/npc.ts";
import {
  buildMatchQuery,
  queryTerms,
  searchCampaignMemory,
} from "../agent/lib/campaigns/search.ts";
import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";
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

// --- FTS5: морфология, скопинг, инкрементальный catch-up, fallback ---

describe("buildMatchQuery (FTS5)", () => {
  test("кавычки + префикс-звёздочка снаружи + AND-склейка", () => {
    assert.equal(buildMatchQuery(["тело", "ян"]), '"тело"* AND "ян"*');
  });
  test("пустой список термов даёт пустой запрос", () => {
    assert.equal(buildMatchQuery([]), "");
  });
});

describe("searchCampaignMemory (FTS5)", () => {
  test("префиксное совпадение словоформ: «прокляти» находит «проклятие»", () => {
    const hits = searchCampaignMemory(slug, "прокляти");
    assert.ok(hits.length >= 1, `expected hits, got ${JSON.stringify(hits)}`);
    assert.ok(
      hits.some((h) => h.source === "транскрипт, день 3" && h.snippet.includes("проклятие")),
      `missing day-3 transcript hit: ${JSON.stringify(hits)}`,
    );
  });

  test("AND через FTS: строка без одного терма не попадает в выдачу", () => {
    const hits = searchCampaignMemory(slug, "часовн каэль");
    assert.ok(hits.length >= 1, `expected hits, got ${JSON.stringify(hits)}`);
    assert.ok(hits.some((h) => h.snippet.includes("часовн")), `no chapel hit: ${JSON.stringify(hits)}`);
    // Ключевое событие «Каэль дал клятву» не содержит «часовн» — его нет.
    assert.ok(
      !hits.some((h) => h.source.startsWith("ключевое")),
      `key event without chapel leaked: ${JSON.stringify(hits)}`,
    );
  });

  test("fallback на JS-скан: совпадение внутри слова («эль» → «Каэль»)", () => {
    const hits = searchCampaignMemory(slug, "эль");
    assert.ok(hits.length >= 1, `expected substring hits, got ${JSON.stringify(hits)}`);
    assert.ok(hits.some((h) => h.snippet.includes("Каэль")), `no Каэль hit: ${JSON.stringify(hits)}`);
  });

  test("скопинг по кампании: записи другой кампании не всплывают", () => {
    const otherSlug = "other-campaign";
    store.createCampaign(
      { title: "other-campaign", length: "medium", setting: "Пустыня", theme: "выживание" },
      { userId: "u-dm" },
    );
    appendTranscriptEntry(otherSlug, 5, { kind: "dm", text: "Каэль — злой близнец в другом мире." });
    const hits = searchCampaignMemory(slug, "близнец");
    assert.deepEqual(hits, []);
    const own = searchCampaignMemory(slug, "Каэль");
    assert.ok(!own.some((h) => h.snippet.includes("близнец")), `foreign hit leaked: ${JSON.stringify(own)}`);
  });

  test("инкрементальный catch-up: новая запись находится вторым поиском", () => {
    appendTranscriptEntry(slug, 4, { kind: "dm", text: "Артефакт спрятан под алтарём." });
    const hits = searchCampaignMemory(slug, "артефакт");
    assert.ok(hits.length >= 1, `expected hit, got ${JSON.stringify(hits)}`);
    assert.ok(
      hits.some((h) => h.source === "транскрипт, день 4"),
      `missing day-4 hit: ${JSON.stringify(hits)}`,
    );
  });

  test("catch-up идемпотентен: повторный поиск и новые записи не дублируют хиты", () => {
    const before = searchCampaignMemory(slug, "артефакт").length;
    appendTranscriptEntry(slug, 4, { kind: "dm", text: "Каэль вспоминает про артефакт." });
    const after = searchCampaignMemory(slug, "артефакт").length;
    assert.ok(after > before, `expected new hit, got ${before} -> ${after}`);
    assert.equal(searchCampaignMemory(slug, "артефакт").length, after, "third search must not duplicate");
  });

  test("первый catch-up индексирует всю историю (fts_meta догоняет max id)", () => {
    const handle = openCampaignDb();
    const meta = handle.prepare(
      "SELECT last_source_id FROM fts_meta WHERE source = 'transcript_entries'",
    ).get() as { last_source_id: number } | undefined;
    assert.ok(meta, "fts_meta row for transcript_entries missing");
    const max = handle.prepare("SELECT MAX(id) AS m FROM transcript_entries").get() as { m: number | null };
    assert.ok(meta!.last_source_id >= (max.m ?? 0), `stale index: ${meta!.last_source_id} < ${max.m}`);
  });

  test("FTS покрывает журнал лута", () => {
    appendLedgerRow(slug, { day: 2, type: "found", itemOrGold: "Серебряный медальон" });
    const hits = searchCampaignMemory(slug, "медальон");
    assert.ok(
      hits.some((h) => h.source === "журнал лута, день 2"),
      `missing ledger hit: ${JSON.stringify(hits)}`,
    );
  });

  test("DELETE-триггер вычищает удалённые строки из индекса", () => {
    appendKeyEvent(slug, 2, "Древний клинок блестит во тьме.");
    const before = searchCampaignMemory(slug, "клинок");
    assert.ok(before.some((h) => h.source.startsWith("ключевое")), `no key-event hit: ${JSON.stringify(before)}`);
    const handle = openCampaignDb();
    handle.prepare("DELETE FROM key_events WHERE line LIKE '%клинок%'").run();
    const after = searchCampaignMemory(slug, "клинок");
    assert.ok(
      !after.some((h) => h.source.startsWith("ключевое")),
      `stale index hit after delete: ${JSON.stringify(after)}`,
    );
  });

  test("свежесть perSource: при избытке совпадений берутся самые новые дни", () => {
    for (const [day, text] of [
      [4, "Каэль идёт в лес."],
      [5, "Каэль находит следы."],
      [6, "Каэль встречает волков."],
      [7, "Каэль возвращается к часовне."],
    ] as const) {
      appendTranscriptEntry(slug, day, { kind: "dm", text });
    }
    const hits = searchCampaignMemory(slug, "Каэль", { perSource: 3 });
    const transcriptDays = hits
      .filter((h) => h.source.startsWith("транскрипт"))
      .map((h) => h.day!)
      .sort((a, b) => b - a);
    assert.deepEqual(transcriptDays, [7, 6, 5], `expected newest 3 days, got ${transcriptDays}`);
  });
});
