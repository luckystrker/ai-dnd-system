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
	if got := len(suite.Active()); got != len(suite.Cases)-1 {
		t.Errorf("активных кейсов %d из %d, ожидался один skip_until", got, len(suite.Cases))
	}
	if skipped := suite.Skipped(); len(skipped) != 1 || skipped[0] != "checks-05-tool-error-retry" {
		t.Errorf("отложенные кейсы = %v, ожидался checks-05-tool-error-retry", skipped)
	}

	seen := map[string]bool{}
	withJudge := 0
	for _, c := range suite.Cases {
		if seen[c.Name] {
			t.Fatalf("дубль имени кейса: %s", c.Name)
		}
		seen[c.Name] = true
		meta := suite.MetaFor(c.Name)
		if meta.Category == "" {
			t.Errorf("кейс %s без категории", c.Name)
		}
		if meta.Speaker == "" {
			t.Errorf("кейс %s без speaker", c.Name)
		}
		if meta.Role != "player" && meta.Role != "dm" {
			t.Errorf("кейс %s: role = %q", c.Name, meta.Role)
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

// Закрытие дня измеряется постусловием на БД, а не только вызовом advance_day:
// схема должна быть в наборе уже сейчас, исполнение — в слайсе 7.
func TestDayClosurePostconditions(t *testing.T) {
	t.Parallel()

	suite, err := LoadSuite(casesDir, NewRegistry(nil))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	for _, name := range []string{"day-01-overnight-rest", "day-02-time-wrap-to-morning"} {
		meta := suite.MetaFor(name)
		if len(meta.Postconditions) != 1 {
			t.Fatalf("кейс %s: postconditions = %v", name, meta.Postconditions)
		}
		got := meta.Postconditions[0]
		if got.Type != "day_summary" || got.Day != "closed" || got.Expect != "nonempty" {
			t.Fatalf("кейс %s: неожиданное postcondition %+v", name, got)
		}
		if len(got.Fields) != 2 {
			t.Fatalf("кейс %s: fields = %v", name, got.Fields)
		}
	}
}

func TestMemoryCasesArePlayerTurns(t *testing.T) {
	t.Parallel()

	suite, err := LoadSuite(casesDir, NewRegistry(nil))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	for _, c := range suite.Cases {
		if suite.MetaFor(c.Name).Category != "memory" {
			continue
		}
		meta := suite.MetaFor(c.Name)
		if meta.Role != "player" {
			t.Errorf("кейс %s: role = %q, want player (баг воспроизводится на ходу игрока)", c.Name, meta.Role)
		}
	}
}

func TestActiveAt(t *testing.T) {
	t.Parallel()

	suite, err := LoadSuite(casesDir, NewRegistry(nil))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if got := len(suite.ActiveAt(0)); got != len(suite.Cases)-1 {
		t.Errorf("ActiveAt(0) = %d, want %d", got, len(suite.Cases)-1)
	}
	if got := len(suite.ActiveAt(6)); got != len(suite.Cases)-1 {
		t.Errorf("ActiveAt(6) = %d, want %d (skip_until ещё не наступил)", got, len(suite.Cases)-1)
	}
	if got := len(suite.ActiveAt(7)); got != len(suite.Cases) {
		t.Errorf("ActiveAt(7) = %d, want %d (checks-05 включается)", got, len(suite.Cases))
	}
}

func TestSkippedCaseCarriesFault(t *testing.T) {
	t.Parallel()

	suite, err := LoadSuite(casesDir, NewRegistry(nil))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	meta := suite.MetaFor("checks-05-tool-error-retry")
	if meta.SkipUntil != "slice-7" {
		t.Fatalf("checks-05: skip_until = %q, want slice-7", meta.SkipUntil)
	}
	if meta.Fault == nil {
		t.Fatal("checks-05: не задан fault")
	}
	if meta.Fault.Tool != "skill_check" || meta.Fault.Call != 1 || meta.Fault.Error == "" {
		t.Fatalf("checks-05: неожиданный fault %+v", meta.Fault)
	}
}

func TestLoadSuiteRejectsBadData(t *testing.T) {
	t.Parallel()

	base := `"name":"a","category":"agency","speaker":"Дэн","role":"player","input":"Дэн: x"`
	cases := []struct {
		name string
		line string
	}{
		{"без имени", `{"category":"agency","speaker":"Дэн","role":"player","input":"x","scorers":[{"name":"no_tool_calls"}]}`},
		{"без input", `{"name":"a","category":"agency","speaker":"Дэн","role":"player","scorers":[{"name":"no_tool_calls"}]}`},
		{"без категории", `{"name":"a","speaker":"Дэн","role":"player","input":"x","scorers":[{"name":"no_tool_calls"}]}`},
		{"без speaker", `{"name":"a","category":"agency","role":"player","input":"x","scorers":[{"name":"no_tool_calls"}]}`},
		{"speaker не в input", `{"name":"a","category":"agency","speaker":"Дэн","role":"player","input":"Иду в таверну.","scorers":[{"name":"no_tool_calls"}]}`},
		{"плохая роль", `{"name":"a","category":"agency","speaker":"Дэн","role":"admin","input":"Дэн: x","scorers":[{"name":"no_tool_calls"}]}`},
		{"без скореров", `{` + base + `}`},
		{"неизвестный скорер", `{` + base + `,"scorers":[{"name":"nope"}]}`},
		{"опечатка в поле", `{` + base + `,"inpyt":"x","scorers":[{"name":"no_tool_calls"}]}`},
		{"неизвестный параметр", `{` + base + `,"scorers":[{"name":"answer_omits","params":{"phrases":["y"],"typo":1}}]}`},
		{"неизвестное postcondition", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"postconditions":[{"type":"magic","expect":"nonempty"}]}`},
		{"postcondition без полей", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"postconditions":[{"type":"day_summary","day":"closed","expect":"nonempty"}]}`},
		{"postcondition без expect", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"postconditions":[{"type":"day_summary","day":"closed","fields":["summary"]}]}`},
		{"fault без tool", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"fault":{"call":1,"error":"boom"}}`},
		{"fault с неизвестным тулом", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"fault":{"tool":"summon_dragon","call":1,"error":"boom"}}`},
		{"fault с call=0", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"fault":{"tool":"skill_check","call":0,"error":"boom"}}`},
		{"fault без error", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"fault":{"tool":"skill_check","call":1}}`},
		{"плохой skip_until", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"skip_until":"later"}`},
		{"skip_until без номера", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"skip_until":"slice-"}`},
		{"skip_until не число", `{` + base + `,"scorers":[{"name":"no_tool_calls"}],"skip_until":"slice-x"}`},
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
	line := `{"name":"dup","category":"agency","speaker":"Дэн","role":"player","input":"Дэн: x","scorers":[{"name":"no_tool_calls"}]}` + "\n"
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
