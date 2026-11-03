package dm

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/middleware"
)

// QuoteAppend — одно срабатывание гарантии публичных бросков: какой тул и
// какие компоненты цитаты дописаны в ответ. Счётчик — продуктовая метрика
// (PLAN 40): по нему видно, как часто модель пропускает цитату сама, хотя
// игроки всё равно видят результат.
type QuoteAppend struct {
	Tool  string   `json:"tool"`
	Parts []string `json:"parts"`
}

// rollQuoteMW гарантирует публичность бросков и боевых эффектов: если модель
// вызвала skill_check или roll_dice, но не назвала d20/итог/DC, в конец
// ответа дописываются только недостающие компоненты. Для Dodge дописывается
// фиксированная фраза про помеху, без служебного хвоста про очередь ходов.
// Промпт требует это делать, но на длинных ходах модель пропускает.
//
// Результаты combat без броска (status/next/damage) и initiative не трогаем:
// в них нет d20/КД, а порядок ходов модель пересказывает сама.
type rollQuoteMW struct {
	middleware.Base
	mu       sync.Mutex
	results  []rollResult
	combats  []string
	dodges   []string
	log      *slog.Logger
	onAppend func(QuoteAppend)
}

// rollResult — результат броскового тула вместе с его именем: счётчику
// срабатываний нужен исходный тул, а не только текст.
type rollResult struct {
	tool string
	text string
}

func (m *rollQuoteMW) OnTurn(next middleware.TurnFunc, ev *events.ModelRequest, cx *agocontext.Context) (*events.ModelResponse, error) {
	m.mu.Lock()
	m.results = nil
	m.combats = nil
	m.dodges = nil
	m.mu.Unlock()

	response, err := next(ev, cx)
	if err != nil || response == nil {
		return response, err
	}
	m.mu.Lock()
	results := append([]rollResult(nil), m.results...)
	combats := append([]string(nil), m.combats...)
	dodges := append([]string(nil), m.dodges...)
	m.mu.Unlock()
	if (len(results) == 0 && len(combats) == 0 && len(dodges) == 0) || response.Message == nil || len(response.ToolCalls.Calls) > 0 {
		return response, nil
	}

	body := response.Message.Content
	suffixes := make([]string, 0, len(results)+1)
	appends := make([]QuoteAppend, 0, len(results)+1)
	for _, result := range results {
		if parts := missingRollParts(body, result.text); len(parts) > 0 {
			suffixes = append(suffixes, "("+strings.Join(parts, ", ")+")")
			appends = append(appends, QuoteAppend{Tool: result.tool, Parts: parts})
		}
	}
	for _, result := range combats {
		if parts := missingCombatParts(body, result); len(parts) > 0 {
			suffixes = append(suffixes, "("+strings.Join(parts, ", ")+")")
			appends = append(appends, QuoteAppend{Tool: "combat", Parts: parts})
		}
	}
	if len(dodges) > 0 && !dodgeQuoted(body) {
		suffixes = append(suffixes, dodgeSuffix)
		appends = append(appends, QuoteAppend{Tool: "combat", Parts: []string{"помеха"}})
	}
	if len(suffixes) == 0 {
		return response, nil
	}
	if strings.TrimSpace(body) != "" {
		body = strings.TrimRight(body, "\n") + " "
	}
	response.Message.Content = body + strings.Join(suffixes, " ")
	m.report(appends, cx)
	return response, nil
}

// report фиксирует срабатывания гарантии: строка в лог хода и колбэк для
// eval (PLAN 40). Лог не содержит текста ответа — только счётчик и компоненты.
func (m *rollQuoteMW) report(appends []QuoteAppend, cx *agocontext.Context) {
	if len(appends) == 0 {
		return
	}
	tools := map[string]int{}
	seen := map[string]bool{}
	parts := make([]string, 0, len(appends))
	for _, item := range appends {
		tools[item.Tool]++
		for _, part := range item.Parts {
			if !seen[part] {
				seen[part] = true
				parts = append(parts, part)
			}
		}
	}
	if m.log != nil {
		fields := []any{"appends", len(appends), "parts", parts}
		for tool, count := range tools {
			fields = append(fields, "tool_"+tool, count)
		}
		if session := sessionFrom(cx); session != nil && session.ChatID != "" {
			fields = append(fields, "chat", session.ChatID)
		}
		m.log.Info("dm: roll quote appended", fields...)
	}
	if m.onAppend != nil {
		for _, item := range appends {
			m.onAppend(item)
		}
	}
}

func (m *rollQuoteMW) OnToolExecution(next middleware.ToolExecFunc, ev *events.ToolCallEvent, cx *agocontext.Context) (events.Event, error) {
	result, err := next(ev, cx)
	if err != nil {
		return result, err
	}
	if ev.Name != "skill_check" && ev.Name != "roll_dice" && ev.Name != "combat" {
		return result, nil
	}
	if _, failed := events.AsValue(result).(events.ToolErrorEvent); failed {
		return result, nil
	}
	text := toolResultText(result)
	if text == "" {
		return result, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.Name == "combat" {
		if isDodgeCall(ev.Arguments) {
			m.dodges = append(m.dodges, text)
		} else {
			m.combats = append(m.combats, text)
		}
		return result, nil
	}
	m.results = append(m.results, rollResult{tool: ev.Name, text: text})
	return result, nil
}

func isDodgeCall(arguments string) bool {
	var args struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return false
	}
	return args.Action == "dodge"
}

