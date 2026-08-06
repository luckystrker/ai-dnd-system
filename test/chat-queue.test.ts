import { test, describe } from "node:test";
import assert from "node:assert/strict";

import { PerChatQueue } from "../agent/lib/chat-queue.ts";

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe("PerChatQueue", () => {
  test("runs tasks of the same chat strictly in order", async () => {
    const queue = new PerChatQueue();
    const order: string[] = [];
    const first = queue.enqueue("a", async () => {
      order.push("first-start");
      await sleep(20);
      order.push("first-end");
    });
    const second = queue.enqueue("a", async () => {
      order.push("second");
    });
    await Promise.all([first, second]);
    assert.deepEqual(order, ["first-start", "first-end", "second"]);
  });

  test("different chats run concurrently", async () => {
    const queue = new PerChatQueue();
    let overlap = false;
    const first = queue.enqueue("a", async () => {
      await sleep(20);
      overlap = true;
    });
    const second = queue.enqueue("b", async () => {
      overlap = false;
    });
    await Promise.all([first, second]);
    assert.equal(overlap, true);
  });

  test("a failing task does not block the next one", async () => {
    const queue = new PerChatQueue();
    let ran = false;
    const failing = queue.enqueue("a", async () => {
      throw new Error("boom");
    });
    const next = queue.enqueue("a", async () => {
      ran = true;
    });
    await assert.rejects(failing, /boom/);
    await next;
    assert.equal(ran, true);
  });

  test("chat key is released after the last task settles", async () => {
    const queue = new PerChatQueue();
    await queue.enqueue("a", async () => {});
    await sleep(0);
    assert.equal(queue.size, 0);
  });
});
