import { test, describe } from "node:test";
import assert from "node:assert/strict";

import {
  buildDocument,
  parseFrontmatter,
  serializeFrontmatter,
  splitFrontmatter,
} from "../agent/lib/campaigns/frontmatter.ts";

describe("serializeFrontmatter", () => {
  test("emits scalars: strings quoted, numbers and booleans bare", () => {
    const out = serializeFrontmatter({ title: "Hello", day: 3, active: true });
    assert.deepEqual(out.split("\n"), [
      'title: "Hello"',
      "day: 3",
      "active: true",
    ]);
  });

  test("skips undefined and null values", () => {
    const out = serializeFrontmatter({ a: undefined, b: null, c: "x" });
    assert.equal(out, 'c: "x"');
  });

  test("emits nested objects with indentation", () => {
    const out = serializeFrontmatter({ boundChat: { chatId: "-100", thread: 7 } });
    assert.deepEqual(out.split("\n"), [
      "boundChat:",
      "  chatId: \"-100\"",
      "  thread: 7",
    ]);
  });

  test("emits arrays of scalars and arrays of objects", () => {
    const out = serializeFrontmatter({
      tags: ["a", "b"],
      empty: [],
      members: [{ userId: "1", role: "dm" }],
    });
    assert.deepEqual(out.split("\n"), [
      'tags:',
      '  - "a"',
      '  - "b"',
      "empty: []",
      "members:",
      "  -",
      "    userId: \"1\"",
      "    role: \"dm\"",
    ]);
  });
});

describe("parseFrontmatter", () => {
  test("parses hand-written scalar YAML", () => {
    const parsed = parseFrontmatter('title: "Поход"\nday: 12\nactive: true\nnone: null');
    assert.deepEqual(parsed, { title: "Поход", day: 12, active: true, none: null });
  });

  test("parses nested objects by indentation", () => {
    const parsed = parseFrontmatter("outer:\n  inner: 5\n  list:\n    - 1\n    - 2");
    assert.deepEqual(parsed, { outer: { inner: 5, list: [1, 2] } });
  });

  test("parses arrays with inline objects", () => {
    const parsed = parseFrontmatter("members:\n  - name: \"Ann\"\n    role: dm\n  - name: \"Bob\"\n");
    assert.deepEqual(parsed, {
      members: [
        { name: "Ann", role: "dm" },
        { name: "Bob" },
      ],
    });
  });

  test("parses inline map and empty list literals", () => {
    const parsed = parseFrontmatter("a: {}\nb: []");
    assert.deepEqual(parsed, { a: {}, b: [] });
  });

  test("ignores comments and blank lines", () => {
    const parsed = parseFrontmatter("# comment\n\nkey: 1\n");
    assert.deepEqual(parsed, { key: 1 });
  });

  test("returns empty object for empty input", () => {
    assert.deepEqual(parseFrontmatter(""), {});
  });
});

describe("buildDocument / splitFrontmatter", () => {
  test("round-trips data and body", () => {
    const data = {
      id: "abc",
      title: "Приключение",
      members: [{ userId: "u1", role: "dm" }],
    };
    const doc = buildDocument(data, "Описание кампании.\n\nВторая строка.");
    const { data: parsed, body } = splitFrontmatter(doc);
    assert.deepEqual(parsed, data);
    assert.equal(body, "Описание кампании.\n\nВторая строка.");
  });

  test("serialize -> parse preserves nested structures", () => {
    const data = {
      boundChat: { chatId: "-100x", messageThreadId: 42 },
      stats: { str: 15, dex: 8 },
      conditions: ["poisoned", "frightened"],
      emptyList: [],
      flag: false,
    };
    assert.deepEqual(parseFrontmatter(serializeFrontmatter(data)), data);
  });

  test("doc without frontmatter yields empty data and trimmed body", () => {
    const { data, body } = splitFrontmatter("  just text\n");
    assert.deepEqual(data, {});
    assert.equal(body, "just text");
  });

  test("doc with unterminated frontmatter is treated as plain body", () => {
    const { data, body } = splitFrontmatter("---\nkey: 1\n");
    assert.deepEqual(data, {});
    assert.equal(body, "---\nkey: 1");
  });

  test("body is always terminated with a newline", () => {
    const doc = buildDocument({ key: 1 }, "text");
    assert.ok(doc.endsWith("text\n"));
  });
});
