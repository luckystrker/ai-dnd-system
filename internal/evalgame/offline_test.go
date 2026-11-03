package evalgame

import (
	"context"
	"fmt"
	"testing"

	"github.com/luckystrker/ago/agent"
	"github.com/luckystrker/ago/eval"
	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"
	"github.com/luckystrker/ago/tools"
)

// cannedTool — тул с заранее заданным результатом: траектория в тесте
// скриптованная, настоящие правила движка здесь не проверяются.
func cannedTool(t *testing.T, name, result string) tools.Tool {
	t.Helper()
	tool, err := tools.NewFunctionTool(name, "test tool", func(args struct{}) (string, error) {
		return result, nil
	})
	if err != nil {
		t.Fatalf("tool %s: %v", name, err)
	}
	return tool
}

func mustLoadSuite(t *testing.T) Suite {
	t.Helper()
	suite, err := LoadSuite(casesDir, NewRegistry(nil))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	return suite
}

func findCase(t *testing.T, suite Suite, name string) eval.Case {
	t.Helper()
	for _, c := range suite.Cases {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("кейс %q не найден", name)
	return eval.Case{}
}

// mechanicalOnly убирает judge: офлайн у него нет модели.
func mechanicalOnly(c eval.Case) eval.Case {
	filtered := make([]eval.Scorer, 0, len(c.Scorers))
	for _, scorer := range c.Scorers {
		if scorer.Name() != "judge" {
			filtered = append(filtered, scorer)
		}
	}
	c.Scorers = filtered
	return c
}

// runMechanical прогоняет один кейс набора на скриптованной модели и
// возвращает результат с механическими скорерами.
func runMechanical(t *testing.T, suite Suite, name string, scripted []tools.Tool, responses ...*events.ModelResponse) eval.CaseResult {
	t.Helper()
	cfg := agotest.NewTestConfig(responses...)
	a := agent.New("dm", "test", agent.WithConfig(cfg), agent.WithTools(scripted...))
	return runCase(t, suite, name, a)
}

// runCase прогоняет один кейс на готовом агенте: так eval проверяет и
// настоящего DM-агента, а не только голый цикл ago.
func runCase(t *testing.T, suite Suite, name string, a *agent.Agent) eval.CaseResult {
	t.Helper()
	c := mechanicalOnly(findCase(t, suite, name))
	if len(c.Scorers) == 0 {
		t.Fatalf("у кейса %q нет механических скореров", name)
	}
	result, err := eval.Run(context.Background(), a, eval.Suite{c}, eval.WithConcurrency(1))
	if err != nil {
		t.Fatalf("eval.Run: %v", err)
	}
	if len(result.Cases) != 1 {
		t.Fatalf("кейсов в результате %d", len(result.Cases))
	}
	cr := result.Cases[0]
	if cr.Err != "" {
		t.Fatalf("кейс %q упал при прогоне: %s", name, cr.Err)
	}
	return cr
}

func requireScore(t *testing.T, cr eval.CaseResult, scorer string, want bool) {
	t.Helper()
	found := false
	for _, score := range cr.Scores {
		if score.Name != scorer {
			continue
		}
		found = true
		if score.Passed != want {
			t.Fatalf("скорер %q: Passed = %v, want %v (детали: %v)", scorer, score.Passed, want, score.Details)
		}
	}
	if !found {
		t.Fatalf("скорер %q не отработал, есть: %v", scorer, scoreNames(cr))
	}
}

func scoreNames(cr eval.CaseResult) []string {
	names := make([]string, 0, len(cr.Scores))
	for _, score := range cr.Scores {
		names = append(names, score.Name)
	}
	return names
}

const skillCheckResult = "🎲 Дэн — проверка «Восприятие» (МДР): d20 = 14 +3 = 17 vs DC 12 -> УСПЕХ"

func TestMechanicalScorersSkillCheck(t *testing.T) {
	t.Parallel()

	suite := mustLoadSuite(t)
	toolset := []tools.Tool{
		cannedTool(t, "skill_check", skillCheckResult),
		cannedTool(t, "roll_dice", "🎲 Бросок 1d20: 1d20 → [9] = 9\nИтого: 9"),
	}

	t.Run("правильный вызов проходит", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "rolls-01-skill-check-public", toolset,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "skill_check",
				Arguments: `{"character_name":"Дэн","skill":"Восприятие","difficulty":12}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Дэн замечает царапины на полу: d20 14, итог 17 против DC 12 — успех."}},
		)
		requireScore(t, cr, "tools_called", true)
		requireScore(t, cr, "tools_not_called", true)
		requireScore(t, cr, "quotes_roll", true)
	})

	t.Run("бросок кубиком вместо skill_check ловится", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "rolls-01-skill-check-public", toolset,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "roll_dice", Arguments: `{"notation":"1d20+3"}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Выпало 9, итог 12 против DC 12."}},
		)
		requireScore(t, cr, "tools_called", false)
		requireScore(t, cr, "tools_not_called", false)
	})

	t.Run("непроцитированный бросок ловится", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "rolls-01-skill-check-public", toolset,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "skill_check",
				Arguments: `{"character_name":"Дэн","skill":"Восприятие","difficulty":12}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Дэн замечает царапины на полу."}},
		)
		requireScore(t, cr, "quotes_roll", false)
	})

	t.Run("выдуманные числа у якорей ловятся", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "rolls-01-skill-check-public", toolset,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "skill_check",
				Arguments: `{"character_name":"Дэн","skill":"Восприятие","difficulty":12}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Бросок 12, итог 14 против 9 — провал."}},
		)
		requireScore(t, cr, "quotes_roll", false)
	})
}

