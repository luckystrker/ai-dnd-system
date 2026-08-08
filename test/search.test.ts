import { test, describe, after, before } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { queryTerms, searchCampaignMemory } from "../agent/lib/campaigns/search.ts";
import { StoreError } from "../agent/lib/campaigns/types.ts";
import { tempDir } from "./helpers.ts";

const { root, cleanup } = tempDir("search");
process.env.CAMPAIGN_DATA_DIR = root;
after(cleanup);

const slug = "test-campaign";
const campaignRoot = join(root, slug);

before(() => {
  rmSync(campaignRoot, { recursive: true, force: true });
  // Транскрипт дня 1 с упоминанием Каэля.
  mkdirSync(join(campaignRoot, "history", "days"), { recursive: true });
  writeFileSync(
    join(campaignRoot, "history", "days", "day-0001.md"),
    "---\nday: 1\n---\n\n# Игровой день 1\n\n- [11:00] **Игрок @hero**: Я говорю с Каэлем у часовни.\n- [11:05] **DM**: Каэль обещает помочь, если принести тело Яна.\n",
    "utf8",
  );
  // Транскрипт дня 3 — упоминание Каэля в более позднем дне.
  writeFileSync(
    join(campaignRoot, "history", "days", "day-0003.md"),
    "---\nday: 3\n---\n\n# Игровой день 3\n\n- [09:00] **DM**: Каэль снимает проклятие в часовне.\n",
    "utf8",
  );
  // Карточка NPC Каэль.
  mkdirSync(join(campaignRoot, "npcs"), { recursive: true });
  writeFileSync(
    join(campaignRoot, "npcs", "kael.md"),
    '---\nname: "Каэль"\nrole: "Отшельник"\nstatus: "alive"\n---\n\n- [День 1] Договорился с партией.\n',
    "utf8",
  );
  // Ключевые события.
  writeFileSync(join(campaignRoot, "history", "key-events.md"), "- **День 1**: Каэль дал клятву.\n", "utf8");
});

describe("queryTerms", () => {
  test("splits into normalized lowercase words, length >= 2, dedups", () => {
    assert.deepEqual(queryTerms("Каэль, ЧАСОВНЯ! часовня"), ["каэль", "часовня"]);
  });
  test("ignores single chars and punctuation", () => {
    assert.deepEqual(queryTerms("а у K?"), []);
    assert.deepEqual(queryTerms("   "), []);
  });
});

describe("searchCampaignMemory", () => {
  test("finds matches across files and returns snippets with day", () => {
    const hits = searchCampaignMemory(slug, "Каэль");
    assert.ok(hits.length >= 3, `expected >=3 hits, got ${hits.length}`);
    // Свежий день (3) должен быть выше дня 1.
    const days = hits.filter((h) => h.day !== undefined).map((h) => h.day);
    assert.equal(days[0], 3);
    assert.ok(days.includes(1));
    for (const hit of hits) {
      assert.ok(hit.file.endsWith(".md"));
      assert.ok(hit.snippet.length > 0);
      assert.ok(hit.snippet.toLowerCase().includes("каэль"), `snippet missing term: ${hit.snippet}`);
    }
  });

  test("includes NPC and key-events files (day undefined for those)", () => {
    const hits = searchCampaignMemory(slug, "Каэль");
    const files = hits.map((h) => h.file);
    assert.ok(files.some((f) => f.startsWith("npcs/")), "expected a npcs/ hit");
    assert.ok(files.some((f) => f === "history/key-events.md"), "expected a key-events hit");
  });

  test("returns empty for no matches", () => {
    assert.deepEqual(searchCampaignMemory(slug, "несуществующееслово"), []);
  });

  test("respects limit option", () => {
    const hits = searchCampaignMemory(slug, "Каэль", { limit: 1 });
    assert.equal(hits.length, 1);
  });

  test("multiple terms match lines containing any term", () => {
    const hits = searchCampaignMemory(slug, "тело ян");
    assert.ok(hits.length >= 1);
    assert.ok(hits.some((h) => h.snippet.includes("тело")));
  });

  test("rejects path-traversal slug", () => {
    assert.throws(() => searchCampaignMemory("../etc", "x"), StoreError);
  });
});
