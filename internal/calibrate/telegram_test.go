package calibrate

import (
	"testing"
	"time"
)

// Значения окон не меняются: тест фиксирует данные калибровки, а не
// подгоняет их. Ожидания — сколько батчей будит LLM и сколько подавляет.
func TestRunScenarios(t *testing.T) {
	t.Parallel()

	report := Run(Scenarios(), DefaultConfig())
	want := map[string]struct {
		batches    int
		turns      int
		suppressed int
	}{
		"ooc-dialog":             {batches: 2, turns: 0, suppressed: 2},
		"burst-then-question":    {batches: 2, turns: 1, suppressed: 1},
		"command":                {batches: 1, turns: 1, suppressed: 0},
		"reply-within-follow-up": {batches: 2, turns: 2, suppressed: 0},
		"reply-after-follow-up":  {batches: 2, turns: 1, suppressed: 1},
		"mention":                {batches: 2, turns: 1, suppressed: 1},
	}

	if len(report.Scenarios) != len(want) {
		t.Fatalf("сценариев %d, want %d", len(report.Scenarios), len(want))
	}
	for _, scenario := range report.Scenarios {
		expected, ok := want[scenario.Name]
		if !ok {
			t.Fatalf("неизвестный сценарий %q", scenario.Name)
		}
		if scenario.Batches != expected.batches || scenario.Turns != expected.turns || scenario.Suppressed != expected.suppressed {
			t.Errorf("%s: батчей %d (want %d), ходов %d (want %d), подавлено %d (want %d)",
				scenario.Name, scenario.Batches, expected.batches,
				scenario.Turns, expected.turns, scenario.Suppressed, expected.suppressed)
		}
	}
}

// Окно склейки: две реплики с интервалом меньше 2.5 с — один ход, больше —
// два. Это данные для владельца, значение окна тест не меняет.
func TestCoalesceWindowBoundary(t *testing.T) {
	t.Parallel()

	base := Config{Coalesce: 2500 * 1e6, FollowUp: DefaultConfig().FollowUp, TurnLength: DefaultConfig().TurnLength, BotName: "dnd_bot"}
	cases := []struct {
		name     string
		gap      int
		expected int
	}{
		{name: "внутри окна", gap: 2400, expected: 1},
		{name: "на границе", gap: 2600, expected: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := Run([]Scenario{{
				Name: "boundary",
				Messages: []Msg{
					{At: 0, User: "den", Text: "ДМ, я иду вперёд."},
					{At: durationMS(tc.gap), User: "den", Text: "И открываю дверь."},
				},
			}}, base)
			if report.Scenarios[0].Batches != tc.expected {
				t.Fatalf("батчей %d, want %d", report.Scenarios[0].Batches, tc.expected)
			}
		})
	}
}

func durationMS(ms int) time.Duration { return time.Duration(ms) * time.Millisecond }
