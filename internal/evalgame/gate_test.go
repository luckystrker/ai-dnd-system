package evalgame

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luckystrker/ago/eval"
)

func TestParseCategoryThresholds(t *testing.T) {
	t.Parallel()

	parsed, err := ParseCategoryThresholds("agency=0.7, memory=0.8")
	if err != nil {
		t.Fatalf("ParseCategoryThresholds: %v", err)
	}
	if parsed["agency"] != 0.7 || parsed["memory"] != 0.8 {
		t.Fatalf("пороги разобраны неверно: %v", parsed)
	}
	for _, bad := range []string{"agency", "agency=2", "agency=x"} {
		if _, err := ParseCategoryThresholds(bad); err == nil {
			t.Fatalf("%q должен отклоняться", bad)
		}
	}
}

func TestGateViolations(t *testing.T) {
	t.Parallel()

	report := &Report{
		PassRate: 0.5,
		Categories: map[string]CategorySummary{
			"memory": {Cases: 2, Passed: 2, PassRate: 1},
			"day":    {Cases: 1, Passed: 0, PassRate: 0},
		},
		Cases: []CaseReport{
			{Name: "memory-01-damage-on-player-turn", Passes: 1, Repeats: 1, PassPowK: true},
			{Name: "day-01-overnight-rest", Passes: 0, Repeats: 1},
		},
		Run: &eval.RunResult{},
	}
	gate := Gate{
		MinPassRate: 0.8,
		Categories:  map[string]float64{"memory": 0.8, "day": 0.5, "agency": 0.5},
		Required:    []string{"memory-01-damage-on-player-turn", "day-01-overnight-rest", "rolls-01-skill-check-public"},
	}
	violations := gate.Violations(report)
	if len(violations) != 5 {
		t.Fatalf("ожидалось 5 нарушений, получено %d: %v", len(violations), violations)
	}
	joined := ""
	for _, item := range violations {
		joined += item + "\n"
	}
	for _, want := range []string{"общий pass rate", "категория \"day\"", "категория \"agency\"", "day-01-overnight-rest", "rolls-01-skill-check-public"} {
		if !containsText(joined, want) {
			t.Fatalf("нет нарушения %q в %s", want, joined)
		}
	}
}

func TestRegressionViolations(t *testing.T) {
	t.Parallel()

	baseline := &eval.RunResult{Cases: []eval.CaseResult{
		{Name: "rolls-01", Passed: true},
		{Name: "day-01", Passed: true},
	}}
	current := &eval.RunResult{Cases: []eval.CaseResult{
		{Name: "rolls-01", Passed: false},
		{Name: "day-01", Passed: true},
		{Name: "checks-05", Passed: true},
	}}
	violations := RegressionViolations(current, baseline)
	if len(violations) != 1 || !containsText(violations[0], "rolls-01") {
		t.Fatalf("ожидалась одна регрессия rolls-01, получено: %v", violations)
	}
}

// Калибровка судьи: обвязка различает good/bad по вердикту скорера. Живой
// прогон проверяет то же самое настоящим судьёй (cmd/eval -judge-calibration).
func TestCalibrateSeparatesGoodAndBad(t *testing.T) {
	t.Parallel()

	cases := []CalibrationCase{
		{ID: "good-1", Label: "good", Rubric: "agency", Answer: "мир ждёт"},
		{ID: "good-2", Label: "good", Rubric: "agency", Answer: "тоже ждёт"},
		{ID: "bad-1", Label: "bad", Rubric: "agency", Answer: "Дэн кивает"},
		{ID: "bad-2", Label: "bad", Rubric: "agency", Answer: "Дэн смеётся"},
	}
	report := Calibrate(context.Background(), cases, func(rubric string) eval.Scorer {
		return fakeJudge{fail: "Дэн", rubric: rubric}
	})
	if !report.Passed {
		t.Fatalf("калибровка должна пройти: %+v", report)
	}
	if report.GoodPassed != 2 || report.BadFailed != 2 || report.Accuracy != 1 {
		t.Fatalf("неверная сводка: %+v", report)
	}
}

func TestCalibrateFailsWhenJudgeAcceptsBadAnswer(t *testing.T) {
	t.Parallel()

	cases := []CalibrationCase{
		{ID: "good-1", Label: "good", Rubric: "agency", Answer: "мир ждёт"},
		{ID: "bad-1", Label: "bad", Rubric: "agency", Answer: "Дэн кивает"},
	}
	report := Calibrate(context.Background(), cases, func(rubric string) eval.Scorer {
		return fakeJudge{fail: "ничего", rubric: rubric}
	})
	if report.Passed {
		t.Fatal("судья, который всё принимает, не должен проходить калибровку")
	}
}

func TestLoadCalibration(t *testing.T) {
	t.Parallel()

	items, err := LoadCalibration(filepath.Join("..", "..", "eval", "judge", "calibration.jsonl"))
	if err != nil {
		t.Fatalf("LoadCalibration: %v", err)
	}
	if len(items) < 6 {
		t.Fatalf("нужно минимум 3 good и 3 bad, получено %d", len(items))
	}
	good, bad := 0, 0
	for _, item := range items {
		switch item.Label {
		case "good":
			good++
		case "bad":
			bad++
		}
	}
	if good < 3 || bad < 3 {
		t.Fatalf("good=%d bad=%d, нужно по 3+", good, bad)
	}
}

type fakeJudge struct {
	fail   string
	rubric string
}

func (f fakeJudge) Name() string { return "judge" }

func (f fakeJudge) Score(_ context.Context, trace *eval.Trace) (eval.Score, error) {
	passed := !containsText(trace.FinalResponse, f.fail)
	value := 0.0
	if passed {
		value = 1
	}
	return eval.Score{Name: f.Name(), Value: value, Passed: passed, Details: map[string]any{"rubric": f.rubric}}, nil
}

func containsText(haystack, needle string) bool { return strings.Contains(haystack, needle) }
