package telegram

import (
	"strings"
	"testing"
)

func TestMarkdownToTelegramHTML(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold stars", "Ты видишь **дракона** вдалеке", "Ты видишь <b>дракона</b> вдалеке"},
		{"bold underscores", "Внимание: __опасность__", "Внимание: <b>опасность</b>"},
		{"italic star", "тихо *шепчет*", "тихо <i>шепчет</i>"},
		{"italic underscore", "тихо _шепчет_", "тихо <i>шепчет</i>"},
		{"escapes html", "x < 10 и a & b", "x &lt; 10 и a &amp; b"},
		{"stray asterisk", "5 * 3 = 15", "5 * 3 = 15"},
		{"inline code", "команда `roll_dice`", "команда <code>roll_dice</code>"},
		{"code block untouched", "```python\nx = **2**\n```", "<pre>x = **2**\n</pre>"},
		{"dollar in code", "```bash\necho $$PID\n```", "<pre>echo $$PID\n</pre>"},
		{"dollar ampersand", "```js\nconst x = \"$&\";\n```", "<pre>const x = \"$&amp;\";\n</pre>"},
		{"no nesting bold code", "**`inline`**", "**<code>inline</code>**"},
		{"no nesting strike code", "~~`code`~~", "~~<code>code</code>~~"},
		{"no nesting link code", "[метка `x`](https://example.com)", "[метка <code>x</code>](https://example.com)"},
		{"no nesting italic code", "*`x`*", "*<code>x</code>*"},
		{"link", "[сайт](https://example.com)", `<a href="https://example.com">сайт</a>`},
		{"strikethrough", "~~старое~~", "<s>старое</s>"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := MarkdownToTelegramHTML(tc.in); got != tc.want {
				t.Fatalf("MarkdownToTelegramHTML(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

// Незакрытые конструкции не превращаются в битые теги: Telegram отверг бы
// сообщение с ошибкой парсинга сущностей.
func TestMarkdownToTelegramHTMLProducesClosedTags(t *testing.T) {
	t.Parallel()

	out := MarkdownToTelegramHTML("Незакрытый **жирный и `код")
	if strings.Contains(out, "<b>") || strings.Contains(out, "<code>") {
		t.Fatalf("незакрытая разметка стала тегом: %q", out)
	}
	if strings.Contains(out, "<") && !strings.Contains(out, "&lt;") {
		t.Fatalf("сырой '<' в выводе: %q", out)
	}
}

func TestStripMarkdown(t *testing.T) {
	t.Parallel()

	cases := []struct{ in, want string }{
		{"**жирный** и `код` и ~~старое~~", "жирный и код и старое"},
		{"[сайт](https://example.com)", "сайт"},
		{"```js\na * b\n```", "a  b\n"},
	}
	for _, tc := range cases {
		if got := StripMarkdown(tc.in); got != tc.want {
			t.Fatalf("StripMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitMessageText(t *testing.T) {
	t.Parallel()

	if chunks := SplitMessageText(""); len(chunks) != 0 {
		t.Fatalf("пустой текст: %v", chunks)
	}
	short := "короткое сообщение"
	if chunks := SplitMessageText(short); len(chunks) != 1 || chunks[0] != short {
		t.Fatalf("короткий текст: %v", chunks)
	}

	long := strings.Repeat("а", MessageLimit*2+100)
	chunks := SplitMessageText(long)
	if len(chunks) != 3 {
		t.Fatalf("кусков = %d, want 3", len(chunks))
	}
	for index, chunk := range chunks {
		if utf16Len(chunk) > MessageLimit {
			t.Fatalf("кусок %d длиннее лимита: %d", index, utf16Len(chunk))
		}
	}
	if joined := strings.Join(chunks, ""); joined != long {
		t.Fatalf("текст потерялся при разбивке: %d != %d", len(joined), len(long))
	}

	// Предпочитаем перенос строки, а не разрез посреди слова.
	paragraphs := strings.Repeat("слово ", 700)
	chunks = SplitMessageText(paragraphs)
	if len(chunks) < 2 {
		t.Fatalf("ожидалось несколько кусков: %d", len(chunks))
	}
	for _, chunk := range chunks {
		if utf16Len(chunk) > MessageLimit {
			t.Fatalf("кусок длиннее лимита: %d", utf16Len(chunk))
		}
	}
	if joined := strings.Join(chunks, ""); joined != paragraphs {
		t.Fatal("текст изменился при разбивке")
	}

	// Суррогатная пара не должна разрезаться между кусками.
	emoji := strings.Repeat("🙂", MessageLimit)
	for _, chunk := range SplitMessageText(emoji) {
		if utf16Len(chunk) > MessageLimit {
			t.Fatalf("эмодзи-кусок длиннее лимита: %d", utf16Len(chunk))
		}
		if !strings.HasPrefix(chunk, "🙂") && chunk != "" {
			t.Fatalf("кусок начинается с половины суррогатной пары: %q", chunk)
		}
	}
}
