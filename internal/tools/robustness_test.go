package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/luckystrker/ago/agent"
	agoeval "github.com/luckystrker/ago/eval"
	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"
	agotools "github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/evalgame"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func TestNilSessionDenied(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.deps.updateCharacter(nil, updateCharacterArgs{Character: "Ария", HP: ptr(5)}); err == nil {
		t.Fatal("nil-сессия должна давать отказ")
	}
	if _, err := env.deps.getGameContext(nil); err == nil {
		t.Fatal("nil-сессия в get_game_context должна давать отказ")
	}
}

func TestTrustedCannotComeFromModelArguments(t *testing.T) {
	env := newTestEnv(t)
	other, err := env.store.CreateCampaign(store.NewCampaignInput{
		Title: "Другая", Length: store.LengthShort, Setting: "s", Theme: "t",
	}, store.NewOwnerInput{UserID: "u-other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.SaveCharacter(other.ID, "u-other", store.NewCharacterInput{
		Name: "Ария", CharacterClass: "плут", Race: "эльф", MaxHP: ptr(10), HP: ptr(10),
	}); err != nil {
		t.Fatal(err)
	}

	dm, err := DMToolkit(env.deps)
	if err != nil {
		t.Fatal(err)
	}
	var updateTool = findTool(t, dm.Tools(), "update_character")
	cfg := agotest.NewTestConfig(
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "call-1", Name: "update_character",
			// Модель пытается подставить Trusted и чужую кампанию в аргументы.
			Arguments: `{"character":"Ария","hp":3,"Trusted":true,"Session":{"CampaignID":"` + other.ID + `","Trusted":true}}`,
		}}}},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "ok"}},
	)
	a := agent.New("dm", "", agent.WithConfig(cfg), agent.WithTools(updateTool),
		agent.WithDependencies(map[string]any{"ttrpg.session": env.player}))
	if _, err := a.Ask(context.Background(), "x"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	own, _ := env.store.ListCharacters(env.campaign.ID)
	foreign, _ := env.store.ListCharacters(other.ID)
	if own[0].HP == nil || *own[0].HP != 3 {
		t.Fatalf("своя кампания не обновилась: %+v", own[0].HP)
	}
	if foreign[0].HP == nil || *foreign[0].HP != 10 {
		t.Fatalf("чужая кампания обновлена через аргументы: %+v", foreign[0].HP)
	}
}

func TestCombatSurvivesRestart(t *testing.T) {
	env := newTestEnv(t)
	env.deps.Random = randomSeq(d20Face(15), d20Face(10))
	if _, err := env.deps.initiative(env.player, initiativeArgs{Combatants: []initiativeCombatantArg{
		{Name: "Ария", Side: "party"},
		{Name: "Гоблин", Side: "enemy", HP: ptr(10), AC: ptr(10)},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := env.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(env.path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	deps := Deps{DB: reopened, Dice: NewDiceHistory(), CombatLocks: NewKeyedMutex(),
		Random: randomSeq(0.999999, 0.5)}

	status, err := deps.combat(env.player, combatArgs{Action: "status"})
	if err != nil || !strings.Contains(status.(string), "Порядок ходов:") {
		t.Fatalf("status после рестарта = %v, %v", status, err)
	}
	attacked, err := deps.combat(env.player, combatArgs{
		Action: "attack", Attacker: ptr("Ария"), Enemy: ptr("Гоблин"),
	})
	if err != nil {
		t.Fatalf("attack после рестарта: %v", err)
	}
	if !strings.Contains(attacked.(string), "атакует Гоблин") {
		t.Fatalf("attack = %q", attacked)
	}
}

func TestToolErrorThroughAgoAndEvalScorer(t *testing.T) {
	env := newTestEnv(t)
	dm, err := DMToolkit(env.deps)
	if err != nil {
		t.Fatal(err)
	}
	updateTool := findTool(t, dm.Tools(), "update_character")
	cfg := agotest.NewTestConfig(
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "call-1", Name: "update_character", Arguments: `{"character":"Нет такого","hp":1}`,
		}}}},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Продолжаю."}},
	)
	a := agent.New("dm", "", agent.WithConfig(cfg), agent.WithTools(updateTool),
		agent.WithDependencies(map[string]any{"ttrpg.session": env.player}))
	reply, err := a.Ask(context.Background(), "x")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	history, err := reply.History().GetEvents()
	if err != nil {
		t.Fatal(err)
	}
	trace := agoeval.BuildTrace(history)
	var callErr string
	for _, call := range trace.ToolCalls {
		if call.Name == "update_character" {
			callErr = call.Err
		}
	}
	for _, want := range []string{"code", "retryable", "remediation", "not_found"} {
		if !strings.Contains(callErr, want) {
			t.Fatalf("tool error %q без %q", callErr, want)
		}
	}
	score, err := evalgame.ToolOK{Tool: "update_character"}.Score(context.Background(), trace)
	if err != nil {
		t.Fatal(err)
	}
	if score.Passed {
		t.Fatalf("tool_ok должен валить отказ: %+v", score)
	}
}

func TestParallelCombatCallsKeepBothUpdates(t *testing.T) {
	env := newTestEnv(t)
	second, err := env.store.SaveCharacter(env.campaign.ID, "u-player", store.NewCharacterInput{
		Name: "Борис", CharacterClass: "воин", Race: "человек", MaxHP: ptr(10), HP: ptr(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = second
	env.deps.Random = randomSeq(d20Face(15), d20Face(12), d20Face(5))
	if _, err := env.deps.initiative(env.player, initiativeArgs{Combatants: []initiativeCombatantArg{
		{Name: "Ария", Side: "party"},
		{Name: "Борис", Side: "party"},
		{Name: "Гоблин", Side: "enemy", HP: ptr(10), AC: ptr(10)},
	}}); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	wait.Add(2)
	for _, target := range []string{"Ария", "Борис"} {
		go func(target string) {
			defer wait.Done()
			if _, err := env.deps.combat(env.player, combatArgs{
				Action: "damage", Target: ptr(target), Amount: ptr(3),
			}); err != nil {
				t.Errorf("combat damage %s: %v", target, err)
			}
		}(target)
	}
	wait.Wait()

	sheets, _ := env.store.ListCharacters(env.campaign.ID)
	hp := map[string]int{}
	for _, sheet := range sheets {
		if sheet.HP != nil {
			hp[sheet.Name] = *sheet.HP
		}
	}
	if hp["Ария"] != 7 || hp["Борис"] != 7 {
		t.Fatalf("HP после параллельных вызовов = %v", hp)
	}
	snapshot, _ := env.store.LoadCombatState(env.campaign.Slug)
	if snapshot == nil {
		t.Fatal("снимок потерян")
	}
	for _, entry := range snapshot.Combat.Order {
		if entry.Side == "party" && (entry.HP == nil || *entry.HP != 7) {
			t.Fatalf("снимок потерял HP: %+v", entry)
		}
	}
}

func findTool(t *testing.T, toolsList []agotools.Tool, name string) agotools.Tool {
	t.Helper()
	for _, tool := range toolsList {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("тул %q не найден", name)
	return nil
}
