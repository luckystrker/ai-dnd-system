// Package telegram — Telegram-канал бота: Bot API на stdlib, durable-очередь
// апдейтов в SQLite, склейка сообщений чата, дешёвый гейт «отвечать ли» и
// форматирование ответов DM.
package telegram

import (
	"regexp"
	"strconv"
	"strings"
)

// MessageLimit — максимальная длина одного сообщения Telegram в UTF-16
// code units (так его считает Bot API).
const MessageLimit = 4096

// placeholderToken — маркер спрятанного фрагмента (кода), который не должен
// попасть под markdown-разметку. Управляющий \x00 в тексте модели не
// встречается.
func placeholderToken(index int) string {
	return "\x00" + strconv.Itoa(index) + "\x00"
}

var (
	codeBlockPattern   = regexp.MustCompile("(?s)```[A-Za-z0-9_-]*\\n(.*?)```")
	inlineCodePattern  = regexp.MustCompile("`([^`\\n]+)`")
	boldStarPattern    = regexp.MustCompile(`\*\*([^\x00*\n]+)\*\*`)
	boldUnderPattern   = regexp.MustCompile(`__([^\x00_\n]+)__`)
	italicStarPattern  = regexp.MustCompile(`\*([^\x00*\n]+)\*`)
	italicUnderPattern = regexp.MustCompile(`_([^\x00_\n]+)_`)
	strikePattern      = regexp.MustCompile(`~~([^\x00~\n]+)~~`)
	linkPattern        = regexp.MustCompile(`\[([^\x00\]\n]+)\]\(([^\x00)\s]+)\)`)

	stripCodeBlock   = regexp.MustCompile("(?s)```[A-Za-z0-9_-]*\\n(.*?)```")
	stripInlineCode  = regexp.MustCompile("`([^`\\n]+)`")
	stripBoldStar    = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	stripBoldUnder   = regexp.MustCompile(`__([^_\n]+)__`)
	stripItalicStar  = regexp.MustCompile(`\*([^*\n]+)\*`)
	stripItalicUnder = regexp.MustCompile(`_([^_\n]+)_`)
	stripStrike      = regexp.MustCompile(`~~([^~\n]+)~~`)
	stripLink        = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)
	stripSymbols     = regexp.MustCompile("[*_~`]")
)

// MarkdownToTelegramHTML переводит Markdown модели в безопасное подмножество
// HTML для parse_mode=HTML: **жирный**, __жирный__, *курсив*, _курсив_,
// ~~зачёркнутый~~, `код`, ```блоки``` и [ссылки](url). Всё остальное
// экранируется, непарные маркеры остаются текстом, теги не оборачивают код
// (Telegram запрещает вложенные сущности).
//
// Порт agent/lib/telegram-format.ts 1:1.
func MarkdownToTelegramHTML(text string) string {
	placeholders := make([]string, 0, 4)
	stash := func(html string) string {
		token := placeholderToken(len(placeholders))
		placeholders = append(placeholders, html)
		return token
	}

	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)

	// Блоки кода прячем от последующей обработки: код не обрамляется тегами.
	escaped = codeBlockPattern.ReplaceAllStringFunc(escaped, func(match string) string {
		code := codeBlockPattern.FindStringSubmatch(match)[1]
		return stash("<pre>" + code + "</pre>")
	})
	escaped = inlineCodePattern.ReplaceAllStringFunc(escaped, func(match string) string {
		code := inlineCodePattern.FindStringSubmatch(match)[1]
		return stash("<code>" + code + "</code>")
	})

	escaped = boldStarPattern.ReplaceAllString(escaped, "<b>$1</b>")
	escaped = boldUnderPattern.ReplaceAllString(escaped, "<b>$1</b>")
	escaped = italicStarPattern.ReplaceAllString(escaped, "<i>$1</i>")
	escaped = italicUnderPattern.ReplaceAllString(escaped, "<i>$1</i>")
	escaped = strikePattern.ReplaceAllString(escaped, "<s>$1</s>")
	escaped = linkPattern.ReplaceAllStringFunc(escaped, func(match string) string {
		groups := linkPattern.FindStringSubmatch(match)
		label, url := groups[1], groups[2]
		// < и > уже экранированы; чистим только то, что ломает атрибут href.
		href := strings.NewReplacer(`"`, "", "'", "").Replace(url)
		return `<a href="` + href + `">` + label + `</a>`
	})

	// Подстановка через strings.Replace: строковая замена не интерпретирует
	// $& / $` / $$ / $' в содержимом кода (в отличие от regexp.ReplaceAll).
	for index, html := range placeholders {
		escaped = strings.Replace(escaped, placeholderToken(index), html, 1)
	}
	return escaped
}

