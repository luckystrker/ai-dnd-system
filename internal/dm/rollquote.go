package dm

import (
	"regexp"
	"strings"
	"sync"

	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/middleware"
)

// rollQuoteMW гарантирует публичность бросков: если модель вызвала
// skill_check или roll_dice, но не назвала в ответе d20/итог/DC, точный
// результат тула дописывается в конец ответа. Промпт требует цитировать
// каждый бросок, но на длинных ходах модель это пропускает; правило важнее
// стиля, а числа уже есть в результате тула.
//
// Тулы initiative и combat не трогаем: их результат — многострочный порядок
// ходов, и модель пересказывает его сама; для них достаточно промпта.
type rollQuoteMW struct {
	middleware.Base
	mu      sync.Mutex
	results []string
}

func (m *rollQuoteMW) OnTurn(next middleware.TurnFunc, ev *events.ModelRequest, cx *agocontext.Context) (*events.ModelResponse, error) {
	m.mu.Lock()
	m.results = nil
	m.mu.Unlock()

	response, err := next(ev, cx)
	if err != nil || response == nil {
		return response, err
	}
	m.mu.Lock()
	results := append([]string(nil), m.results...)
	m.mu.Unlock()
	if len(results) == 0 || response.Message == nil || len(response.ToolCalls.Calls) > 0 {
		return response, nil
	}

	body := response.Message.Content
	missing := make([]string, 0, len(results))
	for _, result := range results {
		if !rollResultQuoted(body, result) {
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
	if ev.Name != "skill_check" && ev.Name != "roll_dice" {
		return result, nil
	}
	if _, failed := events.AsValue(result).(events.ToolErrorEvent); failed {
		return result, nil
	}
	if text := toolResultText(result); text != "" {
		m.mu.Lock()
		m.results = append(m.results, text)
		m.mu.Unlock()
	}
	return result, nil
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
