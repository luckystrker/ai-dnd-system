import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";

import {
  appendKeyEvent,
  appendTranscriptEntry,
  buildDayDigest,
  AUTO_DIGEST_MARK,
  ensureDay,
  listDayHeadlines,
  listDays,
  readCampaignSummary,
  readDay,
  readDayTail,
  readKeyEvents,
  setDayHeadline,
  setDaySummary,
  splitKeyEvents,
  upsertCampaignSummaryDay,
} from "../agent/lib/campaigns/journal.ts";
import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";
import { SqliteCampaignStore } from "../agent/lib/campaigns/store-sqlite.ts";
import { tempDb } from "./helpers.ts";

// Фикстура: temp-файл БД (CAMPAIGN_DB_PATH до первого обращения) + кампания.
const { path, cleanup } = tempDb("journal");
const store = new SqliteCampaignStore(path);
after(() => store.close());
after(cleanup);

const slug = "test-campaign";
store.createCampaign(
  { title: "test-campaign", length: "medium", setting: "Лес", theme: "приключение" },
  { userId: "u-dm" },
);

// Каждый тест начинает с пустого журнала — как свежая кампания. Первый вызов
// журнальной функции создаёт таблицы (DDL выполняется лениво), затем чистим их.
beforeEach(() => {
  listDays(slug);
  openCampaignDb().exec(
    "DELETE FROM transcript_entries; DELETE FROM key_events; DELETE FROM ledger_rows; DELETE FROM campaign_summary; DELETE FROM campaign_days;",
  );
});

describe("ensureDay / listDays", () => {
  test("ensureDay создаёт метаданные дня", () => {
    ensureDay(slug, 1, { note: "Старт" });
    const day = readDay(slug, 1)!;
    assert.match(day.date ?? "", /^\d{4}-\d{2}-\d{2}$/);
    assert.equal(day.note, "Старт");
  });

  test("ensureDay идемпотентен", () => {
    ensureDay(slug, 2, { note: "Первая версия" });
    ensureDay(slug, 2, { note: "Не перезапишется" });
    assert.equal(readDay(slug, 2)!.note, "Первая версия");
  });

  test("listDays возвращает дни по возрастанию", () => {
    ensureDay(slug, 2);
    ensureDay(slug, 10);
    ensureDay(slug, 1);
    assert.deepEqual(listDays(slug), [1, 2, 10]);
  });
});

describe("appendTranscriptEntry / readDay", () => {
  test("форматирует записи player, dm и action", () => {
    appendTranscriptEntry(slug, 1, { kind: "player", author: "gandalf", text: "Я осматриваю дверь." });
    appendTranscriptEntry(slug, 1, { kind: "dm", text: "Дверь заперта." });
    appendTranscriptEntry(slug, 1, { kind: "action", text: "Ария прячется в тенях." });
    const day = readDay(slug, 1)!;
    assert.equal(day.entries.length, 3);
    assert.match(day.entries[0], /^\-\s+\[\d{2}:\d{2}\] \*\*Игрок @gandalf\*\*: Я осматриваю дверь\./);
    assert.match(day.entries[1], /^\-\s+\[\d{2}:\d{2}\] \*\*DM\*\*: Дверь заперта\./);
    assert.match(day.entries[2], /^\-\s+\[\d{2}:\d{2}\] \*Ария прячется в тенях\.\*/);
  });

  test("дедуплицирует по eventId", () => {
    appendTranscriptEntry(slug, 1, { kind: "action", text: "Событие", eventId: "evt-1" });
    appendTranscriptEntry(slug, 1, { kind: "action", text: "Событие", eventId: "evt-1" });
    const day = readDay(slug, 1)!;
    assert.equal(day.entries.length, 1);
  });

  test("схлопывает переводы строк и пропускает пустой текст", () => {
    appendTranscriptEntry(slug, 1, { kind: "dm", text: "Первая\n\nстрока\n длинная" });
    appendTranscriptEntry(slug, 1, { kind: "dm", text: "   " });
    const day = readDay(slug, 1)!;
    assert.equal(day.entries.length, 1);
    assert.match(day.entries[0], /^\-\s+\[\d{2}:\d{2}\] \*\*DM\*\*: Первая строка длинная$/);
  });

  test("readDay возвращает undefined для отсутствующего дня", () => {
    assert.equal(readDay(slug, 99), undefined);
  });

  test("readDayTail ограничивает число записей", () => {
    for (let i = 1; i <= 5; i++) {
      appendTranscriptEntry(slug, 1, { kind: "action", text: `Событие ${i}` });
    }
    const tail = readDayTail(slug, 1, 2)!;
    assert.deepEqual(tail.entries.map((e) => e.match(/Событие (\d)/)![1]), ["4", "5"]);
  });
});

describe("setDaySummary", () => {
  test("записывает и обновляет саммари, сохраняя записи дня", () => {
    appendTranscriptEntry(slug, 1, { kind: "action", text: "Бой" });
    setDaySummary(slug, 1, "Герои победили гоблинов.");
    assert.equal(readDay(slug, 1)!.summary, "Герои победили гоблинов.");
    setDaySummary(slug, 1, "Обновлённое саммари.");
    assert.equal(readDay(slug, 1)!.summary, "Обновлённое саммари.");
    assert.ok(readDay(slug, 1)!.entries.length === 1);
  });
});

