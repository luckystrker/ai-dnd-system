package dm

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/middleware"
)

// PlayerPromptAppend — одно срабатывание гарантии «вопрос другому игроку»:
// модель объявила передачу решения вызовом ask_player, а ответ не кончился
// вопросом, и код дописал его (PLAN 41).
type PlayerPromptAppend struct {
	Character string `json:"character"`
}

// playerPromptMW гарантирует закрывающий вопрос другому игроку. Сигнал —
// явный вызов тула ask_player (структурный, не эвристика по тексту ответа):
// модель сама объявляет, чьё решение ждёт ход, а middleware проверяет только
// наличие «?» в конце ответа. Без сигнала код ничего не дописывает.
type playerPromptMW struct {
	middleware.Base
	mu        sync.Mutex
	character string
	log       *slog.Logger
	onAppend  func(PlayerPromptAppend)
}

func (m *playerPromptMW) OnTurn(next middleware.TurnFunc, ev *events.ModelRequest, cx *agocontext.Context) (*events.ModelResponse, error) {
	m.mu.Lock()
	m.character = ""
	m.mu.Unlock()

	response, err := next(ev, cx)
	if err != nil || response == nil || response.Message == nil || len(response.ToolCalls.Calls) > 0 {
		return response, err
	}
	m.mu.Lock()
	character := m.character
	m.mu.Unlock()
	body := response.Message.Content
	if character == "" || endsWithQuestion(body) {
		return response, nil
	}
	question := character + ", что делаешь?"
	if strings.TrimSpace(body) == "" {
		response.Message.Content = question
	} else {
		response.Message.Content = strings.TrimRight(body, " \n") + "\n\n" + question
	}
	m.report(character, cx)
	return response, nil
}

func (m *playerPromptMW) OnToolExecution(next middleware.ToolExecFunc, ev *events.ToolCallEvent, cx *agocontext.Context) (events.Event, error) {
	result, err := next(ev, cx)
	if err != nil || ev.Name != "ask_player" {
		return result, err
	}
	if _, failed := events.AsValue(result).(events.ToolErrorEvent); failed {
		return result, nil
	}
	var args struct {
		Character string `json:"character"`
	}
	if json.Unmarshal([]byte(ev.Arguments), &args) != nil {
		return result, nil
	}
	character := strings.TrimSpace(args.Character)
	if character == "" {
		return result, nil
	}
	m.mu.Lock()
	m.character = character
	m.mu.Unlock()
	return result, nil
}

// report: строка в лог хода и колбэк для eval-счётчика.
func (m *playerPromptMW) report(character string, cx *agocontext.Context) {
	if m.log != nil {
		fields := []any{"character", character}
		if session := sessionFrom(cx); session != nil && session.ChatID != "" {
			fields = append(fields, "chat", session.ChatID)
		}
		m.log.Info("dm: player decision question appended", fields...)
	}
	if m.onAppend != nil {
		m.onAppend(PlayerPromptAppend{Character: character})
	}
}

// endsWithQuestion — последняя значимая фраза ответа заканчивается вопросом.
// Хвостовые кавычки, скобки и markdown-выделение не мешают; «!» не считается
// точкой конца фразы, «.» считается.
func endsWithQuestion(text string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(text), " \t\n\r\"'»”’)])}*_`")
	for index := len(trimmed) - 1; index >= 0; index-- {
		switch trimmed[index] {
		case '?':
			return true
		case '.':
			return false
		}
	}
	return false
}
