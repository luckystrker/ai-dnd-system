import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { rmSync } from "node:fs";
import { join } from "node:path";

import { MarkdownLocationStore } from "../agent/lib/campaigns/locations.ts";
import { campaignStore } from "../agent/lib/campaigns/store.ts";
import { StoreError } from "../agent/lib/campaigns/types.ts";
import { tempDir } from "./helpers.ts";

const { root, cleanup } = tempDir("locations");
process.env.CAMPAIGN_DATA_DIR = root;
process.env.CAMPAIGN_STORE = "markdown";
after(cleanup);

const store = new MarkdownLocationStore(root);
const slug = "temnyy-les";

beforeEach(() => {
  rmSync(join(root, slug), { recursive: true, force: true });
  campaignStore.createCampaign(
    { title: "Тёмный лес", length: "medium", setting: "Лес", theme: "хоррор" },
    { userId: "u-dm" },
  );
});

describe("MarkdownLocationStore", () => {
  test("upsertLocation creates a location with empty connections and visitedDays", () => {
    const loc = store.upsertLocation(slug, { name: "Мораг", description: "Деревня у ручья", discoveredDay: 1 });
    assert.equal(loc.name, "Мораг");
    assert.equal(loc.description, "Деревня у ручья");
    assert.equal(loc.discoveredDay, 1);
    assert.deepEqual(loc.connections, []);
    assert.deepEqual(loc.visitedDays, []);
    assert.equal(loc.current, undefined);
    assert.ok(loc.id);
  });

  test("upsertLocation updates an existing location by name case-insensitively", () => {
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

  test("current flag is exclusive — setting it on one clears others", () => {
    store.upsertLocation(slug, { name: "Мораг", current: true });
    store.upsertLocation(slug, { name: "Чёрный лес", current: true });
    const all = store.listLocations(slug);
    const morag = all.find((l) => l.name === "Мораг")!;
    const forest = all.find((l) => l.name === "Чёрный лес")!;
    assert.equal(morag.current, false);
    assert.equal(forest.current, true);
    assert.equal(store.currentLocation(slug)?.name, "Чёрный лес");
  });

  test("markVisited adds the day without duplicates and sorted", () => {
    const loc = store.upsertLocation(slug, { name: "Мораг", discoveredDay: 1 });
    store.markVisited(slug, loc.slug, 3);
    store.markVisited(slug, loc.slug, 1);
    store.markVisited(slug, loc.slug, 3); // дубликат
    const reloaded = store.getLocation(slug, loc.slug)!;
    assert.deepEqual(reloaded.visitedDays, [1, 3]);
  });

  test("getLocation resolves by id, slug and name; unknown returns undefined", () => {
    const loc = store.upsertLocation(slug, { name: "Развалины" });
    assert.equal(store.getLocation(slug, loc.id)?.name, "Развалины");
    assert.equal(store.getLocation(slug, loc.slug)?.name, "Развалины");
    assert.equal(store.getLocation(slug, "развалины")?.name, "Развалины");
    assert.equal(store.getLocation(slug, "Нигде"), undefined);
  });

  test("setCurrent throws StoreError for an unknown location", () => {
    assert.throws(
      () => store.setCurrent(slug, "несуществующая"),
      (error: unknown) => error instanceof StoreError && error.code === "not_found",
    );
  });

  test("same name (different case) merges into one location, not a duplicate", () => {
    const first = store.upsertLocation(slug, { name: "Лагерь", discoveredDay: 1 });
    const second = store.upsertLocation(slug, { name: "лагерь", description: "Тент у ручья" });
    assert.equal(first.slug, second.slug);
    assert.equal(store.listLocations(slug).length, 1);
    assert.equal(second.discoveredDay, 1);
    assert.equal(second.description, "Тент у ручья");
  });

  test("persists and reloads connections with optional fields", () => {
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
