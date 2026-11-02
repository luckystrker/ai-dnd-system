package evalrun

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	agoconfig "github.com/luckystrker/ago/config"
	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/eval"
	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"

	"github.com/luckystriker/ai-dnd-system/internal/chronicler"
	"github.com/luckystriker/ai-dnd-system/internal/dayclose"
	"github.com/luckystriker/ai-dnd-system/internal/evalgame"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// scriptedFactory отдаёт независимый скрипт на каждый повтор: у TestConfig
// свой курсор, поэтому прогоны не съедают ответы друг друга.
func scriptedFactory(build func(sessionID string) []*events.ModelResponse) ModelFactory {
	return func(sessionID string) (agoconfig.ModelConfig, error) {
		return agotest.NewTestConfig(build(sessionID)...), nil
	}
}

// suiteWithMechanical только механические скореры: офлайн-прогон не должен
// зависеть от судьи.
func suiteWithMechanical(t *testing.T) evalgame.Suite {
	t.Helper()
	suite, err := evalgame.LoadSuite("../../eval/cases", evalgame.NewRegistry(nil))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	for index := range suite.Cases {
		suite.Cases[index] = mechanicalOnly(suite.Cases[index])
	}
	return suite
}

func TestRunnerRealToolkitWritesMemoryOnPlayerTurn(t *testing.T) {
	t.Parallel()

	suite := suiteWithMechanical(t)
	runner, err := NewRunner(suite, scriptedFactory(func(string) []*events.ModelResponse {
		return []*events.ModelResponse{
			{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{
				{ID: "c1", Name: "combat", Arguments: `{"action":"damage","target":"Дэн","amount":4}`},
				{ID: "c2", Name: "update_character", Arguments: `{"character":"Дэн","hp":8}`},
			}}},
			{Message: &events.ModelMessage{Content: "Кинжал рассекает плечо: минус 4 хита. Следующая — Лиса."}},
		}
	}), RunnerConfig{Repeats: 1, Concurrency: 1, TempRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	report, err := runner.Run(context.Background(), RunOptions{Cases: []string{"memory-01-damage-on-player-turn"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.FinishedAt.Before(report.StartedAt) {
		t.Fatalf("FinishedAt %v раньше StartedAt %v", report.FinishedAt, report.StartedAt)
	}
	if !report.Cases[0].PassPowK {
		t.Fatalf("кейс должен пройти на настоящих тулах, провал: %+v", report.Cases[0].Runs)
	}
	requireReportScore(t, report, "memory-01-damage-on-player-turn", "tool_ok", true)
}

func TestRunnerExecutesDaySummaryPostcondition(t *testing.T) {
	t.Parallel()

	suite := suiteWithMechanical(t)
	runner, err := NewRunner(suite, scriptedFactory(func(string) []*events.ModelResponse {
		return []*events.ModelResponse{
			{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "advance_day", Arguments: `{"note":"ночёвка в трактире"}`,
			}}}},
			{Message: &events.ModelMessage{Content: "Утро. День 2 начинается в трактире."}},
		}
	}), RunnerConfig{
		Repeats: 1, Concurrency: 1, TempRoot: t.TempDir(),
		NewArchivist: func(st *store.Store) dayclose.ArchivistFactory {
			return func(campaign *store.Campaign) (dayclose.Archivist, error) {
				return fakeArchivist{store: st}, nil
			}
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	report, err := runner.Run(context.Background(), RunOptions{Cases: []string{"day-01-overnight-rest"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	caseRun := report.Cases[0]
	if !caseRun.PassPowK {
		t.Fatalf("день должен закрыться саммари и шапкой: %+v", caseRun.Runs)
	}
	if len(caseRun.Runs[0].Postconditions) != 1 || !caseRun.Runs[0].Postconditions[0].Passed {
		t.Fatalf("postcondition day_summary не прошёл: %+v", caseRun.Runs[0].Postconditions)
	}
}

func TestRunnerDaySummaryPostconditionFailsWithoutAdvanceDay(t *testing.T) {
	t.Parallel()

	suite := suiteWithMechanical(t)
	runner, err := NewRunner(suite, scriptedFactory(func(string) []*events.ModelResponse {
		return []*events.ModelResponse{{Message: &events.ModelMessage{Content: "Вы отдыхаете у костра."}}}
	}), RunnerConfig{Repeats: 1, Concurrency: 1, TempRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	report, err := runner.Run(context.Background(), RunOptions{Cases: []string{"day-01-overnight-rest"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Cases[0].PassPowK {
		t.Fatal("без advance_day кейс не должен проходить: день не закрыт")
	}
	post := report.Cases[0].Runs[0].Postconditions
	if len(post) != 1 || post[0].Passed {
		t.Fatalf("postcondition должен упасть: %+v", post)
	}
}

// Повторы: первый прогон «провальный», второй — правильный. Pass@k истинен,
// Pass^k ложен, pass rate отчёта считается по кейсу целиком.
func TestRunnerPassAtKAndPassPowK(t *testing.T) {
	t.Parallel()

	suite := suiteWithMechanical(t)
	runner, err := NewRunner(suite, scriptedFactory(func(sessionID string) []*events.ModelResponse {
		// sessionID заканчивается номером повтора: -0 / -1.
		if strings.HasSuffix(sessionID, "-0") {
			return []*events.ModelResponse{{Message: &events.ModelMessage{Content: "Дэн замечает царапины."}}}
		}
		return []*events.ModelResponse{
			{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
				ID: "c1", Name: "skill_check",
				Arguments: `{"character_name":"Дэн","skill":"Восприятие","difficulty":12}`,
			}}}},
			{Message: &events.ModelMessage{Content: "d20 11, итог 12 против DC 12 — успех."}},
		}
	}), RunnerConfig{Repeats: 2, Concurrency: 1, TempRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	report, err := runner.Run(context.Background(), RunOptions{Cases: []string{"rolls-01-skill-check-public"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	caseRun := report.Cases[0]
	if !caseRun.PassAtK {
		t.Fatalf("Pass@k должен быть истинен: второй повтор прошёл: %+v", caseRun.Runs)
	}
	if caseRun.PassPowK {
		t.Fatal("Pass^k должен быть ложен: первый повтор провален")
	}
	if report.PassRate != 0 {
		t.Fatalf("pass rate кейса (все повторы) = %.2f, want 0", report.PassRate)
	}
	if report.RunResult().PassRate != 0 {
		t.Fatalf("Run.PassRate первого повтора = %.2f, want 0", report.RunResult().PassRate)
	}
}

func TestFaultMiddlewareReplacesNthCall(t *testing.T) {
	t.Parallel()

	factory := faultMiddleware(&evalgame.Fault{Tool: "skill_check", Call: 2, Error: "unknown skill: Восприятие"})
	fault := factory[0]() // отдельный ход = отдельный счётчик
	next := func(ev *events.ToolCallEvent, _ *agocontext.Context) (events.Event, error) {
		return events.FromResultCall(*ev, events.ToolResult{Parts: []events.Input{events.TextInput{Content: "ok"}}}), nil
	}
	var last events.Event
	for i := 1; i <= 3; i++ {
		event, err := fault.OnToolExecution(next, &events.ToolCallEvent{ID: "c", Name: "skill_check"}, nil)
		if err != nil {
			t.Fatalf("вызов %d: %v", i, err)
		}
		last = event
		if i == 2 {
			if _, isError := events.AsValue(event).(events.ToolErrorEvent); !isError {
				t.Fatalf("вызов 2 должен вернуть ToolErrorEvent, получено %T", events.AsValue(event))
			}
		}
	}
	if _, isError := events.AsValue(last).(events.ToolErrorEvent); isError {
		t.Fatal("вызов 3 не должен подменяться")
	}
}

func TestFaultMiddlewareIgnoresOtherTools(t *testing.T) {
	t.Parallel()

	event, err := faultMiddleware(&evalgame.Fault{Tool: "skill_check", Call: 1, Error: "x"})[0]().OnToolExecution(
		func(ev *events.ToolCallEvent, _ *agocontext.Context) (events.Event, error) {
			return events.FromResultCall(*ev, events.ToolResult{Parts: []events.Input{events.TextInput{Content: "ok"}}}), nil
		},
		&events.ToolCallEvent{ID: "c", Name: "roll_dice"}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, isError := events.AsValue(event).(events.ToolErrorEvent); isError {
		t.Fatal("чужой тул не подменяется")
	}
}

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

func requireReportScore(t *testing.T, report *evalgame.Report, caseName, scorer string, want bool) {
	t.Helper()
	for _, item := range report.Cases {
		if item.Name != caseName {
			continue
		}
		for _, run := range item.Runs {
			for _, score := range run.Scores {
				if score.Name == scorer {
					if score.Passed != want {
						t.Fatalf("%s: скорер %s Passed = %v, want %v (%v)", caseName, scorer, score.Passed, want, score.Details)
					}
					return
				}
			}
		}
	}
	t.Fatalf("скорер %s не найден у %s", scorer, caseName)
}

// Фикстура задаёт цель кампании: кейс открытия сессии проверяет пересказ
// «какая цель активна», и цель должна быть в базе, а не только в теме.
func TestFixtureCampaignHasGoal(t *testing.T) {
	t.Parallel()

	st, err := store.Open(filepath.Join(t.TempDir(), "campaigns.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = st.Close() }()
	fixture, err := buildFixture(st, "day-04-session-opener", evalgame.CaseMeta{
		Category: "day", Speaker: "Дэн", Role: "player",
	})
	if err != nil {
		t.Fatalf("buildFixture: %v", err)
	}
	campaign, err := st.GetCampaign(fixture.campaignID)
	if err != nil || campaign == nil {
		t.Fatalf("GetCampaign: %v", err)
	}
	if campaign.Goal == nil || strings.TrimSpace(*campaign.Goal) == "" {
		t.Fatalf("у кампании фикстуры нет цели: %+v", campaign.Goal)
	}
}

// fakeArchivist пишет саммари и шапку, как настоящий летописец, без LLM.
type fakeArchivist struct {
	store *store.Store
}

func (f fakeArchivist) CloseDay(_ context.Context, campaign *store.Campaign, day int, _ string) (chronicler.Report, error) {
	if err := f.store.SetDaySummary(campaign.Slug, day, "День прошёл в трактире."); err != nil {
		return chronicler.Report{}, err
	}
	if err := f.store.SetDayHeadline(campaign.Slug, day, "Ночёвка в трактире"); err != nil {
		return chronicler.Report{}, err
	}
	return chronicler.Report{DaySummaryWritten: true}, nil
}
