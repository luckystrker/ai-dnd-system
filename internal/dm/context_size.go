package dm

import (
	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/policies"
)

// charsPerToken — та же оценка, что у ago (events.EstimatedTokens): 4 символа
// на токен.
const charsPerToken = 4

// contentTokens оценивает размер события по его содержимому. Штатный
// events.EstimatedTokens для ModelResponse прибавляет Usage ответа, то есть
// весь промпт вызова (система, тулы, память, история). В истории таких
// ответов много, и сумма быстро превышает окно, хотя сама история маленькая:
// компакция срабатывала бы после каждого хода и удваивала платные вызовы.
func contentTokens(ev events.Event) int {
	if ev == nil {
		return 0
	}
	if resp, ok := events.AsValue(ev).(events.ModelResponse); ok {
		resp.Usage = events.Usage{}
		return events.EstimatedTokens(resp, charsPerToken)
	}
	return events.EstimatedTokens(ev, charsPerToken)
}

func contentSize(evs []events.Event) int {
	total := 0
	for _, ev := range evs {
		total += contentTokens(ev)
	}
	return total
}

// contentTrigger — compact.Trigger по содержимому истории, а не по
// заявленному Usage ответов.
type contentTrigger struct{ budget int }

func (t contentTrigger) ShouldTrigger(evs []events.Event, _ events.Usage) bool {
	if t.budget <= 0 {
		return false
	}
	return contentSize(evs) > t.budget
}

// contentBudgetPolicy — cap окна по содержимому. Оборачивает штатную
// TokenBudgetPolicy: подменяет ModelResponse копиями с обнулённым Usage,
// применяет политику (она сохраняет суффикс и целые тул-раунды), затем
// возвращает исходные события по указателям копий.
type contentBudgetPolicy struct {
	inner policies.AssemblyPolicy
}

var _ policies.AssemblyPolicy = contentBudgetPolicy{}

func newContentBudgetPolicy(budget int) contentBudgetPolicy {
	return contentBudgetPolicy{inner: policies.NewTokenBudgetPolicy(budget)}
}

func (p contentBudgetPolicy) Apply(prompts []string, evs []events.Event, cx *agocontext.Context) ([]string, []events.Event, error) {
	if p.inner == nil {
		return prompts, evs, nil
	}
	copies := make([]events.Event, len(evs))
	original := make(map[*events.ModelResponse]events.Event)
	for index, ev := range evs {
		if resp, ok := events.AsValue(ev).(events.ModelResponse); ok {
			zero := resp
			zero.Usage = events.Usage{}
			ptr := &zero
			original[ptr] = ev
			copies[index] = ptr
			continue
		}
		copies[index] = ev
	}
	keptPrompts, kept, err := p.inner.Apply(prompts, copies, cx)
	if err != nil {
		return nil, nil, err
	}
	out := make([]events.Event, 0, len(kept))
	for _, ev := range kept {
		if ptr, ok := ev.(*events.ModelResponse); ok {
			if source, found := original[ptr]; found {
				out = append(out, source)
				continue
			}
		}
		out = append(out, ev)
	}
	return keptPrompts, out, nil
}
