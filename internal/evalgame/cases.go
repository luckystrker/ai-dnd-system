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

// Suite — загруженный набор: кейсы ago плюс метаданные для фикстур и отчётов.
type Suite struct {
	Cases eval.Suite
	Meta  map[string]CaseMeta // имя кейса → метаданные
	Files []string
}

// CaseMeta — то, что не влезает в eval.Case ago: кто пишет, какие права у
// говорящего, что проверять в БД после хода и подмена результата тула.
type CaseMeta struct {
	Category string
	// Speaker — автор сообщения, чей ход запускает кейс (Telegram-имя или
	// имя персонажа из текста). В слайсе 7 по нему собирается фикстура auth.
	Speaker string
	// Role — права говорящего: player (обычный игрок) или dm.
	Role string
	// Postconditions — проверки состояния БД после хода; исполняются в
	// слайсе 7, пока только схема и валидация.
	Postconditions []Postcondition
	// Fault — подмена результата тула ошибкой (для кейса ретрая).
	Fault *Fault
	// SkipUntil — слайс, до которого кейс не считается в наборе.
	SkipUntil string
}

// Postcondition — декларативная проверка состояния БД после хода.
//
//	{"type":"day_summary","day":"closed","fields":["summary","headline"],"expect":"nonempty"}
type Postcondition struct {
	Type   string   `json:"type"`
	Day    string   `json:"day,omitempty"`
	Fields []string `json:"fields,omitempty"`
	Expect string   `json:"expect,omitempty"`
}

// Fault — на N-й вызов тула вернуть ошибку вместо настоящего результата.
type Fault struct {
	Tool  string `json:"tool"`
	Call  int    `json:"call"`
	Error string `json:"error"`
}

// Names возвращает имена кейсов в порядке набора.
func (s Suite) Names() []string {
	names := make([]string, 0, len(s.Cases))
	for _, c := range s.Cases {
		names = append(names, c.Name)
	}
	return names
}

// Active возвращает кейсы без skip_until: только они считаются в наборе.
func (s Suite) Active() eval.Suite {
	active := make(eval.Suite, 0, len(s.Cases))
	for _, c := range s.Cases {
		if s.Meta[c.Name].SkipUntil != "" {
			continue
		}
		active = append(active, c)
	}
	return active
}

// Skipped возвращает имена отложенных кейсов.
func (s Suite) Skipped() []string {
	var skipped []string
	for _, c := range s.Cases {
		if s.Meta[c.Name].SkipUntil != "" {
			skipped = append(skipped, c.Name)
		}
	}
	return skipped
}

// MetaFor возвращает метаданные кейса (пустые, если имени нет).
func (s Suite) MetaFor(name string) CaseMeta { return s.Meta[name] }

// CountByCategory считает активные кейсы по категориям.
func (s Suite) CountByCategory() map[string]int {
	counts := map[string]int{}
	for _, c := range s.Cases {
		meta := s.Meta[c.Name]
		if meta.SkipUntil != "" {
			continue
		}
		counts[meta.Category]++
	}
	return counts
}

// rawCase — строка JSONL. Формат совместим с eval.SuiteFromJSONL ago
// (name/input/expected/scorers) плюс обязательные category/speaker/role.
type rawCase struct {
	Name           string            `json:"name"`
	Input          string            `json:"input"`
	Expected       string            `json:"expected"`
	Category       string            `json:"category"`
	Speaker        string            `json:"speaker"`
	Role           string            `json:"role"`
	Scorers        []eval.ScorerSpec `json:"scorers"`
	Postconditions []Postcondition   `json:"postconditions,omitempty"`
	Fault          *Fault            `json:"fault,omitempty"`
	SkipUntil      string            `json:"skip_until,omitempty"`
}

// LoadSuite читает все *.jsonl из каталога в алфавитном порядке файлов и
// строк. Требования набора: уникальное имя, непустой input, категория,
// говорящий и его роль, хотя бы один скорер; неизвестные поля — ошибка
// (опечатка в датасете не должна молча проглатываться).
func LoadSuite(dir string, registry *eval.ScorerRegistry) (Suite, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return Suite{}, fmt.Errorf("evalgame: шаблон %s: %w", dir, err)
	}
	if len(files) == 0 {
		return Suite{}, fmt.Errorf("evalgame: в %s нет *.jsonl", dir)
	}
	sort.Strings(files)

	suite := Suite{Meta: map[string]CaseMeta{}, Files: files}
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
			suite.Meta[c.Name] = c.meta
		}
	}
	return suite, nil
}