func TestMechanicalScorersEnemyAttackNotation(t *testing.T) {
	t.Parallel()

	suite := mustLoadSuite(t)
	toolset := []tools.Tool{
		cannedTool(t, "roll_dice", "🎲 Бросок 1d20+4:\n1d20+4 → [13] = 13\nИтого: 17"),
	}

	cr := runMechanical(t, suite, "rolls-02-enemy-attack-notation", toolset,
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "c1", Name: "roll_dice", Arguments: `{"notation":"1d20+4"}`,
		}}}},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Гоблин бьёт: d20 13, итог 17 против КД Дэна — попадание."}},
	)
	requireScore(t, cr, "tools_called", true)
	requireScore(t, cr, "tool_args", true)
	requireScore(t, cr, "quotes_roll", true)
}

func TestMechanicalScorersInitiativeArgs(t *testing.T) {
	t.Parallel()

	suite := mustLoadSuite(t)
	initResult := "Инициатива:\n" +
		"1. Дэн [id: dan] (15 = 12 + 3, HP 10/10)\n" +
		"2. Лиса [id: lisa] (13 = 10 + 3, HP 8/8)\n" +
		"3. Гоблин [id: goblin-1] (11 = 8 + 3, враг, HP 7, КД 13)\n" +
		"4. Гоблин [id: goblin-2] (9 = 6 + 3, враг, HP 7, КД 13)\n" +
		"Бой начат, раунд 1. Первый ход: Дэн (dan)."
	toolset := []tools.Tool{cannedTool(t, "initiative", initResult)}
	announcement := "Порядок инициативы: Дэн — 15, Лиса — 13, гоблин-1 — 11, гоблин-2 — 9. Первым ходит Дэн."

	t.Run("все участники проходят", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "combat-01-initiative-all-participants", toolset,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "initiative",
				Arguments: `{"combatants":[` +
					`{"name":"Дэн","side":"party","bonus":3},` +
					`{"name":"Лиса","side":"party","bonus":3},` +
					`{"name":"Гоблин","side":"enemy","bonus":3,"hp":7,"ac":13},` +
					`{"name":"Гоблин","side":"enemy","bonus":3,"hp":7,"ac":13}]}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: announcement}},
		)
		requireScore(t, cr, "tools_called", true)
		requireScore(t, cr, "tool_args", true)
		requireScore(t, cr, "quotes_roll", true)
	})

	t.Run("пропущенный враг ловится", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "combat-01-initiative-all-participants", toolset,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "initiative",
				Arguments: `{"combatants":[` +
					`{"name":"Дэн","side":"party","bonus":3},` +
					`{"name":"Лиса","side":"party","bonus":3},` +
					`{"name":"Гоблин","side":"enemy","bonus":3,"hp":7,"ac":13}]}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: announcement}},
		)
		requireScore(t, cr, "tool_args", false)
	})
}

