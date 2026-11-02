package dm

import (
	"strings"
	"testing"

	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
)

// runRollQuote прогоняет middleware как в ходе: сначала тул отдаёт результат,
// затем модель пишет ответ. Возвращает итоговый текст игроку.
func runRollQuote(t *testing.T, toolName, result, answer string) string {
	t.Helper()
	mw := &rollQuoteMW{}
	call := events.ToolCallEvent{ID: "call-1", Name: toolName}
	execNext := func(*events.ToolCallEvent, *agocontext.Context) (events.Event, error) {
		return events.FromResultCall(call, events.NewToolResult(false, result)), nil
	}
	response := &events.ModelResponse{Message: &events.ModelMessage{Content: answer}}
	turnNext := func(*events.ModelRequest, *agocontext.Context) (*events.ModelResponse, error) {
		if _, err := mw.OnToolExecution(execNext, &call, nil); err != nil {
			return nil, err
		}
		return response, nil
	}
	got, err := mw.OnTurn(turnNext, nil, nil)
	if err != nil {
		t.Fatalf("OnTurn: %v", err)
	}
	return got.Content()
}

// Если модель не процитировала бросок, middleware дописывает точный результат
// тула: броски публичны, а не остаются только в трассе.
func TestRollQuoteAppendsMissingResult(t *testing.T) {
	t.Parallel()

	result := "🎲 Дэн — проверка «Восприятие» (МДР): d20 = 11 +0 = 11 vs DC 12 -> ПРОВАЛ"
	reply := runRollQuote(t, "skill_check", result, "Ты ничего не замечаешь.")
	for _, want := range []string{"d20 = 11", "DC 12"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("в ответе нет %q:\n%s", want, reply)
		}
	}
	if !strings.Contains(reply, "Ты ничего не замечаешь.") {
		t.Fatalf("ответ модели потерялся:\n%s", reply)
	}
}

// Уже процитированный бросок не дублируется.
func TestRollQuoteKeepsQuotedResult(t *testing.T) {
	t.Parallel()

	result := "🎲 Дэн — проверка «Восприятие» (МДР): d20 = 11 +0 = 11 vs DC 12 -> ПРОВАЛ"
	answer := "Провал: d20 = 11, итог 11 против DC 12."
	reply := runRollQuote(t, "skill_check", result, answer)
	if reply != answer {
		t.Fatalf("ответ изменился при цитате:\nбыло: %s\nстало: %s", answer, reply)
	}
}

// roll_dice без «Итого» тоже дописывается.
func TestRollQuoteAppendsDiceTotal(t *testing.T) {
	t.Parallel()

	result := "🎲 Бросок 3d6:\n3d6 → [4, 4, 4] = 12\nИтого: 12"
	reply := runRollQuote(t, "roll_dice", result, "Ты находишь двенадцать монет.")
	if !strings.Contains(reply, "Итого: 12") {
		t.Fatalf("в ответе нет итога броска:\n%s", reply)
	}
}

// Чужие тулы middleware не трогает.
func TestRollQuoteIgnoresNonRollTools(t *testing.T) {
	t.Parallel()

	answer := "Дверь открывается."
	reply := runRollQuote(t, "tick", "tock", answer)
	if reply != answer {
		t.Fatalf("ответ изменился на небросковом туле: %q", reply)
	}
}
