import { test, describe } from "node:test";
import assert from "node:assert/strict";

import {
  resolveCallerIdentity,
  resolveToolCallerIdentity,
  type ToolSessionContext,
} from "../agent/lib/campaigns/session.ts";

const telegramAuth = (overrides: Record<string, unknown> = {}) => ({
  principalId: "telegram:100200300",
  attributes: {
    user_id: "42",
    chat_id: "-1001234567890",
    chat_type: "supergroup",
    username: "gandalf",
    ...overrides,
  },
});

describe("resolveCallerIdentity", () => {
  test("extracts identity from telegram auth", () => {
    const identity = resolveCallerIdentity(telegramAuth({ message_thread_id: "7" }))!;
    assert.equal(identity.userId, "42");
    assert.equal(identity.username, "gandalf");
    assert.equal(identity.chatId, "-1001234567890");
    assert.equal(identity.chatType, "supergroup");
    assert.equal(identity.messageThreadId, 7);
  });

  test("returns undefined without user_id", () => {
    assert.equal(resolveCallerIdentity(telegramAuth({ user_id: undefined })), undefined);
    assert.equal(resolveCallerIdentity({ principalId: "telegram:1", attributes: {} }), undefined);
  });

  test("returns undefined for malformed auth", () => {
    assert.equal(resolveCallerIdentity(undefined), undefined);
    assert.equal(resolveCallerIdentity(null), undefined);
    assert.equal(resolveCallerIdentity("string"), undefined);
    assert.equal(resolveCallerIdentity(42), undefined);
  });

  test("omits invalid message_thread_id", () => {
    const identity = resolveCallerIdentity(telegramAuth({ message_thread_id: "not-a-number" }))!;
    assert.equal(identity.messageThreadId, undefined);
  });

  test("omits optional fields when absent", () => {
    const identity = resolveCallerIdentity({ attributes: { user_id: "1" } })!;
    assert.deepEqual(identity, {
      userId: "1",
      username: undefined,
      chatId: undefined,
      chatType: undefined,
      messageThreadId: undefined,
    });
  });

  test("reads array-typed attributes via the first element", () => {
    const identity = resolveCallerIdentity({
      attributes: { user_id: ["99"], chat_id: ["-1"], username: ["gimli"] },
    })!;
    assert.equal(identity.userId, "99");
    assert.equal(identity.chatId, "-1");
    assert.equal(identity.username, "gimli");
  });
});

describe("resolveToolCallerIdentity", () => {
  const makeCtx = (auth: unknown): ToolSessionContext => ({
    session: {
      auth: { current: auth as never, initiator: null },
    },
  });

  test("uses current auth when present", () => {
    const identity = resolveToolCallerIdentity(makeCtx(telegramAuth()))!;
    assert.equal(identity.userId, "42");
  });

  test("falls back to initiator when current is null", () => {
    const ctx: ToolSessionContext = {
      session: {
        auth: { current: null, initiator: telegramAuth() as never },
      },
    };
    const identity = resolveToolCallerIdentity(ctx)!;
    assert.equal(identity.userId, "42");
  });

  test("returns undefined without context or auth", () => {
    assert.equal(resolveToolCallerIdentity(undefined), undefined);
    const empty: ToolSessionContext = {
      session: { auth: { current: null, initiator: null } },
    };
    assert.equal(resolveToolCallerIdentity(empty), undefined);
  });
});
