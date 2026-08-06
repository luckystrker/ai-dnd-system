import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  appendKeyEvent,
  appendTranscriptEntry,
  ensureDay,
  listDays,
  readCampaignSummary,
  readDay,
  readDayTail,
  readKeyEvents,
  setDaySummary,
  upsertCampaignSummaryDay,
} from "../agent/lib/campaigns/journal.ts";
import { tempDir } from "./helpers.ts";

const { root, cleanup } = tempDir("journal");
process.env.CAMPAIGN_DATA_DIR = root;
after(cleanup);

const slug = "test-campaign";
const dayPath = (day: number) =>
  join(root, slug, "history", "days", `day-${String(day).padStart(4, "0")}.md`);

beforeEach(() => {
  rmSync(join(root, slug), { recursive: true, force: true });
});

describe("ensureDay / listDays", () => {
  test("creates a day file with frontmatter", () => {
    ensureDay(slug, 1, { note: "Старт" });
    const content = readFileSync(dayPath(1), "utf8").replace(/\r/g, "");
    assert.match(content, /^day: 1$/m);
    assert.match(content, /^date: "\d{4}-\d{2}-\d{2}"$/m);
    assert.match(content, /^note: "Старт"$/m);
  });

  test("ensureDay is idempotent", () => {
    ensureDay(slug, 2, { note: "Первая версия" });
    ensureDay(slug, 2, { note: "Не перезапишется" });
    const content = readFileSync(dayPath(2), "utf8");
    assert.match(content, /Первая версия/);
    assert.ok(!content.includes("Не перезапишется"));
  });

  test("listDays returns only real day files in order", () => {
    ensureDay(slug, 2);
    ensureDay(slug, 10);
    ensureDay(slug, 1);
    writeFileSync(join(root, slug, "history", "days", "notes.txt"), "x", "utf8");
    assert.deepEqual(listDays(slug), [1, 2, 10]);
  });
});

describe("appendTranscriptEntry / readDay", () => {
  test("formats player, dm and action entries", () => {
    appendTranscriptEntry(slug, 1, { kind: "player", author: "gandalf", text: "Я осматриваю дверь." });
    appendTranscriptEntry(slug, 1, { kind: "dm", text: "Дверь заперта." });
    appendTranscriptEntry(slug, 1, { kind: "action", text: "Ария прячется в тенях." });
    const day = readDay(slug, 1)!;
    assert.equal(day.entries.length, 3);
    assert.match(day.entries[0], /^\-\s+\[\d{2}:\d{2}\] \*\*Игрок @gandalf\*\*: Я осматриваю дверь\./);
    assert.match(day.entries[1], /^\-\s+\[\d{2}:\d{2}\] \*\*DM\*\*: Дверь заперта\./);
    assert.match(day.entries[2], /^\-\s+\[\d{2}:\d{2}\] \*Ария прячется в тенях\.\*/);
  });

  test("deduplicates by eventId", () => {
    appendTranscriptEntry(slug, 1, { kind: "action", text: "Событие", eventId: "evt-1" });
    appendTranscriptEntry(slug, 1, { kind: "action", text: "Событие", eventId: "evt-1" });
    const day = readDay(slug, 1)!;
    assert.equal(day.entries.length, 1);
  });

  test("collapses newlines and skips empty text", () => {
    appendTranscriptEntry(slug, 1, { kind: "dm", text: "Первая\n\nстрока\n длинная" });
    appendTranscriptEntry(slug, 1, { kind: "dm", text: "   " });
    const day = readDay(slug, 1)!;
    assert.equal(day.entries.length, 1);
    assert.match(day.entries[0], /^\-\s+\[\d{2}:\d{2}\] \*\*DM\*\*: Первая строка длинная$/);
  });

  test("readDay returns undefined for a missing day", () => {
    assert.equal(readDay(slug, 99), undefined);
  });

  test("readDayTail limits the number of entries", () => {
    for (let i = 1; i <= 5; i++) {
      appendTranscriptEntry(slug, 1, { kind: "action", text: `Событие ${i}` });
    }
    const tail = readDayTail(slug, 1, 2)!;
    assert.deepEqual(tail.entries.map((e) => e.match(/Событие (\d)/)![1]), ["4", "5"]);
  });
});

describe("setDaySummary", () => {
  test("writes and updates summary keeping the body", () => {
    appendTranscriptEntry(slug, 1, { kind: "action", text: "Бой" });
    setDaySummary(slug, 1, "Герои победили гоблинов.");
    assert.equal(readDay(slug, 1)!.summary, "Герои победили гоблинов.");
    setDaySummary(slug, 1, "Обновлённое саммари.");
    assert.equal(readDay(slug, 1)!.summary, "Обновлённое саммари.");
    assert.ok(readDay(slug, 1)!.entries.length === 1);
  });
});

describe("upsertCampaignSummaryDay / readCampaignSummary", () => {
  test("appends new day sections and replaces existing ones", () => {
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
  test("appends events with day prefix and dedups by eventId", () => {
    appendKeyEvent(slug, 1, "Герои нашли артефакт.", "k1");
    appendKeyEvent(slug, 2, "Артефакт украден.");
    appendKeyEvent(slug, 1, "Герои нашли артефакт.", "k1");
    const events = readKeyEvents(slug);
    assert.match(events, /\*\*День 1\*\*: Герои нашли артефакт\./);
    assert.match(events, /\*\*День 2\*\*: Артефакт украден\./);
    assert.equal(events.match(/Герои нашли артефакт/g)!.length, 1);
  });

  test("skips empty events", () => {
    appendKeyEvent(slug, 1, "   \n ");
    assert.equal(readKeyEvents(slug), "");
  });
});
