package evalgame

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luckystrker/ago/eval"
)

// CalibrationCase — вручную написанный ответ DM с известной оценкой: good
// должен пройти судью, bad — провалиться. Калибровка проверяет, что судья
// различает качество, а не соглашается со всем подряд.
type CalibrationCase struct {
	ID     string `json:"id"`
	Label  string `json:"label"` // good | bad
	Rubric string `json:"rubric"`
	Answer string `json:"answer"`
}

// CalibrationItem — результат одного ответа.
type CalibrationItem struct {
	CalibrationCase
	Score  float64 `json:"score"`
	Passed bool    `json:"passed"`
	Reason string  `json:"reason,omitempty"`
	Err    string  `json:"err,omitempty"`
}

// CalibrationReport — сводка калибровки.
type CalibrationReport struct {
	Items []CalibrationItem `json:"items"`
	// GoodPassed / GoodTotal и BadFailed / BadTotal — разделение классов.
	GoodPassed int     `json:"good_passed"`
	GoodTotal  int     `json:"good_total"`
	BadFailed  int     `json:"bad_failed"`
	BadTotal   int     `json:"bad_total"`
	Accuracy   float64 `json:"accuracy"`
	// Passed — все good прошли, все bad провалились.
	Passed bool `json:"passed"`
}

// LoadCalibration читает JSONL с ответами для калибровки судьи.
func LoadCalibration(path string) ([]CalibrationCase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("evalgame: калибровка %s: %w", path, err)
	}
	defer file.Close()

	var cases []CalibrationCase
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var item CalibrationCase
		if err := json.Unmarshal([]byte(text), &item); err != nil {
			return nil, fmt.Errorf("evalgame: %s:%d: %w", path, line, err)
		}
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Answer) == "" || strings.TrimSpace(item.Rubric) == "" {
			return nil, fmt.Errorf("evalgame: %s:%d: нужны непустые id, rubric и answer", path, line)
		}
		if item.Label != "good" && item.Label != "bad" {
			return nil, fmt.Errorf("evalgame: %s:%d: label должен быть good или bad, получено %q", path, line, item.Label)
		}
		cases = append(cases, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("evalgame: чтение %s: %w", path, err)
	}
	return cases, nil
}

// Calibrate прогоняет каждый ответ через scorer, собранный под рубрику
// элемента (обычно Judge), и считает разделение классов. Тулов в фикстурах
// нет, поэтому судья оценивает только текст ответа.
func Calibrate(ctx context.Context, cases []CalibrationCase, scorer func(rubric string) eval.Scorer) CalibrationReport {
	report := CalibrationReport{Items: make([]CalibrationItem, 0, len(cases))}
	for _, item := range cases {
		trace := &eval.Trace{FinalResponse: item.Answer, ToolCalls: []eval.ToolCall{}, LLMCalls: []eval.LLMCall{}}
		score := eval.ScoreWith(ctx, scorer(item.Rubric), trace)
		result := CalibrationItem{
			CalibrationCase: item,
			Score:           score.Value,
			Passed:          score.Passed,
		}
		if reason, ok := score.Details["reason"].(string); ok {
			result.Reason = reason
		}
		if errText, ok := score.Details["error"].(string); ok {
			result.Err = errText
			result.Passed = false
		}
		switch item.Label {
		case "good":
			report.GoodTotal++
			if result.Passed {
				report.GoodPassed++
			}
		case "bad":
			report.BadTotal++
			if !result.Passed {
				report.BadFailed++
			}
		}
		report.Items = append(report.Items, result)
	}
	total := report.GoodTotal + report.BadTotal
	correct := report.GoodPassed + report.BadFailed
	if total > 0 {
		report.Accuracy = float64(correct) / float64(total)
	}
	report.Passed = report.GoodTotal > 0 && report.BadTotal > 0 &&
		report.GoodPassed == report.GoodTotal && report.BadFailed == report.BadTotal
	return report
}

// CalibrationPath — путь к фикстурам калибровки внутри репозитория.
func CalibrationPath(repoRoot string) string {
	return filepath.Join(repoRoot, "eval", "judge", "calibration.jsonl")
}
