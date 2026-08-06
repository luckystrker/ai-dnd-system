import { test, describe } from "node:test";
import assert from "node:assert/strict";

import { SlidingWindowLimiter } from "../agent/lib/rate-limit.ts";

describe("SlidingWindowLimiter", () => {
  test("allows requests up to the limit", () => {
    const limiter = new SlidingWindowLimiter();
    for (let i = 0; i < 3; i++) {
      assert.equal(limiter.allow("chat-1", 3, 60_000, 1_000), true);
    }
  });

  test("blocks once the window is full", () => {
    const limiter = new SlidingWindowLimiter();
    for (let i = 0; i < 3; i++) limiter.allow("k", 3, 60_000, 1_000);
    assert.equal(limiter.allow("k", 3, 60_000, 1_000), false);
    assert.equal(limiter.allow("k", 3, 60_000, 1_500), false);
  });

  test("frees capacity after the window slides", () => {
    const limiter = new SlidingWindowLimiter();
    for (let i = 0; i < 3; i++) limiter.allow("k", 3, 60_000, 1_000);
    assert.equal(limiter.allow("k", 3, 60_000, 61_001), true);
  });

  test("expired entries are dropped from the count", () => {
    const limiter = new SlidingWindowLimiter();
    limiter.allow("k", 3, 60_000, 0);
    limiter.allow("k", 3, 60_000, 10_000);
    limiter.allow("k", 3, 60_000, 20_000);
    assert.equal(limiter.allow("k", 3, 60_000, 60_001), true);
  });

  test("keys are isolated", () => {
    const limiter = new SlidingWindowLimiter();
    limiter.allow("a", 1, 60_000, 0);
    assert.equal(limiter.allow("a", 1, 60_000, 1), false);
    assert.equal(limiter.allow("b", 1, 60_000, 1), true);
  });

  test("window boundary is exclusive", () => {
    const limiter = new SlidingWindowLimiter();
    limiter.allow("k", 1, 1_000, 1_000);
    assert.equal(limiter.allow("k", 1, 1_000, 2_000), true);
  });
});
