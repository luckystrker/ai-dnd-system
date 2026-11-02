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
	return runToolQuote(t, toolName, "{}", result, answer)
}

func runToolQuote(t *testing.T, toolName, arguments, result, answer string) string {
	t.Helper()
	mw := &rollQuoteMW{}
	call := events.ToolCallEvent{ID: "call-1", Name: toolName, Arguments: arguments}
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
	for _, want := range []string{"«Восприятие»", "d20 = 11", "мод +0", "итого 11", "DC 12", "провал"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("в ответе нет %q:\n%s", want, reply)
		}
	}
	if !strings.Contains(reply, "Ты ничего не замечаешь.") {
		t.Fatalf("ответ модели потерялся:\n%s", reply)
	}
	if strings.Contains(reply, "🎲") {
		t.Fatalf("в ответ попала вся строка тула:\n%s", reply)
	}
}

// Частичная цитата не дублируется: дописываются только недостающие поля.
func TestRollQuoteAppendsOnlyMissingParts(t *testing.T) {
	t.Parallel()

	result := "🎲 Дэн — проверка «Восприятие» (МДР): d20 = 11 +0 = 11 vs DC 12 -> ПРОВАЛ"
	answer := "Провал: d20 = 11, итог 11."
	reply := runRollQuote(t, "skill_check", result, answer)
	if strings.Count(reply, "d20 = 11") != 1 {
		t.Fatalf("d20 продублирован:\n%s", reply)
	}
	if !strings.Contains(reply, "DC 12") {
		t.Fatalf("DC не дописан:\n%s", reply)
	}
}

// Уже процитированный бросок не дублируется.
func TestRollQuoteKeepsQuotedResult(t *testing.T) {
	t.Parallel()

	result := "🎲 Дэн — проверка «Восприятие» (МДР): d20 = 11 +0 = 11 vs DC 12 -> ПРОВАЛ"
	answer := "Провал: «Восприятие», d20 = 11, мод +0, итог 11 против DC 12."
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
	if !strings.Contains(reply, "итого 12") {
		t.Fatalf("в ответе нет итога броска:\n%s", reply)
	}
}

// Уклонение без слова «помеха» в ответе дописывает результат combat: правило
// Dodge требует объявить помеху игрокам.
func TestDodgeQuoteAppendsMissingDisadvantage(t *testing.T) {
	t.Parallel()

	result := "Дэн уклоняется (Dodge): до его следующего хода атаки по нему идут с помехой. Следующий ход: Лиса (lisa)."
	reply := runToolQuote(t, "combat", `{"action":"dodge","attacker":"Дэн"}`, result, "Дэн пригибается и ждёт.")
	if !strings.Contains(strings.ToLower(reply), "помех") {
		t.Fatalf("в ответе нет помехи:\n%s", reply)
	}
	if strings.Contains(reply, "Следующий ход") || strings.Contains(reply, "(lisa)") {
		t.Fatalf("в ответ попал служебный хвост очереди:\n%s", reply)
	}
}

// Если помеха уже названа, результат не дублируется.
func TestDodgeQuoteKeepsStatedDisadvantage(t *testing.T) {
	t.Parallel()

	result := "Дэн уклоняется (Dodge): до его следующего хода атаки по нему идут с помехой. Следующий ход: Лиса (lisa)."
	answer := "Дэн уклоняется — атаки по нему до его следующего хода идут с помехой. Ход Лисы."
	reply := runToolQuote(t, "combat", `{"action":"dodge","attacker":"Дэн"}`, result, answer)
	if reply != answer {
		t.Fatalf("ответ изменился при названной помехе:\nбыло: %s\nстало: %s", answer, reply)
	}
}

// Атака combat без цитаты получает d20, КД и вердикт, но не хвост очереди.
func TestRollQuoteAppendsCombatAttackParts(t *testing.T) {
	t.Parallel()

	result := "Дэн атакует Гоблин: d20 = 11 +5 против КД 13 → попадание, урон 5. Следующий ход: Лиса (lisa)."
	reply := runToolQuote(t, "combat", `{"action":"attack","enemy":"goblin-1"}`, result, "Дэн бьёт гоблина, урон 5.")
	for _, want := range []string{"d20 = 11", "против КД 13", "попадание"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("в ответе нет %q:\n%s", want, reply)
		}
	}
	if strings.Contains(reply, "Следующий ход") || strings.Contains(reply, "(lisa)") {
		t.Fatalf("в ответ попал служебный хвост очереди:\n%s", reply)
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
