package evalgame

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/luckystrker/ago/eval"
)

// Gate — абсолютный гейт качества: baseline на eve снять нельзя (TS-бот
// локально не запускается), поэтому порог задаётся по категориям плюс список
// обязательных кейсов. Значения DefaultGate утверждены владельцем (PLAN,
// решение 30); гейт включается флагом -gate, пороги переопределяются флагами.
type Gate struct {
	// MinPassRate — общий pass rate по кейсам (кейс считается пройденным,
	// только если прошли все его повторы).
	MinPassRate float64
	// Categories — минимальный pass rate по категориям.
	Categories map[string]float64
	// Required — кейсы, которые обязаны пройти. Сюда входят четыре
	// обязательных поведения: агентность, групповая запись в память,
	// закрытие дня и публичные броски. Проходят по отчёту повторов -n 3
	// (не меньше 2 из 3), а не по полному прогону (решение 46).
	Required []string
}

// DefaultGate — утверждённые пороги. Без -gate прогон ничего не блокирует.
func DefaultGate() Gate {
	return Gate{
		MinPassRate: 0.75,
		Categories: map[string]float64{
			"agency":     0.70,
			"rolls":      0.80,
			"checks":     0.60,
			"combat":     0.60,
			"memory":     0.80,
			"day":        0.80,
			"neutrality": 0.60,
			"group":      0.60,
		},
		Required: []string{
			// Агентность: DM ждёт решения второго игрока.
			"agency-03-group-help-wait",
			// Групповая запись в память на ходу обычного игрока.
			"memory-01-damage-on-player-turn",
			// Закрытие дня вместе с очередью летописца.
			"day-01-overnight-rest",
			// Публичные броски: числа тула процитированы.
			"rolls-01-skill-check-public",
		},
	}
}

// ParseCategoryThresholds разбирает "agency=0.7,memory=0.8".
func ParseCategoryThresholds(text string) (map[string]float64, error) {
	out := map[string]float64{}
	text = strings.TrimSpace(text)
	if text == "" {
		return out, nil
	}
	for _, item := range strings.Split(text, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("evalgame: порог категории %q: ожидается category=0.8", item)
		}
		name := strings.TrimSpace(parts[0])
		value, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil || value < 0 || value > 1 {
			return nil, fmt.Errorf("evalgame: порог категории %q: значение должно быть 0..1", item)
		}
		out[name] = value
	}
	return out, nil
}

// RequiredPasses / RequiredRepeats — правило решения владельца 46:
// обязательный кейс считается пройденным, если прошёл не меньше 2 из 3
// повторов. Категории и общий pass rate по-прежнему считаются по полному
// прогону -n 1.
const (
	RequiredPasses  = 2
	RequiredRepeats = 3
)

// Violations возвращает список нарушений гейта (пусто — гейт пройден).
// report — полный прогон -n 1 (общий pass rate и категории), repeats — отчёт
// -n 3 по обязательным кейсам. Без отчёта повторов гейт явно падает: тихого
// возврата к проверке обязательных кейсов по полному прогону нет.
func (g Gate) Violations(report, repeats *Report) []string {
	if report == nil || report.Run == nil {
		return []string{"гейт: нет результата прогона"}
	}
	var out []string
	if report.PassRate < g.MinPassRate {
		out = append(out, fmt.Sprintf("общий pass rate %.0f%% < %.0f%%",
			report.PassRate*100, g.MinPassRate*100))
	}
	// Голый eval.RunResult (старый/чужой файл) не содержит категорий и
	// pass rate по кейсам; без явной ошибки гейт рапортовал бы «нет кейсов»
	// по каждой категории, что выглядит как провал набора, а не файла.
	if len(g.Categories) > 0 && len(report.Categories) == 0 {
		return []string{"гейт: отчёт без категорий (голый eval.RunResult) — нужен полный отчёт раннера"}
	}
	categories := make([]string, 0, len(g.Categories))
	for category := range g.Categories {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		summary, exists := report.Categories[category]
		if !exists {
			out = append(out, fmt.Sprintf("категория %q: нет кейсов", category))
			continue
		}
		if summary.PassRate < g.Categories[category] {
			out = append(out, fmt.Sprintf("категория %q: %.0f%% < %.0f%% (%d/%d)",
				category, summary.PassRate*100, g.Categories[category]*100,
				summary.Passed, summary.Cases))
		}
	}
	if len(g.Required) == 0 {
		return out
	}
	if repeats == nil {
		out = append(out, "гейт: не передан отчёт повторов обязательных кейсов "+
			"(-required); нужен прогон -n 3 по обязательным кейсам")
		return out
	}
	byName := map[string]CaseReport{}
	for _, item := range repeats.Cases {
		byName[item.Name] = item
	}
	for _, name := range g.Required {
		item, exists := byName[name]
		if !exists {
			out = append(out, fmt.Sprintf("обязательный кейс %q нет в отчёте повторов (-required)", name))
			continue
		}
		if item.Repeats < RequiredRepeats {
			out = append(out, fmt.Sprintf("обязательный кейс %q: повторов %d, нужно не меньше %d (-n 3)",
				name, item.Repeats, RequiredRepeats))
			continue
		}
		// Пропорционально правилу «2 из 3»: на трёх повторах это ровно
		// два, на большем числе — не меньше двух третей.
		if item.Passes*RequiredRepeats < RequiredPasses*item.Repeats {
			out = append(out, fmt.Sprintf("обязательный кейс %q прошёл %d/%d повторов, нужно ≥%d из %d",
				name, item.Passes, item.Repeats, RequiredPasses, RequiredRepeats))
		}
	}
	return out
}

// RegressionViolations сравнивает текущий прогон с прошлым. Регрессией
// считается кейс, который проходил раньше и провалился сейчас; новые кейсы
// (их нет в baseline) регрессией не считаются.
func RegressionViolations(current, baseline *eval.RunResult) []string {
	if baseline == nil || current == nil {
		return nil
	}
	diff := eval.Diff(current, baseline)
	out := make([]string, 0, len(diff.Regressions))
	for _, name := range diff.Regressions {
		out = append(out, fmt.Sprintf("регрессия: %q проходил раньше, сейчас провален", name))
	}
	return out
}
