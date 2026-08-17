import { test, describe, after, beforeEach } from "node:test";
import assert from "node:assert/strict";

import { openCampaignDb } from "../agent/lib/campaigns/sqlite-db.ts";
import { tempDb } from "./helpers.ts";

const { cleanup } = tempDb("access");
after(() => {
  // Закрываем соединение дефолтного стора, чтобы temp-папка удалилась
  // (Windows не даёт удалить файл с открытым соединением).
  (campaignStore as { close?: () => void }).close?.();
});
after(cleanup);

const { campaignStore } = await import("../agent/lib/campaigns/store.ts");
const {
  canActForCharacter,
  findCampaignForIdentity,
  requireIdentity,
  resolveCampaign,
  resolveCampaignForWrite,
} = await import("../agent/lib/campaigns/access.ts");
import { StoreError, type Campaign } from "../agent/lib/campaigns/types.ts";

const dmAuth = (overrides: Record<string, unknown> = {}) => ({
  attributes: {
    user_id: "u-dm",
    chat_id: "-100x",
    chat_type: "supergroup",
    ...overrides,
  },
});
const playerAuth = () => ({ attributes: { user_id: "u-player", chat_id: "-100x" } });

const CHAR_CREATION = {
  name: "Ария",
  characterClass: "плут",
  race: "эльфийка",
  stats: { dex: 18 },
};

let campaign: Campaign;

beforeEach(() => {
  // Первый вызов стора создаёт схему (конструктор выполняет DDL), затем
  // чистим кампании — каждый тест начинает с пустой базы.
  campaignStore.listCampaigns();
  openCampaignDb().exec("DELETE FROM campaigns;");
  campaign = campaignStore.createCampaign(
    { title: "Тайная крепость", length: "short", setting: "Горы", theme: "осада" },
    { userId: "u-dm" },
  );
  campaignStore.bindAndActivate(campaign.id, "u-dm", { chatId: "-100x" });
  campaignStore.addMember(campaign.id, "u-dm", { userId: "u-player" });
  campaignStore.saveCharacter(campaign.id, "u-player", CHAR_CREATION);
});

describe("resolveCampaign", () => {
  test("resolves by explicit id or slug", () => {
    assert.equal(resolveCampaign(dmAuth(), campaign.id)?.id, campaign.id);
    assert.equal(resolveCampaign(dmAuth(), campaign.slug)?.id, campaign.id);
    assert.equal(resolveCampaign(dmAuth(), "missing"), undefined);
  });

  test("resolves by bound chat from auth", () => {
    assert.equal(resolveCampaign(dmAuth())?.id, campaign.id);
  });

  test("returns undefined without chat identity", () => {
    assert.equal(resolveCampaign({ attributes: { user_id: "u-x" } }), undefined);
  });

  test("member can read by explicit slug", () => {
    assert.equal(resolveCampaign(playerAuth(), campaign.slug)?.id, campaign.id);
  });

  test("stranger is denied read by explicit slug", () => {
    const stranger = { attributes: { user_id: "u-stranger", chat_id: "-777" } };
    assert.throws(
      () => resolveCampaign(stranger, campaign.slug),
      (err: StoreError) => err.code === "access_denied",
    );
  });

  test("automation without identity can read by explicit slug", () => {
    assert.equal(resolveCampaign({}, campaign.slug)?.id, campaign.id);
  });
});

describe("findCampaignForIdentity", () => {
  test("matches exact chat without thread", () => {
    const found = findCampaignForIdentity({ userId: "u-dm", chatId: "-100x" });
    assert.equal(found?.id, campaign.id);
  });

  test("falls back from thread-scoped to non-threaded chat campaign", () => {
    const found = findCampaignForIdentity({ userId: "u-dm", chatId: "-100x", messageThreadId: 42 });
    assert.equal(found?.id, campaign.id);
  });

  test("returns undefined for unknown chats", () => {
    assert.equal(findCampaignForIdentity({ userId: "u-dm", chatId: "-999" }), undefined);
  });
});

describe("resolveCampaignForWrite", () => {
  test("explicit slug bypasses role checks (trusted automation)", () => {
    const resolved = resolveCampaignForWrite({}, campaign.slug);
    assert.equal(resolved.id, campaign.id);
  });

  test("throws not_found for unknown explicit slug", () => {
    assert.throws(
      () => resolveCampaignForWrite({}, "nope"),
      (err: StoreError) => err.code === "not_found",
    );
  });

  test("dm can write via auth", () => {
    assert.equal(resolveCampaignForWrite(dmAuth()).id, campaign.id);
  });

  test("player is denied write access", () => {
    assert.throws(
      () => resolveCampaignForWrite(playerAuth()),
      (err: StoreError) => err.code === "access_denied",
    );
  });

  test("dm can write by explicit slug", () => {
    assert.equal(resolveCampaignForWrite(dmAuth(), campaign.slug).id, campaign.id);
  });

  test("player is denied write by explicit slug (IDOR)", () => {
    assert.throws(
      () => resolveCampaignForWrite(playerAuth(), campaign.slug),
      (err: StoreError) => err.code === "access_denied",
    );
  });

  test("stranger is denied write by explicit slug (IDOR)", () => {
    const stranger = { attributes: { user_id: "u-stranger", chat_id: "-777" } };
    assert.throws(
      () => resolveCampaignForWrite(stranger, campaign.slug),
      (err: StoreError) => err.code === "access_denied",
    );
  });

  test("throws without identity", () => {
    assert.throws(
      () => resolveCampaignForWrite({}),
      (err: StoreError) => err.code === "access_denied",
    );
  });
});

describe("requireIdentity", () => {
  test("returns identity from auth", () => {
    assert.equal(requireIdentity(dmAuth()).userId, "u-dm");
  });

  test("throws access_denied without identity", () => {
    assert.throws(
      () => requireIdentity({}),
      (err: StoreError) => err.code === "access_denied",
    );
  });
});

describe("canActForCharacter", () => {
  const ctxOf = (auth: unknown) => ({
    session: { auth: { current: auth as never, initiator: null } },
  });

  test("automation without context is always allowed", () => {
    assert.deepEqual(canActForCharacter(undefined, "Ария"), { allowed: true });
  });

  test("owner can act for their character", () => {
    assert.deepEqual(canActForCharacter(ctxOf(playerAuth()), "Ария"), { allowed: true });
  });

  test("dm can act for any character", () => {
    assert.deepEqual(canActForCharacter(ctxOf(dmAuth()), "Ария"), { allowed: true });
  });

  test("stranger cannot act for another player's character", () => {
    const stranger = ctxOf({ attributes: { user_id: "u-stranger", chat_id: "-100x" } });
    const result = canActForCharacter(stranger, "Ария");
    assert.equal(result.allowed, false);
    assert.match(result.reason ?? "", /принадлежит другому игроку/);
  });

  test("unknown characters are allowed (no sheet to protect)", () => {
    assert.deepEqual(canActForCharacter(ctxOf(playerAuth()), "Кто-то новый"), { allowed: true });
  });

  test("identity without a bound campaign is allowed", () => {
    const elsewhere = ctxOf({ attributes: { user_id: "u-player", chat_id: "-777" } });
    assert.deepEqual(canActForCharacter(elsewhere, "Ария"), { allowed: true });
  });
});
