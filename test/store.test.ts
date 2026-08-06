import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  MarkdownCampaignStore,
  assertCampaignSlug,
  slugify,
  type NewCharacterInput,
  type NewOwnerInput,
} from "../agent/lib/campaigns/store.ts";
import { StoreError, MAX_PARTY, type BoundChat, type Campaign } from "../agent/lib/campaigns/types.ts";
import { tempDir } from "./helpers.ts";

describe("slugify", () => {
  const cases: Array<[string, string]> = [
    ["Восставшие из пепла", "vosstavshie-iz-pepla"],
    ["The Dragon's Hoard", "the-dragon-s-hoard"],
    ["  Тень   и  Кость  ", "ten-i-kost"],
    ["Ёлка и йод", "elka-i-yod"],
    ["ЪъъЪ", ""],
    ["", ""],
    ["---foo---", "foo"],
    ["Загадка: «Тёмный лес»!", "zagadka-temnyy-les"],
  ];
  for (const [input, expected] of cases) {
    test(`${JSON.stringify(input)} -> ${JSON.stringify(expected)}`, () => {
      assert.equal(slugify(input), expected || "untitled");
    });
  }

  test("caps length at 60 characters", () => {
    const slug = slugify("a".repeat(200));
    assert.ok(slug.length <= 60);
  });
});

describe("assertCampaignSlug", () => {
  test("accepts valid slugs", () => {
    for (const slug of ["campaign", "my-campaign", "a1-b2", "1"]) {
      assert.doesNotThrow(() => assertCampaignSlug(slug));
    }
  });

  test("rejects unsafe slugs", () => {
    for (const slug of ["", "Campaign", "my/campaign", "..", "campaign.md", "a b", "caf\u00e9", "-x", "x-", "a".repeat(61)]) {
      assert.throws(() => assertCampaignSlug(slug), StoreError);
    }
  });
});

