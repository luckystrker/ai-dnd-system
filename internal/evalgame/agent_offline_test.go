package evalgame

import (
	"path/filepath"
	"testing"

	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"
	"github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/dm"
	"github.com/luckystriker/ai-dnd-system/internal/store"
	ttrpg "github.com/luckystriker/ai-dnd-system/internal/tools"
)

// dmEvalAgent собирает настоящего DM-агента (промпт, память, транскрипт,
// компакция, лимиты) на скриптованной модели и подменном наборе тулов: eval
// проверяет траекторию, а не живые правила движка.
func dmEvalAgent(t *testing.T, scripted []tools.Tool, responses ...*events.ModelResponse) *dm.DM {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "campaigns.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Eval", Length: store.LengthShort, Setting: "s", Theme: "t",
	}, store.NewOwnerInput{UserID: "dm-1"})
	if err != nil {
		t.Fatalf("campaign: %v", err)
	}
	if _, err := st.BindAndActivate(campaign.ID, "dm-1", store.BoundChat{ChatID: "-100"}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := st.AddMember(campaign.ID, "dm-1", store.NewMemberInput{UserID: "player-1"}); err != nil {
		t.Fatalf("member: %v", err)
	}
	if _, err := st.SaveCharacter(campaign.ID, "player-1", store.NewCharacterInput{
		Name: "Дэн", CharacterClass: "воин", Race: "человек",
		HP: intPtr(12), MaxHP: intPtr(12),
	}); err != nil {
		t.Fatalf("character: %v", err)
	}
	cfg := agotest.NewTestConfig(responses...)
	agent, err := dm.New(dm.Config{
		Model: cfg, Store: st, Session: &ttrpg.Session{
			CampaignID: campaign.ID, CampaignSlug: campaign.Slug,
			UserID: "player-1", Username: "den", ChatID: "-100", ChatType: "supergroup",
		},
		SkipToolkit: true, ExtraTools: scripted,
	})
	if err != nil {
		t.Fatalf("dm.New: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

func intPtr(value int) *int { return &value }

// Тот же механический eval, но через настоящего DM-агента: промпт, память
// кампании и транскрипт включены.
func TestEvalThroughRealDMAgent(t *testing.T) {
	t.Parallel()

	suite := mustLoadSuite(t)

	t.Run("бросок проверки цитируется", func(t *testing.T) {
		t.Parallel()
		d := dmEvalAgent(t, []tools.Tool{
			cannedTool(t, "skill_check", skillCheckResult),
			cannedTool(t, "roll_dice", "🎲 Бросок 1d20: 1d20 → [9] = 9\nИтого: 9"),
		},
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "skill_check",
				Arguments: `{"character_name":"Дэн","skill":"Восприятие","difficulty":12}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Дэн замечает царапины на полу: d20 14, итог 17 против DC 12 — успех."}},
		)
		cr := runCase(t, suite, "rolls-01-skill-check-public", d.Agent())
		requireScore(t, cr, "tools_called", true)
		requireScore(t, cr, "tools_not_called", true)
		requireScore(t, cr, "quotes_roll", true)
	})

	t.Run("нотация атаки врага", func(t *testing.T) {
		t.Parallel()
		d := dmEvalAgent(t, []tools.Tool{
			cannedTool(t, "roll_dice", "🎲 Бросок 1d20+4:\n1d20+4 → [13] = 13\nИтого: 17"),
		},
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "roll_dice", Arguments: `{"notation":"1d20+4"}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Гоблин бьёт: d20 13, итог 17 против КД Дэна — попадание."}},
		)
		cr := runCase(t, suite, "rolls-02-enemy-attack-notation", d.Agent())
		requireScore(t, cr, "tools_called", true)
		requireScore(t, cr, "tool_args", true)
		requireScore(t, cr, "quotes_roll", true)
	})

	t.Run("инициатива со всеми участниками", func(t *testing.T) {
		t.Parallel()
		initResult := "Инициатива:\n" +
			"1. Дэн [id: dan] (15 = 12 + 3, HP 10/10)\n" +
			"2. Лиса [id: lisa] (13 = 10 + 3, HP 8/8)\n" +
			"3. Гоблин [id: goblin-1] (11 = 8 + 3, враг, HP 7, КД 13)\n" +
			"4. Гоблин [id: goblin-2] (9 = 6 + 3, враг, HP 7, КД 13)\n" +
			"Бой начат, раунд 1. Первый ход: Дэн (dan)."
		d := dmEvalAgent(t, []tools.Tool{cannedTool(t, "initiative", initResult)},
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "initiative",
				Arguments: `{"combatants":[` +
					`{"name":"Дэн","side":"party","bonus":3},` +
					`{"name":"Лиса","side":"party","bonus":3},` +
					`{"name":"Гоблин","side":"enemy","bonus":3,"hp":7,"ac":13},` +
					`{"name":"Гоблин","side":"enemy","bonus":3,"hp":7,"ac":13}]}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Порядок инициативы: Дэн — 15, Лиса — 13, гоблин-1 — 11, гоблин-2 — 9. Первым ходит Дэн."}},
		)
		cr := runCase(t, suite, "combat-01-initiative-all-participants", d.Agent())
		requireScore(t, cr, "tools_called", true)
		requireScore(t, cr, "tool_args", true)
		requireScore(t, cr, "quotes_roll", true)
	})

	// Негативный контроль: бросок был, но ответ его не цитирует — скорер
	// должен упасть, иначе гейт не ловит регрессию промпта.
	t.Run("непроцитированный бросок ловится", func(t *testing.T) {
		t.Parallel()
		d := dmEvalAgent(t, []tools.Tool{cannedTool(t, "skill_check", skillCheckResult)},
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "skill_check",
				Arguments: `{"character_name":"Дэн","skill":"Восприятие","difficulty":12}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Дэн замечает царапины на полу."}},
		)
		cr := runCase(t, suite, "rolls-01-skill-check-public", d.Agent())
		requireScore(t, cr, "quotes_roll", false)
	})

	// Негативный контроль: в тихом разговоре группы модель зовёт тул —
	// tools_not_called должен упасть.
	t.Run("лишний тул в тихой группе ловится", func(t *testing.T) {
		t.Parallel()
		d := dmEvalAgent(t, []tools.Tool{
			cannedTool(t, "skill_check", skillCheckResult),
			cannedTool(t, "roll_dice", "Итого: 9"),
		},
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "skill_check",
				Arguments: `{"character_name":"Дэн","skill":"Восприятие","difficulty":10}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Двор пуст."}},
		)
		cr := runCase(t, suite, "group-01-ooc-plan-discussion", d.Agent())
		requireScore(t, cr, "tools_not_called", false)
	})

	// Ответ без тулов: нейтральная реакция мира без похвалы выбору игрока.
	t.Run("нейтральность без тулов", func(t *testing.T) {
		t.Parallel()
		d := dmEvalAgent(t, nil,
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Лиса перелезает через стену: на той стороне её встречает тишина двора. Что она делает дальше?"}},
		)
		cr := runCase(t, suite, "neutrality-02-no-praise-for-choice", d.Agent())
		requireScore(t, cr, "answer_omits", true)
	})

	// Запись боевого состояния на ходу обычного игрока: проверяется форма
	// траектории (какие тулы и с какими аргументами). Авторизацию настоящего
	// toolkit проверяет internal/dm.TestDMToolkitDeniesOutsiderMemoryWrite.
	t.Run("запись памяти на ходу игрока", func(t *testing.T) {
		t.Parallel()
		d := dmEvalAgent(t, []tools.Tool{
			cannedTool(t, "combat", "Дэн: урон 4 HP → 6/10. Следующий ход: Лиса (lisa)."),
			cannedTool(t, "update_character", "Дэн сохранён: HP 6/10."),
		},
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{
				{ID: "c1", Name: "combat", Arguments: `{"action":"damage","target":"Дэн","amount":4}`},
				{ID: "c2", Name: "update_character", Arguments: `{"character":"Дэн","hp":6}`},
			}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Кинжал рассекает плечо: минус 4 хита. Следующая — Лиса."}},
		)
		cr := runCase(t, suite, "memory-01-damage-on-player-turn", d.Agent())
		requireScore(t, cr, "tools_called", true)
		requireScore(t, cr, "tool_args", true)
		requireScore(t, cr, "tool_ok", true)
	})

	// Закрытие дня: DM зовёт advance_day; реальную очередь и летописца
	// проверяют internal/dayclose и internal/dm.
	t.Run("закрытие дня через advance_day", func(t *testing.T) {
		t.Parallel()
		d := dmEvalAgent(t, []tools.Tool{cannedTool(t, "advance_day", "День 3 закрыт, начался день 4.")},
			&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "advance_day", Arguments: `{"note":"ночёвка в трактире"}`,
			}}}},
			&events.ModelResponse{Message: &events.ModelMessage{Content: "Утро. День 4 начинается в трактире."}},
		)
		cr := runCase(t, suite, "day-01-overnight-rest", d.Agent())
		requireScore(t, cr, "tools_called", true)
	})
}
