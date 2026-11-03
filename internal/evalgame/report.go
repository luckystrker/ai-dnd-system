package evalgame

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luckystrker/ago/eval"

	"github.com/luckystriker/ai-dnd-system/internal/engine"
)

// DefaultSlice — слайс, на котором исполняется набор: сюда входят кейсы
// с skip_until: slice-7 (checks-05, fault-инжекция).
const DefaultSlice = 7

// PostconditionResult — результат одной проверки БД после хода.
type PostconditionResult struct {
	Postcondition
	Passed  bool           `json:"passed"`
	Details map[string]any `json:"details,omitempty"`
}

// RepeatReport — один прогон кейса.
type RepeatReport struct {
	Variant string       `json:"variant,omitempty"`
	Passed  bool         `json:"passed"`
	Output  string       `json:"output,omitempty"`
	Err     string       `json:"err,omitempty"`
	Scores  []eval.Score `json:"scores"`
	// ToolCalls — трасса тулов без событий: по ней видно, что модель звала и
	// что тул ответил, когда скорер упал.
	ToolCalls      []eval.ToolCall       `json:"tool_calls,omitempty"`
	Postconditions []PostconditionResult `json:"postconditions,omitempty"`
	DurationMS     int64                 `json:"duration_ms"`
	Tokens         int64                 `json:"tokens"`
	// QuoteAppends / QuoteParts — срабатывания гарантии публичных бросков
	// (PLAN 40): сколько раз код дописал цитату и какие компоненты добавил.
	// Пусто — модель процитировала бросок сама (или бросков не было).
	QuoteAppends int      `json:"quote_appends,omitempty"`
	QuoteParts   []string `json:"quote_parts,omitempty"`
}

// CaseReport — кейс, сведённый по всем повторам.
type CaseReport struct {
	Name       string         `json:"name"`
	Category   string         `json:"category"`
	Repeats    int            `json:"repeats"`
	Passes     int            `json:"passes"`
	PassAtK    bool           `json:"pass_at_k"`
	PassPowK   bool           `json:"pass_pow_k"`
	Runs       []RepeatReport `json:"runs"`
	Regression bool           `json:"regression,omitempty"`
	// QuoteAppends / QuoteParts — сумма срабатываний гарантии цитат по
	// повторам кейса (PLAN 40).
	QuoteAppends int      `json:"quote_appends,omitempty"`
	QuoteParts   []string `json:"quote_parts,omitempty"`
}

// CategorySummary — pass rate категории.
type CategorySummary struct {
	Cases    int     `json:"cases"`
	Passed   int     `json:"passed"`
	PassRate float64 `json:"pass_rate"`
}

// Report — результат прогона: совместим с eval.RunResult через Run (первый
// повтор каждого кейса), плюс повторы, категории и постусловия.
type Report struct {
	StartedAt  time.Time                  `json:"started_at"`
	FinishedAt time.Time                  `json:"finished_at"`
	Slice      int                        `json:"slice"`
	Repeats    int                        `json:"repeats"`
	Cases      []CaseReport               `json:"cases"`
	Categories map[string]CategorySummary `json:"categories"`
	PassRate   float64                    `json:"pass_rate"`
	// Гарантия цитат бросков (PLAN 40): сколько раз код дописал цитату за
	// прогон, в скольких кейсах это было и какие компоненты добавлялись.
	QuoteAppends int      `json:"quote_appends,omitempty"`
	QuoteCases   int      `json:"quote_cases,omitempty"`
	QuoteParts   []string `json:"quote_parts,omitempty"`
	// Run — первое исполнение каждого кейса в формате ago: с ним работает
	// eval.Diff и регрессионный гейт.
	Run *eval.RunResult `json:"run"`
}

// RunResult возвращает совместимый с ago результат.
func (r *Report) RunResult() *eval.RunResult { return r.Run }

// Plan — кейс с необязательным вариантом (детерминированный rand).
type Plan struct {
	Base    string
	Name    string
	Variant string
	Random  engine.RandomSource
	Case    eval.Case
}

// ExpandVariants раскрывает кейс в один или несколько прогонов. rolls-05
// проверяет, что модель пересказывает результат тула, а не придумывает
// исход, поэтому идёт дважды — с натуральной 20 и с натуральной 1.
func ExpandVariants(c eval.Case) []Plan {
	base := Plan{Base: c.Name, Name: c.Name, Case: c}
	if c.Name != "rolls-05-narrate-per-tool-output" {
		return []Plan{base}
	}
	return []Plan{
		{Base: c.Name, Name: c.Name + "/nat-20", Variant: "nat-20", Random: ConstantRandom(0.9999), Case: c},
		{Base: c.Name, Name: c.Name + "/nat-1", Variant: "nat-1", Random: ConstantRandom(0.0), Case: c},
	}
}