describe("MarkdownCampaignStore", () => {
  const { root, cleanup } = tempDir("md-store");
  after(cleanup);

  const store = new MarkdownCampaignStore(root);
  const dm: NewOwnerInput = { userId: "u-dm", name: "Ведущий", username: "dm" };
  let chatCounter = 0;
  const chat = (): BoundChat => ({ chatId: `-100${Date.now()}-${++chatCounter}`, messageThreadId: undefined });

  function createCampaign(title = "Тени старого города"): Campaign {
    return store.createCampaign(
      { title, length: "medium", setting: "Нуарный город", theme: "детектив" },
      dm,
    );
  }

  test("createCampaign assigns slug, dm role and setup status", () => {
    const campaign = createCampaign("Восставшие из пепла");
    assert.equal(campaign.slug, "vosstavshie-iz-pepla");
    assert.equal(campaign.status, "setup");
    assert.equal(campaign.ownerUserId, "u-dm");
    assert.equal(campaign.members.length, 1);
    assert.deepEqual(campaign.members[0], { userId: "u-dm", name: "Ведущий", username: "dm", role: "dm" });
    assert.ok(campaign.createdAt);
  });

  test("creates unique slugs for duplicate titles", () => {
    const first = createCampaign("Одинаковые названия");
    const second = createCampaign("Одинаковые названия");
    assert.notEqual(first.slug, second.slug);
    assert.equal(second.slug, `${first.slug}-2`);
  });

  test("getCampaign resolves by id and by slug", () => {
    const campaign = createCampaign();
    assert.equal(store.getCampaign(campaign.id)?.id, campaign.id);
    assert.equal(store.getCampaign(campaign.slug)?.id, campaign.id);
    assert.equal(store.getCampaign("nope"), undefined);
  });

  test("listCampaigns returns newest first", () => {
    const first = createCampaign("Список А");
    const second = createCampaign("Список Б");
    const listed = store.listCampaigns();
    assert.deepEqual(listed.slice(0, 2).map((c) => c.id), [second.id, first.id]);
  });

  test("listForUser filters by membership", () => {
    const campaign = createCampaign();
    assert.equal(store.listForUser("u-dm").some((c) => c.id === campaign.id), true);
    assert.equal(store.listForUser("u-stranger").some((c) => c.id === campaign.id), false);
  });

  test("skips corrupted campaign files", () => {
    const campaign = createCampaign();
    writeFileSync(join(root, campaign.slug, "campaign.md"), "not a frontmatter doc", "utf8");
    assert.ok(!store.listCampaigns().some((c) => c.id === campaign.id));
    assert.ok(store.listCampaigns().length >= 1);
  });

  test("bindAndActivate binds chat and starts at day 1", () => {
    const campaign = createCampaign("Привязка чата");
    const boundChat = chat();
    const bound = store.bindAndActivate(campaign.id, "u-dm", boundChat);
    assert.equal(bound.status, "active");
    assert.equal(bound.currentDay, 1);
    assert.deepEqual(bound.boundChat, boundChat);
  });

  test("bindAndActivate requires dm role", () => {
    const campaign = createCampaign("Привязка без прав");
    store.addMember(campaign.id, "u-dm", { userId: "u-player" });
    assert.throws(
      () => store.bindAndActivate(campaign.id, "u-player", chat()),
      (err: StoreError) => err.code === "access_denied",
    );
  });

  test("bindAndActivate rejects already active campaigns", () => {
    const campaign = createCampaign("Двойная привязка");
    const boundChat = chat();
    store.bindAndActivate(campaign.id, "u-dm", boundChat);
    assert.throws(
      () => store.bindAndActivate(campaign.id, "u-dm", boundChat),
      (err: StoreError) => err.code === "conflict",
    );
  });

  test("bindAndActivate rejects a second campaign in the same chat", () => {
    const first = createCampaign("Чат занят 1");
    const second = createCampaign("Чат занят 2");
    const boundChat = chat();
    store.bindAndActivate(first.id, "u-dm", boundChat);
    assert.throws(
      () => store.bindAndActivate(second.id, "u-dm", boundChat),
      (err: StoreError) => err.code === "conflict",
    );
  });

  test("advanceDay increments the day for the dm", () => {
    const campaign = createCampaign("Дни");
    store.bindAndActivate(campaign.id, "u-dm", chat());
    const advanced = store.advanceDay(campaign.id, "u-dm");
    assert.equal(advanced.currentDay, 2);
    assert.equal(store.getCampaign(campaign.id)?.currentDay, 2);
  });

  test("advanceDay requires active status and dm role", () => {
    const campaign = createCampaign("Дни без старта");
    assert.throws(
      () => store.advanceDay(campaign.id, "u-dm"),
      (err: StoreError) => err.code === "conflict",
    );
    store.bindAndActivate(campaign.id, "u-dm", chat());
    store.addMember(campaign.id, "u-dm", { userId: "u-player" });
    assert.throws(
      () => store.advanceDay(campaign.id, "u-player"),
      (err: StoreError) => err.code === "access_denied",
    );
  });

  test("addMember adds players and rejects duplicates", () => {
    const campaign = createCampaign("Участники");
    const updated = store.addMember(campaign.id, "u-dm", { userId: "u1", name: "Игрок" });
    assert.equal(updated.members.length, 2);
    assert.equal(updated.members[1].role, "player");
    assert.throws(
      () => store.addMember(campaign.id, "u-dm", { userId: "u1" }),
      (err: StoreError) => err.code === "duplicate",
    );
  });

  test("addMember restricts dm role assignment to the owner", () => {
    const campaign = createCampaign("Роли");
    store.addMember(campaign.id, "u-dm", { userId: "u-helper", role: "dm" });
    assert.throws(
      () => store.addMember(campaign.id, "u-helper", { userId: "u-x", role: "dm" }),
      (err: StoreError) => err.code === "access_denied",
    );
  });

  test("autoRegister is idempotent and downgrades dm requests to player", () => {
    const campaign = createCampaign("Авто-вступление");
    store.autoRegister(campaign.id, { userId: "u1", role: "dm" });
    const member = store.getCampaign(campaign.id)!.members.find((m) => m.userId === "u1")!;
    assert.equal(member.role, "player");
    const again = store.autoRegister(campaign.id, { userId: "u1" });
    assert.equal(again.members.length, 2);
  });

  test("autoRegister silently skips a full party", () => {
    const campaign = createCampaign("Полная партия");
    for (let i = 0; i < MAX_PARTY; i++) {
      store.addMember(campaign.id, "u-dm", { userId: `u-player-${i}` });
    }
    const before = store.getCampaign(campaign.id)!.members.length;
    store.autoRegister(campaign.id, { userId: "u-late" });
    assert.equal(store.getCampaign(campaign.id)!.members.length, before);
    assert.ok(!store.getCampaign(campaign.id)!.members.some((m) => m.userId === "u-late"));
  });

  describe("characters", () => {
    let campaign: Campaign;
    let characterInput: NewCharacterInput;

    beforeEach(() => {
      campaign = createCampaign("Персонажи");
      store.addMember(campaign.id, "u-dm", { userId: "u-player" });
      characterInput = {
        name: "Ария",
        characterClass: "плут",
        race: "эльфийка",
        level: 3,
        stats: { dex: 18, cha: 12 },
        background: "Бывшая воровка из гильдии.",
      };
    });

    test("saveCharacter stores the sheet and persists on disk", () => {
      const sheet = store.saveCharacter(campaign.id, "u-player", characterInput);
      assert.equal(sheet.campaignId, campaign.id);
      assert.equal(sheet.ownerUserId, "u-player");
      assert.equal(sheet.slug, "ariya");
      assert.equal(sheet.level, 3);
      assert.deepEqual(sheet.stats, { dex: 18, cha: 12 });
      const reloaded = store.listCharacters(campaign.id)[0];
      assert.equal(reloaded.name, "Ария");
      assert.equal(reloaded.background, "Бывшая воровка из гильдии.");
    });

    test("saveCharacter rejects non-members", () => {
      assert.throws(
        () => store.saveCharacter(campaign.id, "u-stranger", characterInput),
        (err: StoreError) => err.code === "access_denied",
      );
    });

    test("saveCharacter rejects duplicate names case-insensitively", () => {
      store.saveCharacter(campaign.id, "u-player", characterInput);
      assert.throws(
        () => store.saveCharacter(campaign.id, "u-player", { ...characterInput, name: "ария" }),
        (err: StoreError) => err.code === "duplicate",
      );
    });

    test("saveCharacter enforces party size", () => {
      for (let i = 0; i < MAX_PARTY; i++) {
        store.saveCharacter(campaign.id, "u-player", { ...characterInput, name: `Герой ${i}` });
      }
      assert.throws(
        () => store.saveCharacter(campaign.id, "u-player", { ...characterInput, name: "Лишний" }),
        (err: StoreError) => err.code === "party_full",
      );
    });

    test("updateCharacter patches state by name, slug or id", () => {
      const sheet = store.saveCharacter(campaign.id, "u-player", characterInput);
      const byName = store.updateCharacter(campaign.id, "Ария", { hp: 10, gold: 50 });
      assert.equal(byName.hp, 10);
      assert.equal(byName.gold, 50);
      const bySlug = store.updateCharacter(campaign.id, "ariya", { hp: 7, conditions: ["poisoned"] });
      assert.equal(bySlug.hp, 7);
      assert.deepEqual(bySlug.conditions, ["poisoned"]);
      const byId = store.updateCharacter(campaign.id, sheet.id, { level: 4, inventory: ["кинжал"] });
      assert.equal(byId.level, 4);
      assert.deepEqual(byId.inventory, ["кинжал"]);
    });

    test("updateCharacter reports unknown characters", () => {
      assert.throws(
        () => store.updateCharacter(campaign.id, "Незнакомец", { hp: 1 }),
        (err: StoreError) => err.code === "not_found",
      );
    });

    test("listCharacters orders by creation", () => {
      store.saveCharacter(campaign.id, "u-player", characterInput);
      store.saveCharacter(campaign.id, "u-player", { ...characterInput, name: "Борин" });
      const names = store.listCharacters(campaign.id).map((c) => c.name);
      assert.deepEqual(names, ["Ария", "Борин"]);
    });
  });
});
