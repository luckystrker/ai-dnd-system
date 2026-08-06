import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import { MarkdownNpcStore } from "../agent/lib/campaigns/npc.ts";
import { campaignStore } from "../agent/lib/campaigns/store.ts";
import { tempDir } from "./helpers.ts";

const { root, cleanup } = tempDir("npc-store");
process.env.CAMPAIGN_DATA_DIR = root;
process.env.CAMPAIGN_STORE = "markdown";
after(cleanup);

const store = new MarkdownNpcStore(root);
const slug = "temnyy-les"; // slug of "Тёмный лес"

beforeEach(() => {
  rmSync(join(root, slug), { recursive: true, force: true });
  campaignStore.createCampaign(
    { title: "Тёмный лес", length: "medium", setting: "Лес", theme: "хоррор" },
    { userId: "u-dm" },
  );
});

describe("MarkdownNpcStore", () => {
  test("upsertNpc creates a profile with alive status and empty memory", () => {
    const npc = store.upsertNpc(slug, { name: "Трактирщик Борн", role: "трактирщик" });
    assert.equal(npc.name, "Трактирщик Борн");
    assert.equal(npc.status, "alive");
    assert.equal(npc.role, "трактирщик");
    assert.equal(npc.memory, "");
    assert.ok(npc.id);
    assert.ok(npc.createdAt);
  });

  test("upsertNpc appends memory lines with day marks", () => {
    store.upsertNpc(slug, { name: "Борн", memoryAppend: "Игроки купили комнату.", memoryAppendDay: 1 });
    store.upsertNpc(slug, { name: "Борн", memoryAppend: "Борн видел странный свет." });
    const npc = store.getNpc(slug, "Борн")!;
    assert.match(npc.memory, /\[День 1\] Игроки купили комнату\./);
    assert.match(npc.memory, /Борн видел странный свет\./);
  });

  test("upsertNpc updates an existing npc by name case-insensitively", () => {
    store.upsertNpc(slug, { name: "Борн", status: "alive", firstSeenDay: 1 });
    const updated = store.upsertNpc(slug, { name: "борн", status: "dead", location: "морг" });
    assert.equal(updated.status, "dead");
    assert.equal(updated.location, "морг");
    assert.equal(updated.firstSeenDay, 1);
    assert.equal(store.listNpcs(slug).length, 1);
  });

  test("upsertNpc merges relationships per character name", () => {
    store.upsertNpc(slug, { name: "Борн", relationships: { "Ария": { attitude: 2 } } });
    const npc = store.upsertNpc(slug, {
      name: "Борн",
      relationships: { "Ария": { attitude: 3, notes: "должник" }, "Гимли": { attitude: -1 } },
    });
    assert.deepEqual(npc.relationships["Ария"], { attitude: 3, notes: "должник" });
    assert.deepEqual(npc.relationships["Гимли"], { attitude: -1 });
  });

  test("getNpc resolves by id, slug and name; unknown returns undefined", () => {
    const npc = store.upsertNpc(slug, { name: "Капитан стражи" });
    assert.equal(store.getNpc(slug, npc.id)?.name, "Капитан стражи");
    assert.equal(store.getNpc(slug, npc.slug)?.name, "Капитан стражи");
    assert.equal(store.getNpc(slug, "капитан стражи")?.name, "Капитан стражи");
    assert.equal(store.getNpc(slug, "Незнакомец"), undefined);
  });

  test("listNpcs survives a garbage file without losing valid profiles", () => {
    store.upsertNpc(slug, { name: "Первый" });
    store.upsertNpc(slug, { name: "Второй" });
    writeFileSync(join(root, slug, "npcs", "broken.md"), "garbage", "utf8");
    const names = store.listNpcs(slug).map((n) => n.name);
    assert.equal(names.length, 3);
    assert.deepEqual(names.filter(Boolean), ["Первый", "Второй"]);
  });

  test("colliding slug bases get numeric suffixes", () => {
    const first = store.upsertNpc(slug, { name: "Иван" });
    const second = store.upsertNpc(slug, { name: "ivan" });
    assert.equal(first.slug, "ivan");
    assert.equal(second.slug, "ivan-2");
    assert.equal(store.listNpcs(slug).length, 2);
  });
});
