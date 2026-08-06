import { test, describe, after, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";

import { SqliteCampaignStore } from "../agent/lib/campaigns/store-sqlite.ts";
import { StoreError, type BoundChat, type Campaign } from "../agent/lib/campaigns/types.ts";
import { tempDir } from "./helpers.ts";

describe("SqliteCampaignStore", () => {
  const { root, cleanup } = tempDir("sqlite-store");
  after(cleanup);

  let store: SqliteCampaignStore;

  beforeEach(() => {
    store = new SqliteCampaignStore(join(root, `campaigns-${Date.now()}-${Math.random()}.db`));
  });

  afterEach(() => store.close());

  const dm = { userId: "u-dm", name: "Ведущий", username: "dm" };

  function createCampaign(title = "Тени старого города"): Campaign {
    return store.createCampaign(
      { title, length: "medium", setting: "Нуарный город", theme: "детектив" },
      dm,
    );
  }

  test("createCampaign persists members and description", () => {
    const campaign = store.createCampaign(
      { title: "Описание в БД", length: "long", setting: "s", theme: "t", description: "Вступление..." },
      dm,
    );
    assert.equal(campaign.status, "setup");
    assert.equal(campaign.members[0].role, "dm");
    assert.equal(store.readDescription(campaign.id), "Вступление...");
  });

  test("getCampaign resolves by id and slug after reload from same db", () => {
    const campaign = createCampaign();
    const byId = store.getCampaign(campaign.id);
    const bySlug = store.getCampaign(campaign.slug);
    assert.equal(byId?.id, campaign.id);
    assert.equal(bySlug?.id, campaign.id);
  });

  test("bindAndActivate is null-safe on message thread", () => {
    const campaign = createCampaign("Топики");
    store.bindAndActivate(campaign.id, "u-dm", { chatId: "-100", messageThreadId: undefined });
    assert.equal(store.findByBoundChat("-100", undefined)?.id, campaign.id);
    assert.equal(store.findByBoundChat("-100", 5), undefined);
    assert.equal(store.findByBoundChat("-101"), undefined);
  });

  test("bindAndActivate finds campaigns bound to a specific thread", () => {
    const campaign = createCampaign("Форум");
    store.bindAndActivate(campaign.id, "u-dm", { chatId: "-100", messageThreadId: 7 });
    assert.equal(store.findByBoundChat("-100", 7)?.id, campaign.id);
    assert.equal(store.findByBoundChat("-100", undefined), undefined);
  });

  test("advanceDay increments current day", () => {
    const campaign = createCampaign("Дни");
    store.bindAndActivate(campaign.id, "u-dm", { chatId: "-1" });
    assert.equal(store.advanceDay(campaign.id, "u-dm").currentDay, 2);
  });

  test("addMember and autoRegister behave like the markdown store", () => {
    const campaign = createCampaign("Участники");
    store.addMember(campaign.id, "u-dm", { userId: "u1" });
    assert.throws(
      () => store.addMember(campaign.id, "u-dm", { userId: "u1" }),
      (err: StoreError) => err.code === "duplicate",
    );
    store.autoRegister(campaign.id, { userId: "u2", role: "dm" });
    const member = store.getCampaign(campaign.id)!.members.find((m) => m.userId === "u2")!;
    assert.equal(member.role, "player");
  });

  test("saveCharacter and updateCharacter round-trip state", () => {
    const campaign = createCampaign("Персонажи");
    store.addMember(campaign.id, "u-dm", { userId: "u-player" });
    const sheet = store.saveCharacter(campaign.id, "u-player", {
      name: "Ария",
      characterClass: "плут",
      race: "эльфийка",
      stats: { dex: 18 },
      appearance: "Silver hair",
    });
    assert.equal(sheet.slug, "ariya");
    const updated = store.updateCharacter(campaign.id, "Ария", {
      hp: 12,
      conditions: ["poisoned"],
      inventory: ["кинжал", "верёвка"],
    });
    assert.equal(updated.hp, 12);
    assert.deepEqual(updated.conditions, ["poisoned"]);
    assert.deepEqual(updated.inventory, ["кинжал", "верёвка"]);
    const reloaded = store.listCharacters(campaign.id)[0];
    assert.equal(reloaded.appearance, "Silver hair");
    assert.deepEqual(reloaded.stats, { dex: 18 });
  });

  test("upsertCampaign and upsertCharacter are idempotent (migration path)", () => {
    const campaign = createCampaign("Миграция");
    store.upsertCampaign(campaign, "описание");
    const sheet = store.saveCharacter(campaign.id, "u-dm", { name: "Герой", characterClass: "воин", race: "человек" });
    store.upsertCharacter(sheet);
    assert.equal(store.getCampaign(campaign.slug)?.title, campaign.title);
    assert.equal(store.listCharacters(campaign.id).length, 1);
  });

  test("role enforcement works", () => {
    const campaign = createCampaign("Права");
    store.addMember(campaign.id, "u-dm", { userId: "u-player" });
    assert.throws(
      () => store.advanceDay(campaign.id, "u-player"),
      (err: StoreError) => err.code === "access_denied",
    );
    assert.throws(
      () => store.saveCharacter(campaign.id, "u-outsider", { name: "X", characterClass: "y", race: "z" }),
      (err: StoreError) => err.code === "access_denied",
    );
  });

  test("a second store on the same file sees the data", () => {
    const path = join(root, `shared-${Date.now()}.db`);
    const writer = new SqliteCampaignStore(path);
    const campaign = writer.createCampaign(
      { title: "Общая база", length: "short", setting: "s", theme: "t" },
      dm,
    );
    writer.close();
    const reader = new SqliteCampaignStore(path);
    assert.equal(reader.getCampaign(campaign.slug)?.id, campaign.id);
    reader.close();
  });
});
