package dm

import (
	"errors"
	"strings"
	"testing"

	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
)

// runPlayerPrompt прогоняет ход: тул ask_player (или другой) отдаёт результат,
// затем модель пишет финальный ответ. Возвращает текст игроку.
func runPlayerPrompt(t *testing.T, mw *playerPromptMW, toolName, arguments, answer string) string {
	t.Helper()
	call := events.ToolCallEvent{ID: "call-1", Name: toolName, Arguments: arguments}
	execNext := func(*events.ToolCallEvent, *agocontext.Context) (events.Event, error) {
		return events.FromResultCall(call, events.NewToolResult(false, `{"ok":true}`)), nil
	}
	response := &events.ModelResponse{Message: &events.ModelMessage{Content: answer}}
	turnNext := func(*events.ModelRequest, *agocontext.Context) (*events.ModelResponse, error) {
		if toolName != "" {
			if _, err := mw.OnToolExecution(execNext, &call, nil); err != nil {
				return nil, err
			}
		}
		return response, nil
	}
	got, err := mw.OnTurn(turnNext, nil, nil)
	if err != nil {
		t.Fatalf("OnTurn: %v", err)
	}
	return got.Content()
}

// Сигнал ask_player есть, ответ кончился описанием — код дописывает вопрос
// этому персонажу и считает срабатывание.
func TestPlayerPromptAppendsMissingQuestion(t *testing.T) {
	t.Parallel()

	var got []PlayerPromptAppend
	mw := &playerPromptMW{onAppend: func(item PlayerPromptAppend) { got = append(got, item) }}
	reply := runPlayerPrompt(t, mw, "ask_player", `{"character":"Лиса"}`, "Дэн проходит между колоннами к алтарю.")
	if !strings.HasSuffix(strings.TrimSpace(reply), "Лиса, что делаешь?") {
		t.Fatalf("вопрос не дописан:\n%s", reply)
	}
	if !strings.Contains(reply, "Дэн проходит") {
		t.Fatalf("ответ модели потерялся:\n%s", reply)
	}
	if len(got) != 1 || got[0].Character != "Лиса" {
		t.Fatalf("счётчик = %+v, want одно срабатывание для Лисы", got)
	}
}

// Ответ уже кончается вопросом — ничего не дописываем и не считаем.
func TestPlayerPromptKeepsExistingQuestion(t *testing.T) {
	t.Parallel()

	var got []PlayerPromptAppend
	mw := &playerPromptMW{onAppend: func(item PlayerPromptAppend) { got = append(got, item) }}
	answer := "Дэн замирает у алтаря.\n\nЛиса, что делаешь?"
	if reply := runPlayerPrompt(t, mw, "ask_player", `{"character":"Лиса"}`, answer); reply != answer {
		t.Fatalf("ответ изменился:\nбыло: %s\nстало: %s", answer, reply)
	}
	if len(got) != 0 {
		t.Fatalf("срабатывание без дописки: %+v", got)
	}
}

// Без структурного сигнала гарантия молчит: это не эвристика по тексту.
func TestPlayerPromptWithoutSignalDoesNothing(t *testing.T) {
	t.Parallel()

	var got []PlayerPromptAppend
	mw := &playerPromptMW{onAppend: func(item PlayerPromptAppend) { got = append(got, item) }}
	answer := "Ветер шевелит траву у алтаря."
	if reply := runPlayerPrompt(t, mw, "", "", answer); reply != answer {
		t.Fatalf("ответ изменился без сигнала: %q", reply)
	}
	if len(got) != 0 {
		t.Fatalf("срабатывание без сигнала: %+v", got)
	}
}

// Ошибка тула — не сигнал: имя могло быть неверным, вопрос не дописываем.
func TestPlayerPromptIgnoresToolError(t *testing.T) {
	t.Parallel()

	var got []PlayerPromptAppend
	mw := &playerPromptMW{onAppend: func(item PlayerPromptAppend) { got = append(got, item) }}
	call := events.ToolCallEvent{ID: "call-1", Name: "ask_player", Arguments: `{"character":"Гэндальф"}`}
	execNext := func(*events.ToolCallEvent, *agocontext.Context) (events.Event, error) {
		return events.FromErrorCall(call, errors.New("неизвестный персонаж")), nil
	}
	response := &events.ModelResponse{Message: &events.ModelMessage{Content: "Дэн идёт к алтарю."}}
	turnNext := func(*events.ModelRequest, *agocontext.Context) (*events.ModelResponse, error) {
		if _, err := mw.OnToolExecution(execNext, &call, nil); err != nil {
			return nil, err
		}
		return response, nil
	}
	gotResponse, err := mw.OnTurn(turnNext, nil, nil)
	if err != nil {
		t.Fatalf("OnTurn: %v", err)
	}
	if gotResponse.Content() != "Дэн идёт к алтарю." || len(got) != 0 {
		t.Fatalf("ошибка тула засчитана как сигнал: %q %+v", gotResponse.Content(), got)
	}
}

// Промежуточный ответ с вызовами тулов не дописывается: финальный ещё будет.
func TestPlayerPromptSkipsIntermediateResponse(t *testing.T) {
	t.Parallel()

	mw := &playerPromptMW{}
	call := events.ToolCallEvent{ID: "call-1", Name: "ask_player", Arguments: `{"character":"Лиса"}`}
	execNext := func(*events.ToolCallEvent, *agocontext.Context) (events.Event, error) {
		return events.FromResultCall(call, events.NewToolResult(false, `{"ok":true}`)), nil
	}
	response := &events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{call}}}
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
	if got.Content() != "" {
		t.Fatalf("промежуточный ответ изменён: %q", got.Content())
	}
}

func TestEndsWithQuestion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		text string
		want bool
	}{
		{"", false},
		{"Дэн идёт к алтарю.", false},
		{"Дэн идёт к алтарю", false},
		{"Лиса, что делаешь?", true},
		{"**Лиса, что делаешь?**", true},
		{"«Лиса, что делаешь?»", true},
		{"Лиса, что делаешь?\n", true},
		{"Что делаешь? Потом уходим.", false},
		{"Готово!", false},
	}
	for _, tc := range cases {
		if got := endsWithQuestion(tc.text); got != tc.want {
			t.Errorf("endsWithQuestion(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
