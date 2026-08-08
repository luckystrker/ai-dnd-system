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

  test("sweeps keys inactive longer than the window", () => {
    const limiter = new SlidingWindowLimiter();
    limiter.allow("stale", 3, 60_000, 1_000);
    // Активный ключ трогаем поздно — он должен остаться.
    limiter.allow("fresh", 3, 60_000, 30_000);
    // Прошло больше окна с момента последнего обращения к stale.
    limiter.sweep(62_000, 60_000);
    assert.equal(limiter.size(), 1);
  });

  test("does not grow unbounded with many unique expired keys", () => {
    const limiter = new SlidingWindowLimiter();
    // Каждый ключ живёт только в своём окне; за его пределами должен вычищаться.
    for (let i = 0; i < 5000; i++) {
      limiter.allow(`key-${i}`, 3, 1_000, i * 2);
    }
    // cleanup внутри allow сработал по порогу > 1000; финальная подчистка.
    limiter.sweep(20_000, 1_000);
    assert.ok(limiter.size() < 5000, `expected map to shrink, got ${limiter.size()}`);
  });

  test("keeps keys that are still active within the window", () => {
    const limiter = new SlidingWindowLimiter();
    limiter.allow("k", 3, 60_000, 0);
    limiter.sweep(1_000, 60_000);
    assert.equal(limiter.size(), 1);
  });
});
