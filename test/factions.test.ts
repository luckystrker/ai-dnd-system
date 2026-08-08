import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { rmSync } from "node:fs";
import { join } from "node:path";

import { MarkdownFactionStore } from "../agent/lib/campaigns/factions.ts";
import { campaignStore } from "../agent/lib/campaigns/store.ts";
import { StoreError } from "../agent/lib/campaigns/types.ts";
import { tempDir } from "./helpers.ts";

const { root, cleanup } = tempDir("factions");
process.env.CAMPAIGN_DATA_DIR = root;
process.env.CAMPAIGN_STORE = "markdown";
after(cleanup);

const store = new MarkdownFactionStore(root);
const slug = "temnyy-les";

beforeEach(() => {
  rmSync(join(root, slug), { recursive: true, force: true });
  campaignStore.createCampaign(
    { title: "Тёмный лес", length: "medium", setting: "Лес", theme: "хоррор" },
    { userId: "u-dm" },
  );
});

describe("MarkdownFactionStore", () => {
  test("upsertFaction creates a faction with neutral standing", () => {
    const faction = store.upsertFaction(slug, { name: "Гильдия купцов", description: "Торговцы Морага" });
    assert.equal(faction.name, "Гильдия купцов");
    assert.equal(faction.standing, 0);
    assert.equal(faction.description, "Торговцы Морага");
    assert.ok(faction.id);
  });

  test("upsertFaction updates an existing faction by name case-insensitively", () => {
    store.upsertFaction(slug, { name: "Орден клинка", standing: 1 });
    const updated = store.upsertFaction(slug, { name: "орден клинка", description: "Рыцари" });
    assert.equal(updated.standing, 1);
    assert.equal(updated.description, "Рыцари");
    assert.equal(store.listFactions(slug).length, 1);
  });

  test("adjustStanding clamps to -5..+5", () => {
    const faction = store.upsertFaction(slug, { name: "Клан гномов", standing: 4 });
    store.adjustStanding(slug, faction.slug, 3); // 4 + 3 → 5
    assert.equal(store.getFaction(slug, faction.slug)?.standing, 5);
    store.adjustStanding(slug, faction.slug, -20); // 5 - 20 → -5
    assert.equal(store.getFaction(slug, faction.slug)?.standing, -5);
  });

  test("adjustStanding throws StoreError for an unknown faction", () => {
    assert.throws(
      () => store.adjustStanding(slug, "несуществующая", 1),
      (error: unknown) => error instanceof StoreError && error.code === "not_found",
    );
  });

  test("getFaction resolves by id, slug and name; unknown returns undefined", () => {
    const faction = store.upsertFaction(slug, { name: "Тайное общество" });
    assert.equal(store.getFaction(slug, faction.id)?.name, "Тайное общество");
    assert.equal(store.getFaction(slug, faction.slug)?.name, "Тайное общество");
    assert.equal(store.getFaction(slug, "тайное общество")?.name, "Тайное общество");
    assert.equal(store.getFaction(slug, "Незнакомцы"), undefined);
  });

  test("persists and reloads standing across reads", () => {
    store.upsertFaction(slug, { name: "Культ", standing: -3 });
    const reloaded = store.getFaction(slug, "Культ")!;
    assert.equal(reloaded.standing, -3);
  });
});