type loadedCase struct {
	eval.Case
	meta CaseMeta
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
	if strings.TrimSpace(raw.Speaker) == "" {
		return loadedCase{}, fmt.Errorf("%s: кейс %q без speaker", where, raw.Name)
	}
	if raw.Role != "player" && raw.Role != "dm" {
		return loadedCase{}, fmt.Errorf("%s: кейс %q: role должен быть player или dm, получено %q", where, raw.Name, raw.Role)
	}
	if len(raw.Scorers) == 0 {
		return loadedCase{}, fmt.Errorf("%s: кейс %q без скореров", where, raw.Name)
	}
	if err := validatePostconditions(raw.Postconditions); err != nil {
		return loadedCase{}, fmt.Errorf("%s: кейс %q: %w", where, raw.Name, err)
	}
	if err := validateFault(raw.Fault); err != nil {
		return loadedCase{}, fmt.Errorf("%s: кейс %q: %w", where, raw.Name, err)
	}
	if raw.SkipUntil != "" && !strings.HasPrefix(raw.SkipUntil, "slice-") {
		return loadedCase{}, fmt.Errorf("%s: кейс %q: skip_until должен быть вида slice-N, получено %q", where, raw.Name, raw.SkipUntil)
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
		meta: CaseMeta{
			Category:       raw.Category,
			Speaker:        raw.Speaker,
			Role:           raw.Role,
			Postconditions: raw.Postconditions,
			Fault:          raw.Fault,
			SkipUntil:      raw.SkipUntil,
		},
	}, nil
}

// PostconditionTypes — типы постусловных проверок, которые поймёт слайс 7.
var PostconditionTypes = []string{"day_summary"}

func validatePostconditions(items []Postcondition) error {
	for _, item := range items {
		if !contains(PostconditionTypes, item.Type) {
			return fmt.Errorf("неизвестный тип postcondition %q (доступны: %s)",
				item.Type, strings.Join(PostconditionTypes, ", "))
		}
		switch item.Type {
		case "day_summary":
			if item.Day != "closed" {
				return fmt.Errorf("postcondition day_summary: day должен быть \"closed\", получено %q", item.Day)
			}
			if len(item.Fields) == 0 {
				return fmt.Errorf("postcondition day_summary: не заданы fields")
			}
			for _, field := range item.Fields {
				if field != "summary" && field != "headline" {
					return fmt.Errorf("postcondition day_summary: неизвестное поле %q (доступны: summary, headline)", field)
				}
			}
			if item.Expect != "nonempty" {
				return fmt.Errorf("postcondition day_summary: expect должен быть \"nonempty\", получено %q", item.Expect)
			}
		}
	}
	return nil
}

func validateFault(fault *Fault) error {
	if fault == nil {
		return nil
	}
	if strings.TrimSpace(fault.Tool) == "" {
		return fmt.Errorf("fault: не задан tool")
	}
	if fault.Call < 1 {
		return fmt.Errorf("fault: call должен быть >= 1, получено %d", fault.Call)
	}
	if strings.TrimSpace(fault.Error) == "" {
		return fmt.Errorf("fault: не задан error")
	}
	return nil
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// Validate проверяет состав набора: размер, покрытие категорий. Считаются
// только активные кейсы (без skip_until). Вынесено из LoadSuite, чтобы живой
// прогон мог отдельно решать, какой набор считать достаточным.
func (s Suite) Validate(minCases, maxCases int, requiredCategories []string) error {
	active := s.Active()
	if len(active) < minCases || len(active) > maxCases {
		return fmt.Errorf("evalgame: активных кейсов %d, ожидалось от %d до %d", len(active), minCases, maxCases)
	}
	counts := s.CountByCategory()
	for _, category := range requiredCategories {
		if counts[category] == 0 {
			return fmt.Errorf("evalgame: нет кейсов категории %q", category)
		}
	}
	for _, c := range active {
		if c.Name == "" || c.Input == "" || len(c.Scorers) == 0 {
			return fmt.Errorf("evalgame: некорректный кейс %+v", c)
		}
	}
	return nil
}