// StripMarkdown срезает разметку для фолбэка: когда Telegram отверг
// HTML-версию, уходит тот же текст без markdown-символов.
func StripMarkdown(text string) string {
	text = stripCodeBlock.ReplaceAllString(text, "$1")
	text = stripInlineCode.ReplaceAllString(text, "$1")
	text = stripBoldStar.ReplaceAllString(text, "$1")
	text = stripBoldUnder.ReplaceAllString(text, "$1")
	text = stripItalicStar.ReplaceAllString(text, "$1")
	text = stripItalicUnder.ReplaceAllString(text, "$1")
	text = stripStrike.ReplaceAllString(text, "$1")
	text = stripLink.ReplaceAllString(text, "$1")
	return stripSymbols.ReplaceAllString(text, "")
}

// SplitMessageText режет длинный ответ на куски не длиннее MessageLimit
// (в UTF-16 code units). Делит по пустой строке, затем по переносу, затем по
// пробелу во второй половине куска; суррогатную пару не разрывает. Склейка
// кусков даёт исходный текст без потерь.
//
// Расхождение с eve 0.29.5 (api.js, splitTelegramMessageText), зафиксированное
// ревью слайса 6: eve режет по последнему «\n» (не «\n\n») не дальше 4096,
// затем по пробелу, тримит границы кусков (теряя переводы строк и пробелы) и
// может разрезать суррогатную пару. Здесь граница — последний перенос/пробел
// во второй половине, куски не тримятся, склейка ровно равна исходнику, а пара
// не рвётся. Векторы ниже сверены с eve 0.29.5: на границе 4096, длинном
// слове и эмодзи куски совпадают; на абзацном разрыве Go сохраняет «\n\n»,
// а eve их съедает.
func SplitMessageText(text string) []string {
	if text == "" {
		return nil
	}
	if utf16Len(text) <= MessageLimit {
		return []string{text}
	}
	runes := []rune(text)
	chunks := make([]string, 0, utf16Len(text)/MessageLimit+1)
	start := 0
	for start < len(runes) {
		width := 0
		cut := start
		// lastGood — резервная точка разреза (перенос/пробел) во второй части.
		lastGood := -1
		for cut < len(runes) {
			runeWidth := utf16RuneLen(runes[cut])
			if width+runeWidth > MessageLimit {
				break
			}
			width += runeWidth
			cut++
			if cut-start >= MessageLimit/2 {
				switch runes[cut-1] {
				case '\n', ' ':
					lastGood = cut
				}
			}
		}
		if cut == len(runes) {
			chunks = append(chunks, string(runes[start:cut]))
			break
		}
		// Предпочитаем перенос строки, затем пробел, но не отдаём слишком
		// короткий кусок: иначе частые переносы дадут лавину сообщений.
		if lastGood > start {
			cut = lastGood
		}
		chunks = append(chunks, string(runes[start:cut]))
		start = cut
	}
	return chunks
}

func utf16Len(text string) int {
	total := 0
	for _, r := range text {
		total += utf16RuneLen(r)
	}
	return total
}

func utf16RuneLen(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}
