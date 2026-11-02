package dm

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/middleware"
)

// rollQuoteMW гарантирует публичность бросков и боевых эффектов: если модель
// вызвала skill_check или roll_dice, но не назвала в ответе d20/итог/DC, или
// взяла Dodge и не объявила помеху, точный результат тула дописывается в
// конец ответа. Промпт требует это делать, но на длинных ходах модель
// пропускает; правило важнее стиля, а текст уже есть в результате тула.
//
// Остальные результаты combat и initiative не трогаем: это многострочный
// порядок ходов, и модель пересказывает его сама.
type rollQuoteMW struct {
	middleware.Base
	mu      sync.Mutex
	results []string
	dodges  []string
}

func (m *rollQuoteMW) OnTurn(next middleware.TurnFunc, ev *events.ModelRequest, cx *agocontext.Context) (*events.ModelResponse, error) {
	m.mu.Lock()
	m.results = nil
	m.dodges = nil
	m.mu.Unlock()

	response, err := next(ev, cx)
	if err != nil || response == nil {
		return response, err
	}
	m.mu.Lock()
	results := append([]string(nil), m.results...)
	dodges := append([]string(nil), m.dodges...)
	m.mu.Unlock()
	if (len(results) == 0 && len(dodges) == 0) || response.Message == nil || len(response.ToolCalls.Calls) > 0 {
		return response, nil
	}

	body := response.Message.Content
	missing := make([]string, 0, len(results)+len(dodges))
	for _, result := range results {
		if !rollResultQuoted(body, result) {
			missing = append(missing, result)
		}
	}
	for _, result := range dodges {
		if !dodgeQuoted(body) {
			missing = append(missing, result)
		}
	}
	if len(missing) == 0 {
		return response, nil
	}
	if strings.TrimSpace(body) != "" {
		body = strings.TrimRight(body, "\n") + "\n\n"
	}
	response.Message.Content = body + strings.Join(missing, "\n")
	return response, nil
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
		}
		return result, nil
	}
	m.results = append(m.results, text)
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

// dodgeQuoted — ответ назвал помеху от Уклонения. Достаточно слова
// «помеха»/«disadvantage»: цифр у Dodge нет.
func dodgeQuoted(answer string) bool {
	answer = strings.ToLower(strings.NewReplacer("*", "", "`", "").Replace(answer))
	return strings.Contains(answer, "помех") || strings.Contains(answer, "disadvantage")
}

var (
	reResultD20   = regexp.MustCompile(`d20\s*=\s*(\d+)`)
	reResultVsDC  = regexp.MustCompile(`=\s*(\d+)\s+vs\s+DC\s*(\d+)`)
	reResultTotal = regexp.MustCompile(`Итого:\s*(\d+)`)

	reAnswerD20 = regexp.MustCompile(`(?i)(?:d20\s*[=:]?\s*(\d+)|выпало\s*(\d+)|(\d+)\s+на\s+d20)`)
	reAnswerSum = regexp.MustCompile(`(?i)(?:итог[а-яё]*|всего|сумма|total|выпало)\D{0,15}(\d+)|=\s*(\d+)|бонус[а-яё]*\s*(\d+)|(\d+)\s+против\s+(?:dc|кд)`)
	reAnswerDC  = regexp.MustCompile(`(?i)(?:dc|кд|сл|сложност[а-яё]*)\s*[=:]?\s*(\d+)`)
)

// rollResultQuoted повторяет формы ответа, которые понимает скорер
// quotes_roll: d20, итог и DC должны быть названы, а не только подразумеваться.
func rollResultQuoted(answer, result string) bool {
	answer = strings.NewReplacer("*", "", "`", "").Replace(answer)
	if match := reResultD20.FindStringSubmatch(result); match != nil {
		if !containsNumber(answerNumbers(reAnswerD20, answer), match[1]) {
			return false
		}
	}
	if match := reResultVsDC.FindStringSubmatch(result); match != nil {
		if !containsNumber(answerNumbers(reAnswerSum, answer), match[1]) {
			return false
		}
		if !containsNumber(answerNumbers(reAnswerDC, answer), match[2]) {
			return false
		}
	}
	if match := reResultTotal.FindStringSubmatch(result); match != nil {
		if !containsNumber(answerNumbers(reAnswerSum, answer), match[1]) {
			return false
		}
	}
	return true
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