// ConstantRandom — d20 всегда 20 (0.9999) или всегда 1 (0.0): SkillCheck
// бросает два d20, «добрый» d20 не перебрасывает максимум, а минимум
// остаётся минимумом даже после переброса.
func ConstantRandom(value float64) engine.RandomSource {
	return func() float64 { return value }
}

// DeterministicRandom — предсказуемый, но не вырожденный источник: 0.5 даёт
// d20 = 11 и не включает ни «добрый» переброс, ни серию низких бросков.
func DeterministicRandom() engine.RandomSource {
	return func() float64 { return 0.5 }
}

// BuildReport сводит повторы в кейсы, категории и eval.RunResult. metaFor
// возвращает метаданные base-кейса (вариант наследует категорию и
// постусловия).
func BuildReport(plans []Plan, results []RepeatReport, slice, repeats int, metaFor func(string) CaseMeta) *Report {
	report := &Report{
		StartedAt: time.Now(), Slice: slice, Repeats: repeats,
		Categories: map[string]CategorySummary{}, Run: &eval.RunResult{},
	}
	byName := map[string]*CaseReport{}
	order := []string{}
	for planIndex, plan := range plans {
		cr, exists := byName[plan.Name]
		if !exists {
			cr = &CaseReport{Name: plan.Name, Category: metaFor(plan.Base).Category, Repeats: repeats}
			byName[plan.Name] = cr
			order = append(order, plan.Name)
		}
		for repeat := 0; repeat < repeats; repeat++ {
			index := planIndex*repeats + repeat
			if index >= len(results) {
				break
			}
			run := results[index]
			cr.Runs = append(cr.Runs, run)
			cr.Repeats = len(cr.Runs)
			if run.Passed {
				cr.Passes++
			}
			cr.QuoteAppends += run.QuoteAppends
			cr.QuoteParts = mergeParts(cr.QuoteParts, run.QuoteParts)
			if repeat == 0 {
				report.Run.Cases = append(report.Run.Cases, eval.CaseResult{
					Name: plan.Name, Output: run.Output,
					Trace:  &eval.Trace{AgentName: "dm", FinalResponse: run.Output, Err: run.Err},
					Scores: run.Scores, Passed: run.Passed, Err: run.Err,
				})
			}
		}
		cr.PassAtK = cr.Passes > 0
		cr.PassPowK = cr.Passes == cr.Repeats
	}
	for _, name := range order {
		cr := byName[name]
		report.Cases = append(report.Cases, *cr)
		summary := report.Categories[cr.Category]
		summary.Cases++
		if cr.PassPowK {
			summary.Passed++
		}
		report.Categories[cr.Category] = summary
		report.QuoteAppends += cr.QuoteAppends
		if cr.QuoteAppends > 0 {
			report.QuoteCases++
		}
		report.QuoteParts = mergeParts(report.QuoteParts, cr.QuoteParts)
	}
	// Run.PassRate — первый повтор (с ним работает eval.Diff); PassRate
	// отчёта — кейс целиком: он прошёл, только если прошли все повторы.
	passedRuns := 0
	gradedRuns := 0
	for _, caseRun := range report.Run.Cases {
		gradedRuns++
		if caseRun.Passed {
			passedRuns++
		}
	}
	if gradedRuns > 0 {
		report.Run.PassRate = float64(passedRuns) / float64(gradedRuns)
	}
	if len(report.Cases) > 0 {
		passedCases := 0
		for _, item := range report.Cases {
			if item.PassPowK {
				passedCases++
			}
		}
		report.PassRate = float64(passedCases) / float64(len(report.Cases))
	}
	for category, summary := range report.Categories {
		if summary.Cases > 0 {
			summary.PassRate = float64(summary.Passed) / float64(summary.Cases)
		}
		report.Categories[category] = summary
	}
	report.FinishedAt = time.Now()
	return report
}

// mergeParts дополняет dst уникальными компонентами из src, сохраняя порядок:
// в отчёте компоненты гарантии цитат идут в том порядке, в каком их дописали.
func mergeParts(dst, src []string) []string {
	seen := make(map[string]bool, len(dst)+len(src))
	for _, part := range dst {
		seen[part] = true
	}
	for _, part := range src {
		if seen[part] {
			continue
		}
		seen[part] = true
		dst = append(dst, part)
	}
	return dst
}

// SaveReport пишет отчёт в JSON (родительские каталоги создаются).
func SaveReport(path string, report *Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("evalgame: сериализация отчёта: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("evalgame: каталог отчёта: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("evalgame: запись отчёта: %w", err)
	}
	return nil
}

// LoadReport читает отчёт. Если файл — голый eval.RunResult (например, снятый
// старым раннером), он заворачивается в минимальный Report.
func LoadReport(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report Report
	if err := json.Unmarshal(data, &report); err == nil && report.Run != nil {
		return &report, nil
	}
	run, runErr := eval.LoadJSON(path)
	if runErr != nil {
		return nil, fmt.Errorf("evalgame: %s не похож ни на отчёт, ни на eval.RunResult: %w", path, runErr)
	}
	return &Report{Run: run}, nil
}
