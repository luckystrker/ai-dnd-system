package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/luckystrker/ago/agent"
	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"
	"github.com/luckystrker/ago/tools"
)

// DI-слот aginject доходит до тула через агента: модель не передаёт ни
// campaignSlug, ни сессию — контекст приходит из dependencies.
func TestSessionInjectedThroughAgo(t *testing.T) {
	env := newTestEnv(t)
	dm, err := DMToolkit(env.deps)
	if err != nil {
		t.Fatal(err)
	}
	var updateTool tools.Tool
	for _, item := range dm.Tools() {
		if item.Name() == "update_character" {
			updateTool = item
		}
	}
	if updateTool == nil {
		t.Fatal("update_character нет в toolkit")
	}
	cfg := agotest.NewTestConfig(
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "call-1", Name: "update_character", Arguments: `{"character":"Ария","hp":5}`,
		}}}},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Готово."}},
	)
	a := agent.New("dm", "test", agent.WithConfig(cfg), agent.WithTools(updateTool),
		agent.WithDependencies(map[string]any{"ttrpg.session": env.player}))
	if _, err := a.Ask(context.Background(), "Ария теряет 5 хитов"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	sheets, err := env.store.ListCharacters(env.campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sheets[0].HP == nil || *sheets[0].HP != 5 {
		t.Fatalf("HP = %+v: DI-сессия не дошла до тула", sheets[0].HP)
	}
}

func TestSchemasHideSessionAndCampaignSlug(t *testing.T) {
	dm, err := DMToolkit(Deps{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range dm.Tools() {
		schemas, err := item.Schemas(nil)
		if err != nil {
			t.Fatalf("%s: %v", item.Name(), err)
		}
		data, err := json.Marshal(schemas)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "campaignSlug") {
			t.Errorf("%s: campaignSlug всё ещё в схеме", item.Name())
		}
		if strings.Contains(string(data), "ttrpg.session") || strings.Contains(string(data), "Session") {
			t.Errorf("%s: DI-сессия попала в схему", item.Name())
		}
	}
}
