import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { rmSync } from "node:fs";
import { join } from "node:path";

import { readWorldState, renderWorldState, upsertWorldChange } from "../agent/lib/campaigns/world-state.ts";
import { tempDir } from "./helpers.ts";

const { root, cleanup } = tempDir("world-state");
process.env.CAMPAIGN_DATA_DIR = root;
after(cleanup);

const slug = "test-campaign";

beforeEach(() => {
  rmSync(join(root, slug), { recursive: true, force: true });
});

describe("world-state", () => {
  test("upsertWorldChange writes a fact into a category", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    const cats = readWorldState(slug);
    assert.deepEqual(cats.get("Погибшие"), ["- Ян (день 1)"]);
  });

  test("upsertWorldChange is idempotent by category+text (updates day, no duplicate)", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 5 });
    const cats = readWorldState(slug);
    assert.equal(cats.get("Погибшие")!.length, 1);
    assert.deepEqual(cats.get("Погибшие"), ["- Ян (день 5)"]);
  });

  test("upsertWorldChange keeps different texts in the same category", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    upsertWorldChange(slug, { category: "Погибшие", text: "Марк", day: 2 });
    const cats = readWorldState(slug);
    assert.equal(cats.get("Погибшие")!.length, 2);
  });

  test("upsertWorldChange groups by category separately", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    upsertWorldChange(slug, { category: "Изменения", text: "Мораг спасён", day: 2 });
    const cats = readWorldState(slug);
    assert.equal(cats.get("Погибшие")!.length, 1);
    assert.equal(cats.get("Изменения")!.length, 1);
  });

  test("upsertWorldChange with empty text is a no-op", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "   " });
    assert.equal(readWorldState(slug).size, 0);
  });

  test("readWorldState returns empty map when file missing", () => {
    assert.equal(readWorldState(slug).size, 0);
  });

  test("renderWorldState produces human-readable category sections", () => {
    upsertWorldChange(slug, { category: "Погибшие", text: "Ян", day: 1 });
    upsertWorldChange(slug, { category: "Изменения", text: "Мораг благодарен" });
    const rendered = renderWorldState(slug);
    assert.match(rendered, /## Погибшие/);
    assert.match(rendered, /- Ян \(день 1\)/);
    assert.match(rendered, /## Изменения/);
    assert.match(rendered, /- Мораг благодарен/);
  });

  test("renderWorldState returns empty string when nothing recorded", () => {
    assert.equal(renderWorldState(slug), "");
  });

  test("default category is used when category is blank", () => {
    upsertWorldChange(slug, { category: "   ", text: "Что-то случилось" });
    const cats = readWorldState(slug);
    assert.ok(cats.has("Изменения"));
  });
});