const dodgeSuffix = "Атаки по этому персонажу до его следующего хода идут с помехой."

// dodgeQuoted — ответ назвал помеху от Уклонения. Достаточно слова
// «помеха»/«disadvantage»: цифр у Dodge нет.
func dodgeQuoted(answer string) bool {
	answer = strings.ToLower(strings.NewReplacer("*", "", "`", "").Replace(answer))
	return strings.Contains(answer, "помех") || strings.Contains(answer, "disadvantage")
}

var (
	reResultD20           = regexp.MustCompile(`d20\s*=\s*(\d+)`)
	reResultVsDC          = regexp.MustCompile(`=\s*(\d+)\s+vs\s+DC\s*(\d+)`)
	reResultTotal         = regexp.MustCompile(`Итого:\s*(\d+)`)
	reResultSkill         = regexp.MustCompile(`проверка «([^»]+)»`)
	reResultMod           = regexp.MustCompile(`d20\s*=\s*\d+\s*([+-]\d+)`)
	reResultOutcome       = regexp.MustCompile(`->\s*([А-Яа-яA-Za-z]+)`)
	reResultCombatAC      = regexp.MustCompile(`против КД\s*(\d+)`)
	reResultCombatOutcome = regexp.MustCompile(`→\s*(попадание|промах)`)

	reAnswerD20 = regexp.MustCompile(`(?i)(?:d20\s*[=:]?\s*(\d+)|выпало\s*(\d+)|(\d+)\s+на\s+d20)`)
	reAnswerSum = regexp.MustCompile(`(?i)(?:итог[а-яё]*|всего|сумма|total|выпало)\D{0,15}(\d+)|=\s*(\d+)|бонус[а-яё]*\s*(\d+)|(\d+)\s+против\s+(?:dc|кд)`)
	reAnswerDC  = regexp.MustCompile(`(?i)(?:dc|кд|сл|сложност[а-яё]*)\s*[=:]?\s*(\d+)`)
)

// missingRollParts возвращает только те компоненты броска, которых нет в
// ответе. Целиком строку тула не дописываем: иначе уже названные числа
// повторяются в ответе игроку.
func missingRollParts(answer, result string) []string {
	answer = strings.NewReplacer("*", "", "`", "").Replace(answer)
	var parts []string
	if match := reResultSkill.FindStringSubmatch(result); match != nil {
		if !strings.Contains(strings.ToLower(answer), strings.ToLower(match[1])) {
			parts = append(parts, "«"+match[1]+"»")
		}
	}
	if match := reResultD20.FindStringSubmatch(result); match != nil {
		if !containsNumber(answerNumbers(reAnswerD20, answer), match[1]) {
			parts = append(parts, "d20 = "+match[1])
		}
	}
	if match := reResultMod.FindStringSubmatch(result); match != nil {
		if !strings.Contains(answer, match[1]) {
			parts = append(parts, "мод "+match[1])
		}
	}
	if match := reResultVsDC.FindStringSubmatch(result); match != nil {
		if !containsNumber(answerNumbers(reAnswerSum, answer), match[1]) {
			parts = append(parts, "итого "+match[1])
		}
		if !containsNumber(answerNumbers(reAnswerDC, answer), match[2]) {
			parts = append(parts, "DC "+match[2])
		}
	}
	if match := reResultTotal.FindStringSubmatch(result); match != nil {
		if !containsNumber(answerNumbers(reAnswerSum, answer), match[1]) {
			parts = append(parts, "итого "+match[1])
		}
	}
	if match := reResultOutcome.FindStringSubmatch(result); match != nil {
		outcome := strings.ToLower(match[1])
		if !strings.Contains(strings.ToLower(answer), "успех") &&
			!strings.Contains(strings.ToLower(answer), "провал") {
			parts = append(parts, outcome)
		}
	}
	return parts
}

// missingCombatParts — то же для атаки combat: d20, КД цели и вердикт.
// Служебный хвост про очередь ходов не дописывается.
func missingCombatParts(answer, result string) []string {
	answer = strings.NewReplacer("*", "", "`", "").Replace(answer)
	var parts []string
	if match := reResultD20.FindStringSubmatch(result); match != nil {
		if !containsNumber(answerNumbers(reAnswerD20, answer), match[1]) {
			parts = append(parts, "d20 = "+match[1])
		}
	}
	if match := reResultCombatAC.FindStringSubmatch(result); match != nil {
		if !containsNumber(answerNumbers(reAnswerDC, answer), match[1]) {
			parts = append(parts, "против КД "+match[1])
		}
	}
	if match := reResultCombatOutcome.FindStringSubmatch(result); match != nil {
		if !strings.Contains(strings.ToLower(answer), match[1]) {
			parts = append(parts, match[1])
		}
	}
	return parts
}

func answerNumbers(pattern *regexp.Regexp, text string) []string {
	var out []string
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		for _, group := range match[1:] {
			if group != "" {
				out = append(out, group)
			}
		}
	}
	return out
}

func containsNumber(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
