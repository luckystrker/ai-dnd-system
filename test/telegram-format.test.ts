import { test, describe } from "node:test";
import assert from "node:assert/strict";

import { markdownToTelegramHtml, stripMarkdown } from "../agent/lib/telegram-format.ts";

describe("markdownToTelegramHtml", () => {
  test("bolds **...**", () => {
    assert.equal(markdownToTelegramHtml("Ты видишь **дракона** вдалеке"), "Ты видишь <b>дракона</b> вдалеке");
  });

  test("bolds __...__", () => {
    assert.equal(markdownToTelegramHtml("Внимание: __опасность__"), "Внимание: <b>опасность</b>");
  });

  test("italicizes *...* and _..._", () => {
    assert.equal(markdownToTelegramHtml("тихо *шепчет*"), "тихо <i>шепчет</i>");
    assert.equal(markdownToTelegramHtml("тихо _шепчет_"), "тихо <i>шепчет</i>");
  });

  test("escapes html metacharacters in plain text", () => {
    assert.equal(markdownToTelegramHtml("x < 10 и a & b"), "x &lt; 10 и a &amp; b");
  });

  test("leaves stray asterisks as literal text", () => {
    assert.equal(markdownToTelegramHtml("5 * 3 = 15"), "5 * 3 = 15");
  });

  test("converts inline code", () => {
    assert.equal(markdownToTelegramHtml("команда `roll_dice`"), "команда <code>roll_dice</code>");
  });

  test("does not reformat content inside code blocks", () => {
    assert.equal(markdownToTelegramHtml("```python\nx = **2**\n```"), "<pre>x = **2**\n</pre>");
  });

  test("keeps $ pattern chars in code blocks verbatim", () => {
    assert.equal(markdownToTelegramHtml("```bash\necho $$PID\n```"), "<pre>echo $$PID\n</pre>");
    assert.equal(markdownToTelegramHtml("```js\nconst x = \"$&\";\n```"), '<pre>const x = "$&amp;";\n</pre>');
  });

  test("never nests formatting tags around code", () => {
    assert.equal(markdownToTelegramHtml("**`inline`**"), "**<code>inline</code>**");
    assert.equal(markdownToTelegramHtml("~~`code`~~"), "~~<code>code</code>~~");
    assert.equal(markdownToTelegramHtml("[метка `x`](https://example.com)"), "[метка <code>x</code>](https://example.com)");
    assert.equal(markdownToTelegramHtml("*`x`*"), "*<code>x</code>*");
  });

  test("converts links", () => {
    assert.equal(
      markdownToTelegramHtml("[сайт](https://example.com)"),
      '<a href="https://example.com">сайт</a>',
    );
  });

  test("converts strikethrough", () => {
    assert.equal(markdownToTelegramHtml("~~старое~~"), "<s>старое</s>");
  });

  test("produces only closed tags", () => {
    const out = markdownToTelegramHtml("Незакрытый **жирный и `код");
    assert.ok(!/<[^/]/.test(out.replace(/<\/(\w+)>/g, "")));
  });
});

describe("stripMarkdown", () => {
  test("removes markdown syntax", () => {
    assert.equal(stripMarkdown("**жирный** и `код` и ~~старое~~"), "жирный и код и старое");
  });

  test("keeps link label", () => {
    assert.equal(stripMarkdown("[сайт](https://example.com)"), "сайт");
  });
});
