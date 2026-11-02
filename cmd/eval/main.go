// Команда eval — живой прогон eval-набора DM-бота и регрессионный гейт.
//
// Живой прогон требует LLM_API_KEY (тот же, что у бота) и стоит денег: 43
// кейса × повторы, плюс судья и летописец на кейсах закрытия дня. Офлайн в CI
// гоняются `go test ./internal/evalgame` (скриптованные траектории) и
// `cmd/eval -check-only` (валидация набора без модели).
//
// Примеры:
//
//	cmd/eval -n 1 -out eval-results/last.json
//	cmd/eval -case rolls-05-narrate-per-tool-output -n 3
//	cmd/eval -baseline eval-results/baseline.json -gate
//	cmd/eval -check-only
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	agoconfig "github.com/luckystrker/ago/config"
	"github.com/luckystrker/ago/eval"

	"github.com/luckystriker/ai-dnd-system/internal/chronicler"
	"github.com/luckystriker/ai-dnd-system/internal/config"
	"github.com/luckystriker/ai-dnd-system/internal/dayclose"
	"github.com/luckystriker/ai-dnd-system/internal/evalgame"
	"github.com/luckystriker/ai-dnd-system/internal/evalrun"
	"github.com/luckystriker/ai-dnd-system/internal/llm"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	casesDir := fs.String("cases", "eval/cases", "каталог с *.jsonl кейсами")
	slice := fs.Int("slice", evalgame.DefaultSlice, "слайс набора (skip_until)")
	repeats := fs.Int("n", 1, "повторов на кейс (Pass@k / Pass^k)")
	concurrency := fs.Int("concurrency", evalrun.DefaultRepeatConcurrency, "параллельных прогонов")
	caseList := fs.String("case", "", "имена кейсов через запятую (по умолчанию все активные)")
	out := fs.String("out", "eval-results/last.json", "куда писать JSON-отчёт")
	baseline := fs.String("baseline", "", "прошлый отчёт для регрессионного гейта")
	gate := fs.Bool("gate", false, "включить абсолютный гейт и упасть на нарушениях")
	minPass := fs.Float64("min-pass", evalgame.ProposedGate().MinPassRate, "порог общего pass rate (для -gate)")
	categoryText := fs.String("category", "", "пороги категорий, например agency=0.7,memory=0.8")
	requiredText := fs.String("required", "", "обязательные кейсы через запятую (пусто — предложенный список)")
	noJudge := fs.Bool("no-judge", false, "не подключать LLM-судью (только механические скореры)")
	calibrate := fs.Bool("judge-calibration", true, "калибровать судью на вручную написанных ответах")
	calibrationPath := fs.String("calibration", "eval/judge/calibration.jsonl", "фикстуры калибровки судьи")
	checkOnly := fs.Bool("check-only", false, "только загрузить и проверить набор, без модели")
	timeout := fs.Duration("timeout", evalrun.DefaultTurnTimeout, "дедлайн одного хода")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var judgeClient agoconfig.LLMClient
	if !*noJudge && !*checkOnly {
		client, err := evalgame.NewJudgeClient(os.Getenv)
		if err != nil {
			return err
		}
		judgeClient = client
	}
	suite, err := evalgame.LoadSuite(*casesDir, evalgame.NewRegistry(judgeClient))
	if err != nil {
		return err
	}
	active := suite.ActiveAt(*slice)
	if err := suite.Validate(30, 60, []string{"agency", "rolls", "checks", "combat", "memory", "day", "neutrality", "group"}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "eval: кейсов %d (активных на слайсе %d — %d), файлов %d\n",
		len(suite.Cases), *slice, len(active), len(suite.Files))
	if *checkOnly {
		counts := suite.CountByCategoryAt(*slice)
		for _, category := range []string{"agency", "rolls", "checks", "combat", "memory", "day", "neutrality", "group"} {
			fmt.Fprintf(os.Stderr, "  %-11s %d\n", category, counts[category])
		}
		return nil
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	settings := cfg.LLM
	modelFactory := func(sessionID string) (agoconfig.ModelConfig, error) {
		model, _, err := settings.SessionConfig(sessionID).ModelConfig()
		return model, err
	}
	chroniclerModel, _, err := settings.SessionConfig(llm.ChroniclerSessionID).ModelConfig()
	if err != nil {
		return err
	}

	runner, err := evalrun.NewRunner(suite, modelFactory, evalrun.RunnerConfig{
		Repeats:     *repeats,
		Concurrency: *concurrency,
		Timeout:     *timeout,
		NewArchivist: func(st *store.Store) dayclose.ArchivistFactory {
			return func(campaign *store.Campaign) (dayclose.Archivist, error) {
				return chronicler.New(chronicler.Config{
					Model:      chroniclerModel,
					Store:      st,
					CampaignID: campaign.ID,
				})
			}
		},
	})
	if err != nil {
		return err
	}

	var names []string
	for _, name := range strings.Split(*caseList, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}

	started := time.Now()
	report, err := runner.Run(ctx, evalrun.RunOptions{Cases: names, Slice: *slice})
	if err != nil {
		return err
	}
	printReport(report, time.Since(started))

	if err := evalgame.SaveReport(*out, report); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nотчёт: %s\n", *out)

	failed := false
	if *calibrate && judgeClient != nil {
		items, err := evalgame.LoadCalibration(*calibrationPath)
		if err != nil {
			return err
		}
		calibration := evalgame.Calibrate(ctx, items, func(rubric string) eval.Scorer {
			return evalgame.Judge{Client: judgeClient, Rubric: rubric}
		})
		printCalibration(calibration)
		if !calibration.Passed {
			failed = true
			fmt.Fprintln(os.Stderr, "калибровка судьи не прошла: good должен проходить, bad — проваливаться")
		}
	}
	if *baseline != "" {
		previous, err := evalgame.LoadReport(*baseline)
		if err != nil {
			return err
		}
		violations := evalgame.RegressionViolations(report.RunResult(), previous.RunResult())
		if len(violations) > 0 {
			failed = true
			fmt.Fprintln(os.Stderr, "\nрегрессии:")
			for _, item := range violations {
				fmt.Fprintln(os.Stderr, "  ✗", item)
			}
		} else {
			fmt.Fprintf(os.Stderr, "\nрегрессий относительно %s нет\n", *baseline)
		}
	}
	if *gate {
		thresholds := evalgame.ProposedGate()
		thresholds.MinPassRate = *minPass
		parsed, err := evalgame.ParseCategoryThresholds(*categoryText)
		if err != nil {
			return err
		}
		if len(parsed) > 0 {
			thresholds.Categories = parsed
		}
		if trimmed := strings.TrimSpace(*requiredText); trimmed != "" {
			thresholds.Required = nil
			for _, name := range strings.Split(trimmed, ",") {
				if item := strings.TrimSpace(name); item != "" {
					thresholds.Required = append(thresholds.Required, item)
				}
			}
		}
		violations := thresholds.Violations(report)
		if len(violations) > 0 {
			failed = true
			fmt.Fprintln(os.Stderr, "\nгейт не пройден:")
			for _, item := range violations {
				fmt.Fprintln(os.Stderr, "  ✗", item)
			}
		} else {
			fmt.Fprintln(os.Stderr, "\nгейт пройден")
		}
	}
	if failed {
		return fmt.Errorf("прогон не прошёл гейт")
	}
	return nil
}

