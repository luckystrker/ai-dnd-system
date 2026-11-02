package dm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/luckystrker/ago/agent"
	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"
	"github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/dm"
)

// toolCallResponse — ответ модели, который просит один вызов тула.
func toolCallResponse(id, name string) *events.ModelResponse {
	return &events.ModelResponse{
		ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{
			{ID: id, Name: name, Arguments: `{}`},
		}},
	}
}

// Явный WithMaxToolRounds: модель, которая бесконечно просит тул, должна
// остановиться после заданного числа раундов, а не крутиться до таймаута.
func TestNewAppliesMaxToolRounds(t *testing.T) {
	t.Parallel()

	tick, err := tools.NewFunctionTool("tick", "Ticks", func(args struct{}) (string, error) {
		return "tock", nil
	})
	if err != nil {
		t.Fatalf("tool: %v", err)
	}

	cfg := agotest.NewTestConfig(
		toolCallResponse("call-1", "tick"),
		toolCallResponse("call-2", "tick"),
		toolCallResponse("call-3", "tick"),
	)
	a := dm.New(cfg, dm.Options{MaxToolRounds: 2, Tools: []tools.Tool{tick}})

	run, err := a.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer run.Close()
	if _, err := run.Result(context.Background()); !errors.Is(err, agent.ErrMaxToolRounds) {
		t.Fatalf("ожидалась agent.ErrMaxToolRounds, получено %v", err)
	}
	if got := len(cfg.Calls()); got != 3 {
		t.Fatalf("вызовов модели = %d, want 3 (первый + 2 раунда тулов)", got)
	}
}
