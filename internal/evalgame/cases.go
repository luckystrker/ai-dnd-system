package evalgame

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luckystrker/ago/eval"
)

// Suite — загруженный набор: кейсы ago плюс метаданные для отчётов.
type Suite struct {
	Cases      eval.Suite
	Categories map[string]string // имя кейса → категория
	Files      []string
}

// Names возвращает имена кейсов в порядке набора.
func (s Suite) Names() []string {
	names := make([]string, 0, len(s.Cases))
	for _, c := range s.Cases {
		names = append(names, c.Name)
	}
	return names
}

// CountByCategory считает кейсы по категориям.
func (s Suite) CountByCategory() map[string]int {
	counts := map[string]int{}
	for _, category := range s.Categories {
		counts[category]++
	}
	return counts
}

// rawCase — строка JSONL. Формат совместим с eval.SuiteFromJSONL ago
// (name/input/expected/scorers) плюс обязательная для набора category.
type rawCase struct {
	Name     string            `json:"name"`
	Input    string            `json:"input"`
	Expected string            `json:"expected"`
	Category string            `json:"category"`
	Scorers  []eval.ScorerSpec `json:"scorers"`
}

// LoadSuite читает все *.jsonl из каталога в алфавитном порядке файлов и
// строк. Требования набора: у кейса есть уникальное имя, непустой input,
// категория и хотя бы один скорер; неизвестные поля — ошибка (опечатка в
// датасете не должна молча проглатываться).
func LoadSuite(dir string, registry *eval.ScorerRegistry) (Suite, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return Suite{}, fmt.Errorf("evalgame: шаблон %s: %w", dir, err)
	}
	if len(files) == 0 {
		return Suite{}, fmt.Errorf("evalgame: в %s нет *.jsonl", dir)
	}
	sort.Strings(files)

	suite := Suite{Categories: map[string]string{}, Files: files}
	seen := map[string]string{} // имя кейса → файл
	for _, path := range files {
		cases, err := loadFile(path, registry)
		if err != nil {
			return Suite{}, err
		}
		for _, c := range cases {
			if previous, dup := seen[c.Name]; dup {
				return Suite{}, fmt.Errorf("evalgame: %s: кейс %q уже объявлен в %s", path, c.Name, previous)
			}
			seen[c.Name] = path
			suite.Cases = append(suite.Cases, c.Case)
			suite.Categories[c.Name] = c.category
		}
	}
	return suite, nil
}

type loadedCase struct {
	eval.Case
	category string
}

func loadFile(path string, registry *eval.ScorerRegistry) ([]loadedCase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("evalgame: открыть %s: %w", path, err)
	}
	defer file.Close()

	var cases []loadedCase
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimPrefix(strings.TrimSpace(scanner.Text()), "\ufeff")
		if text == "" {
			continue
		}
		c, err := parseCase(path, line, text, registry)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("evalgame: чтение %s: %w", path, err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("evalgame: %s пуст", path)
	}
	return cases, nil
}

func parseCase(path string, line int, text string, registry *eval.ScorerRegistry) (loadedCase, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	var raw rawCase
	if err := decoder.Decode(&raw); err != nil {
		return loadedCase{}, fmt.Errorf("evalgame: %s:%d: %w", path, line, err)
	}

	where := fmt.Sprintf("evalgame: %s:%d", path, line)
	if strings.TrimSpace(raw.Name) == "" {
		return loadedCase{}, fmt.Errorf("%s: не задано имя кейса", where)
	}
	if strings.TrimSpace(raw.Input) == "" {
		return loadedCase{}, fmt.Errorf("%s: кейс %q без input", where, raw.Name)
	}
	if strings.TrimSpace(raw.Category) == "" {
		return loadedCase{}, fmt.Errorf("%s: кейс %q без category", where, raw.Name)
	}
	if len(raw.Scorers) == 0 {
		return loadedCase{}, fmt.Errorf("%s: кейс %q без скореров", where, raw.Name)
	}

	scorers, err := registry.BuildScorers(raw.Scorers, eval.CaseScope{Expected: raw.Expected})
	if err != nil {
		return loadedCase{}, fmt.Errorf("%s: кейс %q: %w", where, raw.Name, err)
	}
	return loadedCase{
		Case: eval.Case{
			Name:     raw.Name,
			Input:    raw.Input,
			Expected: raw.Expected,
			Scorers:  scorers,
		},
		category: raw.Category,
	}, nil
}

// Validate проверяет состав набора: размер, уникальность имён, покрытие
// категорий. Вынесено из LoadSuite, чтобы живой прогон мог отдельно решать,
// какой набор считать достаточным.
func (s Suite) Validate(minCases, maxCases int, requiredCategories []string) error {
	if len(s.Cases) < minCases || len(s.Cases) > maxCases {
		return fmt.Errorf("evalgame: кейсов %d, ожидалось от %d до %d", len(s.Cases), minCases, maxCases)
	}
	counts := s.CountByCategory()
	for _, category := range requiredCategories {
		if counts[category] == 0 {
			return fmt.Errorf("evalgame: нет кейсов категории %q", category)
		}
	}
	for _, c := range s.Cases {
		if c.Name == "" || c.Input == "" || len(c.Scorers) == 0 {
			return fmt.Errorf("evalgame: некорректный кейс %+v", c)
		}
	}
	return nil
}