// printCalibration печатает таблицу калибровки судьи.
func printCalibration(report evalgame.CalibrationReport) {
	fmt.Fprintf(os.Stderr, "\n=== калибровка судьи: good %d/%d, bad %d/%d, точность %.0f%% ===\n",
		report.GoodPassed, report.GoodTotal, report.BadFailed, report.BadTotal, report.Accuracy*100)
	for _, item := range report.Items {
		mark := "ok  "
		if (item.Label == "good" && !item.Passed) || (item.Label == "bad" && item.Passed) {
			mark = "FAIL"
		}
		errText := ""
		if item.Err != "" {
			errText = " ошибка: " + item.Err
		}
		fmt.Fprintf(os.Stderr, "  %s %-6s %-22s score=%.2f%s\n", mark, item.Label, item.ID, item.Score, errText)
	}
}

// printReport печатает сводку по категориям и проваленные кейсы.
func printReport(report *evalgame.Report, elapsed time.Duration) {
	fmt.Fprintf(os.Stderr, "\n=== eval: %d кейсов, pass rate %.0f%% (%s) ===\n",
		len(report.Cases), report.PassRate*100, elapsed.Round(time.Second))
	categories := make([]string, 0, len(report.Categories))
	for category := range report.Categories {
		categories = append(categories, category)
	}
	sortStrings(categories)
	for _, category := range categories {
		summary := report.Categories[category]
		fmt.Fprintf(os.Stderr, "  %-11s %3d/%3d  %5.0f%%\n",
			category, summary.Passed, summary.Cases, summary.PassRate*100)
	}
	var failed []evalgame.CaseReport
	for _, item := range report.Cases {
		if !item.PassPowK {
			failed = append(failed, item)
		}
	}
	if len(failed) == 0 {
		fmt.Fprintln(os.Stderr, "\nвсе кейсы прошли")
		return
	}
	fmt.Fprintln(os.Stderr, "\nпровалы:")
	for _, item := range failed {
		reason := ""
		if len(item.Runs) > 0 {
			run := item.Runs[len(item.Runs)-1]
			reason = run.Err
			if reason == "" {
				reason = firstFailedScore(run)
			}
		}
		extra := ""
		if item.Repeats > 1 {
			extra = fmt.Sprintf(" [%d/%d повторов]", item.Passes, item.Repeats)
		}
		fmt.Fprintf(os.Stderr, "  ✗ %s%s: %s\n", item.Name, extra, reason)
	}
}

func firstFailedScore(run evalgame.RepeatReport) string {
	for _, score := range run.Scores {
		if score.Passed {
			continue
		}
		return fmt.Sprintf("скорер %s: %v", score.Name, score.Details)
	}
	for _, post := range run.Postconditions {
		if !post.Passed {
			return fmt.Sprintf("postcondition %s: %v", post.Type, post.Details)
		}
	}
	return "нет деталей"
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