func TestMechanicalScorersMemoryWrite(t *testing.T) {
	t.Parallel()

	suite := mustLoadSuite(t)
	toolset := []tools.Tool{
		cannedTool(t, "combat", "Дэн: урон 4 HP → 6/10. Следующий ход: Лиса (lisa)."),
		cannedTool(t, "update_character", "Дэн сохранён: HP 6/10."),
	}

	damageCall := events.ToolCallEvent{ID: "c1", Name: "combat", Arguments: `{"action":"damage","target":"Дэн","amount":4}`}
	persistCall := events.ToolCallEvent{ID: "c2", Name: "update_character", Arguments: `{"character":"Дэн","hp":6}`}

	t.Run("запись на ходу игрока проходит", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "memory-01-damage-on-player-turn", toolset,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{damageCall, persistCall}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Кинжал рассекает плечо: минус 4 хита. Следующая — Лиса."}},
		)
		requireScore(t, cr, "tools_called", true)
		requireScore(t, cr, "tool_args", true)
		requireScore(t, cr, "tool_ok", true)
	})

	t.Run("забытый combat ловится", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "memory-01-damage-on-player-turn", toolset,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{persistCall}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Кинжал рассекает плечо: минус 4 хита. Следующая — Лиса."}},
		)
		requireScore(t, cr, "tools_called", false)
	})

	// Главный негатив: тулы вызваны, но ответили отказом по авторизации —
	// именно так баг групповой записи выглядел в TS-боте.
	t.Run("отказ доступа в результате ловится", func(t *testing.T) {
		t.Parallel()
		deniedTools := []tools.Tool{
			cannedTool(t, "combat", "Это действие доступно только администратору (dm) кампании."),
			cannedTool(t, "update_character", "Это действие доступно только администратору (dm) кампании."),
		}
		cr := runMechanical(t, suite, "memory-01-damage-on-player-turn", deniedTools,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{damageCall, persistCall}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Кинжал рассекает плечо: минус 4 хита. Следующая — Лиса."}},
		)
		requireScore(t, cr, "tools_called", true)
		requireScore(t, cr, "tool_args", true)
		requireScore(t, cr, "tool_ok", false)
	})
}

func TestMechanicalScorersToolErrorRetry(t *testing.T) {
	t.Parallel()

	suite := mustLoadSuite(t)
	calls := 0
	retry, err := tools.NewFunctionTool("skill_check", "test tool", func(args struct{}) (string, error) {
		calls++
		if calls == 1 {
			return "", fmt.Errorf("unknown skill: Восприятие")
		}
		return skillCheckResult, nil
	})
	if err != nil {
		t.Fatalf("skill_check: %v", err)
	}
	toolset := []tools.Tool{retry, cannedTool(t, "roll_dice", "Итого: 9")}

	// checks-05 отложен до слайса 7 (fault-инжекция), но его механические
	// скореры проверяются уже сейчас: ошибка → повтор → чистый результат.
	cr := runMechanical(t, suite, "checks-05-tool-error-retry", toolset,
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "c1", Name: "skill_check", Arguments: `{"character_name":"Лиса","skill":"Восприятие","difficulty":12}`,
		}}}},
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "c2", Name: "skill_check", Arguments: `{"character_name":"Лиса","skill":"Perception","difficulty":12}`,
		}}}},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Лиса напрягает слух: d20 14, итог 17 против DC 12 — успех."}},
	)
	requireScore(t, cr, "tools_called", true)
	requireScore(t, cr, "tools_not_called", true)
	requireScore(t, cr, "answer_omits", true)
}

func TestMechanicalScorersDayClose(t *testing.T) {
	t.Parallel()

	suite := mustLoadSuite(t)
	toolset := []tools.Tool{cannedTool(t, "advance_day", "День 3 закрыт, начался день 4.")}

	cr := runMechanical(t, suite, "day-01-overnight-rest", toolset,
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "c1", Name: "advance_day", Arguments: `{"note":"ночёвка в трактире"}`,
		}}}},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Утро. День 4 начинается в трактире."}},
	)
	requireScore(t, cr, "tools_called", true)
}

func TestMechanicalScorersQuietGroupChat(t *testing.T) {
	t.Parallel()

	suite := mustLoadSuite(t)
	toolset := []tools.Tool{
		cannedTool(t, "skill_check", skillCheckResult),
		cannedTool(t, "roll_dice", "Итого: 9"),
		cannedTool(t, "initiative", "Инициатива: ..."),
		cannedTool(t, "combat", "Раунд 1."),
	}

	t.Run("без бросков проходит", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "group-01-ooc-plan-discussion", toolset,
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Двор пуст, у колодца следы. Жду вашего решения."}},
		)
		requireScore(t, cr, "tools_not_called", true)
	})

	t.Run("лишний бросок ловится", func(t *testing.T) {
		t.Parallel()
		cr := runMechanical(t, suite, "group-01-ooc-plan-discussion", toolset,
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "skill_check", Arguments: `{"character_name":"Дэн","skill":"Восприятие","difficulty":10}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Двор пуст."}},
		)
		requireScore(t, cr, "tools_not_called", false)
	})
}