describe("upsertCampaignSummaryDay / readCampaignSummary", () => {
  test("добавляет секции дней и заменяет существующие", () => {
    upsertCampaignSummaryDay(slug, 1, "День первый: знакомство.");
    upsertCampaignSummaryDay(slug, 2, "День второй: бой.");
    assert.equal(
      readCampaignSummary(slug),
      "## День 1\n\nДень первый: знакомство.\n\n## День 2\n\nДень второй: бой.",
    );
    upsertCampaignSummaryDay(slug, 1, "День первый: переписано.");
    assert.match(readCampaignSummary(slug), /## День 1\n\nДень первый: переписано\./);
    assert.match(readCampaignSummary(slug), /## День 2\n\nДень второй: бой\./);
  });
});

describe("appendKeyEvent / readKeyEvents", () => {
  test("добавляет события с префиксом дня и дедуплицирует по eventId", () => {
    appendKeyEvent(slug, 1, "Герои нашли артефакт.", "k1");
    appendKeyEvent(slug, 2, "Артефакт украден.");
    appendKeyEvent(slug, 1, "Герои нашли артефакт.", "k1");
    const events = readKeyEvents(slug);
    assert.match(events, /\*\*День 1\*\*: Герои нашли артефакт\./);
    assert.match(events, /\*\*День 2\*\*: Артефакт украден\./);
    assert.equal(events.match(/Герои нашли артефакт/g)!.length, 1);
  });

  test("пропускает пустые события", () => {
    appendKeyEvent(slug, 1, "   \n ");
    assert.equal(readKeyEvents(slug), "");
  });

  test("помечает permanent-события и splitKeyEvents разделяет их", () => {
    appendKeyEvent(slug, 1, "Обычное событие.");
    appendKeyEvent(slug, 1, "Каэль дал нерушимую клятву.", undefined, true);
    appendKeyEvent(slug, 2, "Город пал.", undefined, true);
    const events = readKeyEvents(slug);
    assert.ok(events.includes("- [важно] **День 1**:"));
    assert.ok(events.includes("- **День 1**: Обычное событие."));
    const { permanent, regular } = splitKeyEvents(slug);
    assert.equal(permanent.length, 2);
    assert.equal(regular.length, 1);
    assert.ok(permanent[0].startsWith("- [важно] "));
    assert.ok(regular[0].startsWith("- "));
    assert.ok(!regular[0].startsWith("- [важно] "));
  });
});

describe("setDayHeadline / listDayHeadlines", () => {
  test("пишет компактную шапку для каждого дня", () => {
    appendTranscriptEntry(slug, 1, { kind: "action", text: "Что-то" });
    appendTranscriptEntry(slug, 2, { kind: "action", text: "Иное" });
    setDayHeadline(slug, 1, "Прибытие в Мораг");
    setDayHeadline(slug, 2, "Бой у часовни");
    const headlines = listDayHeadlines(slug);
    assert.deepEqual(
      headlines,
      [
        { day: 1, headline: "Прибытие в Мораг" },
        { day: 2, headline: "Бой у часовни" },
      ],
    );
  });

  test("шапка читается через readDayTail и обрезается до 140 символов", () => {
    appendTranscriptEntry(slug, 1, { kind: "action", text: "x" });
    const long = "А".repeat(200);
    setDayHeadline(slug, 1, long);
    const record = readDayTail(slug, 1, 1)!;
    assert.equal(record.headline!.length, 140);
  });

  test("дни без шапки пропускаются", () => {
    appendTranscriptEntry(slug, 1, { kind: "action", text: "x" });
    appendTranscriptEntry(slug, 2, { kind: "action", text: "y" });
    setDayHeadline(slug, 2, "Второй день");
    const headlines = listDayHeadlines(slug);
    assert.deepEqual(headlines, [{ day: 2, headline: "Второй день" }]);
  });
});

describe("buildDayDigest", () => {
  test("строит дайджест с маркером из записей транскрипта", () => {
    const digest = buildDayDigest([
      "- [11:00] **Игрок @hero**: Я осматриваю дверь.",
      "- [11:05] **DM**: Дверь заперта.",
    ]);
    assert.ok(digest.startsWith(AUTO_DIGEST_MARK));
    assert.ok(digest.includes("осматриваю дверь"));
    assert.ok(digest.includes("Дверь заперта"));
    assert.ok(!digest.includes("[11:00]"));
  });

  test("убирает маркеры дедупа evt", () => {
    const digest = buildDayDigest(["- [12:00] *Бросок костей* <!-- evt:abc -->"]);
    assert.ok(!digest.includes("evt:abc"));
  });

  test("возвращает пустую строку без записей", () => {
    assert.equal(buildDayDigest([]), "");
  });

  test("ограничивается последними записями", () => {
    const entries = Array.from({ length: 20 }, (_, i) => `- [10:${String(i).padStart(2, "0")}] **DM**: событие ${i}`);
    const digest = buildDayDigest(entries);
    assert.ok(digest.includes("событие 19"));
    assert.ok(!digest.includes("событие 0"));
  });
});
