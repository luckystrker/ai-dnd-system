package evalgame

import (
	"os"
	"path/filepath"
	"testing"
)

const casesDir = "../../eval/cases"

var requiredCategories = []string{"agency", "rolls", "checks", "combat", "memory", "day", "neutrality", "group"}

func TestLoadSuite(t *testing.T) {
	t.Parallel()

	suite, err := LoadSuite(casesDir, NewRegistry(nil))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if err := suite.Validate(30, 50, requiredCategories); err != nil {
		t.Fatalf("набор не прошёл валидацию: %v", err)
	}
	if len(suite.Files) != len(requiredCategories) {
		t.Errorf("файлов %d, категорий %d", len(suite.Files), len(requiredCategories))
	}

	seen := map[string]bool{}
	withJudge := 0
	for _, c := range suite.Cases {
		if seen[c.Name] {
			t.Fatalf("дубль имени кейса: %s", c.Name)
		}
		seen[c.Name] = true
		if suite.Categories[c.Name] == "" {
			t.Errorf("кейс %s без категории", c.Name)
		}
		if len(c.Scorers) == 0 {
			t.Errorf("кейс %s без скореров", c.Name)
		}
		for _, scorer := range c.Scorers {
			if scorer.Name() == "judge" {
				withJudge++
			}
		}
	}
	// Смысловые правила проверяет только судья, поэтому он должен быть
	// почти в каждом кейсе (механические проверки — дополнение к нему).
	if withJudge < len(suite.Cases)-5 {
		t.Errorf("judge есть только в %d из %d кейсов", withJudge, len(suite.Cases))
	}

	counts := suite.CountByCategory()
	for _, category := range requiredCategories {
		if counts[category] == 0 {
			t.Errorf("категория %q пуста", category)
		}
	}
}

func TestLoadSuiteRejectsBadData(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
	}{
		{"без имени", `{"category":"agency","input":"x","scorers":[{"name":"no_tool_calls"}]}`},
		{"без input", `{"name":"a","category":"agency","scorers":[{"name":"no_tool_calls"}]}`},
		{"без категории", `{"name":"a","input":"x","scorers":[{"name":"no_tool_calls"}]}`},
		{"без скореров", `{"name":"a","category":"agency","input":"x"}`},
		{"неизвестный скорер", `{"name":"a","category":"agency","input":"x","scorers":[{"name":"nope"}]}`},
		{"опечатка в поле", `{"name":"a","category":"agency","inpyt":"x","scorers":[{"name":"no_tool_calls"}]}`},
		{"неизвестный параметр", `{"name":"a","category":"agency","input":"x","scorers":[{"name":"answer_omits","params":{"phrases":["y"],"typo":1}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "bad.jsonl"), []byte(tc.line+"\n"), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			if _, err := LoadSuite(dir, NewRegistry(nil)); err == nil {
				t.Fatalf("строка %q должна отклоняться", tc.line)
			}
		})
	}
}

func TestLoadSuiteRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	line := `{"name":"dup","category":"agency","input":"x","scorers":[{"name":"no_tool_calls"}]}` + "\n"
	for _, file := range []string{"a.jsonl", "b.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(line), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	sum, err := LoadSuite(dir, NewRegistry(nil))
	if err == nil {
		t.Fatalf("дубли имён должны отклоняться, загружено %d кейсов", len(sum.Cases))
	}
}

func TestLoadSuiteMissingDir(t *testing.T) {
	t.Parallel()

	if _, err := LoadSuite(filepath.Join(t.TempDir(), "nope"), NewRegistry(nil)); err == nil {
		t.Fatal("пустой каталог должен отклоняться")
	}
}
