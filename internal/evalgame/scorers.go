// Package evalgame — регрессионный eval-набор DM-бота: набор сценариев в
// eval/cases/*.jsonl и механические скореры поверх ago/eval.
//
// Механические скореры проверяют то, что видно в трассе без модели:
// какие тулы вызваны, с какими аргументами и что из результата броска
// процитировано в ответе. Смысловые правила (агентность, нейтральность,
// темп группового чата) оценивает judge по рубрике — он требует LLM-клиента
// и работает только в живом прогоне.
package evalgame

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	agoconfig "github.com/luckystrker/ago/config"
	"github.com/luckystrker/ago/eval"
	"github.com/luckystrker/ago/events"
)

// NewRegistry собирает реестр скореров: встроенные скореры ago плюс
// механические скореры набора и judge. client может быть nil — тогда набор
// грузится и механические скореры работают, а judge честно падает при прогоне.
func NewRegistry(client agoconfig.LLMClient) *eval.ScorerRegistry {
	r := eval.NewScorerRegistry()
	mustRegister(r, "tools_not_called", buildToolsNotCalled)
	mustRegister(r, "tool_args", buildToolArgs)
	mustRegister(r, "tool_ok", buildToolOK)
	mustRegister(r, "tool_sequence", buildToolSequence)
	mustRegister(r, "no_tool_calls", buildNoToolCalls)
	mustRegister(r, "quotes_roll", buildQuotesRoll)
	mustRegister(r, "answer_omits", buildAnswerOmits)
	mustRegister(r, "judge", func(params map[string]any, _ eval.CaseScope) (eval.Scorer, error) {
		if err := checkKeys(params, "rubric"); err != nil {
			return nil, err
		}
		rubric, err := stringParam(params, "rubric")
		if err != nil {
			return nil, err
		}
		return Judge{Client: client, Rubric: rubric}, nil
	})
	return r
}

func mustRegister(r *eval.ScorerRegistry, name string, f eval.ScorerFactory) {
	if err := r.Register(name, f); err != nil {
		panic(fmt.Sprintf("evalgame: register scorer %q: %v", name, err))
	}
}

// boolScore — единый формат булевых скореров: 1/0 и Passed.
func boolScore(name string, passed bool, details map[string]any) eval.Score {
	value := 0.0
	if passed {
		value = 1
	}
	return eval.Score{Name: name, Value: value, Passed: passed, Details: details}
}

func toolCounts(t *eval.Trace) map[string]int {
	counts := make(map[string]int, len(t.ToolCalls))
	for _, call := range t.ToolCalls {
		counts[call.Name]++
	}
	return counts
}

// ToolsNotCalled падает, если модель вызвала любой из запрещённых тулов.
// Главный кейс — проверка навыка не через skill_check, а через roll_dice.
type ToolsNotCalled struct {
	Names []string
}

func (s ToolsNotCalled) Name() string { return "tools_not_called" }

func (s ToolsNotCalled) Score(_ context.Context, t *eval.Trace) (eval.Score, error) {
	counts := toolCounts(t)
	var called []string
	for _, name := range s.Names {
		if counts[name] > 0 {
			called = append(called, name)
		}
	}
	return boolScore(s.Name(), len(called) == 0, map[string]any{
		"forbidden_called": called,
		"counts":           counts,
	}), nil
}

func buildToolsNotCalled(params map[string]any, _ eval.CaseScope) (eval.Scorer, error) {
	if err := checkKeys(params, "names"); err != nil {
		return nil, err
	}
	names, err := stringsParam(params, "names")
	if err != nil {
		return nil, err
	}
	return ToolsNotCalled{Names: names}, nil
}

// ToolOK проверяет, что совпавший вызов тула реально отработал: без Err и
// без маркеров отказа в результате. Закрывает баг групповой записи в память:
// на ходу обычного игрока combat/update_character вызывается, но тул отвечает
// «доступно только администратору», и запись не происходит — tools_called и
// tool_args при этом проходят.
//
// Args (необязательно) сужает проверку до конкретного вызова, как в ToolArgs.
type ToolOK struct {
	Tool string
	Args map[string]any
}

func (s ToolOK) Name() string { return "tool_ok" }

// plainRefusalMarkers — точные фразы авторизации в НЕ-JSON результате тула.
// TS-тулы (до слайса 8 — `agent/tools/combat.ts`), которые отказывают по
// правам, возвращали plain-строку (access.reason); успешные результаты —
// JSON-объекты, поэтому общие слова вроде «ошибка» или «запрещено» здесь не
// ищем: они бывают обычным текстом в описании NPC/квеста.
var plainRefusalMarkers = []string{
	"доступно только администратору",
	"принадлежит другому игроку",
	"не участник этой кампании",
	"не удалось определить, кто вы",
	"нет привязанной кампании",
	"действие от имени этого персонажа запрещено",
	"access_denied",
	"permission denied",
}

func (s ToolOK) Score(_ context.Context, t *eval.Trace) (eval.Score, error) {
	details := map[string]any{"tool": s.Tool}
	var refused []string
	checked := 0
	for _, call := range t.ToolCalls {
		if call.Name != s.Tool {
			continue
		}
		if len(s.Args) > 0 && !argumentsContain(call.Arguments, s.Args) {
			continue
		}
		checked++
		if call.Err != "" {
			refused = append(refused, "Err: "+truncate(call.Err, 200))
			continue
		}
		if marker := refusalMarker(call.Result); marker != "" {
			refused = append(refused, fmt.Sprintf("отказ в результате (%q): %s", marker, truncate(call.Result, 200)))
			continue
		}
		details["checked"] = checked
		details["result"] = truncate(call.Result, 300)
		return boolScore(s.Name(), true, details), nil
	}
	details["checked"] = checked
	if len(refused) > 0 {
		details["refused"] = refused
	}
	if checked == 0 {
		details["reason"] = "подходящий вызов тула не найден"
	}
	return boolScore(s.Name(), false, details), nil
}

// refusalMarker определяет отказ: JSON-результат — только ok:false или
// непустое поле error; plain-строка — точные фразы авторизации.
func refusalMarker(result string) string {
	trimmed := strings.TrimSpace(result)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var payload any
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			return jsonRefusalMarker(payload)
		}
	}
	lower := strings.ToLower(result)
	for _, marker := range plainRefusalMarkers {
		if strings.Contains(lower, marker) {
			return marker
		}
	}
	return ""
}

// jsonRefusalMarker смотрит только структурные признаки отказа, не текст:
// успешный upsert_npc/create_quest может эхом вернуть «запрещено» из заметки.
func jsonRefusalMarker(payload any) string {
	object, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if raw, exists := object["ok"]; exists {
		if value, isBool := raw.(bool); isBool && !value {
			return `"ok":false`
		}
	}
	if raw, exists := object["error"]; exists && raw != nil {
		switch value := raw.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return "error: " + truncate(value, 120)
			}
		default:
			return "error"
		}
	}
	return ""
}

// argumentsContain разбирает аргументы вызова и проверяет подмножество.
func argumentsContain(arguments string, expected map[string]any) bool {
	var actual any
	if strings.TrimSpace(arguments) == "" {
		actual = map[string]any{}
	} else if err := json.Unmarshal([]byte(arguments), &actual); err != nil {
		return false
	}
	return jsonSubset(expected, actual)
}

func buildToolOK(params map[string]any, _ eval.CaseScope) (eval.Scorer, error) {
	if err := checkKeys(params, "tool", "args"); err != nil {
		return nil, err
	}
	tool, err := stringParam(params, "tool")
	if err != nil {
		return nil, err
	}
	var args map[string]any
	if raw, ok := params["args"]; ok {
		if args, ok = normalizeJSON(raw).(map[string]any); !ok {
			return nil, fmt.Errorf("параметр \"args\" должен быть объектом")
		}
	}
	return ToolOK{Tool: tool, Args: args}, nil
}

// ToolArgs проверяет аргументы вызова тула: ожидаемое значение должно быть
// подмножеством фактического JSON (объекты — по ключам, массивы — «каждый
// ожидаемый элемент находит себе пару», строка "*" — любой скаляр). Строки
// сравниваются без учёта регистра и пробелов по краям.
//
// Contains проверяет подстроку в строковом аргументе (например, нотацию
// броска: notation содержит "1d20").
//
// Match: "any" (по умолчанию) — подходит хотя бы один вызов тула,
// "all" — каждый вызов тула должен подойти.
type ToolArgs struct {
	Tool     string
	Args     map[string]any
	Contains map[string]string
	Match    string
}

func (s ToolArgs) Name() string { return "tool_args" }

func (s ToolArgs) Score(_ context.Context, t *eval.Trace) (eval.Score, error) {
	var calls []eval.ToolCall
	for _, call := range t.ToolCalls {
		if call.Name == s.Tool {
			calls = append(calls, call)
		}
	}
	details := map[string]any{"tool": s.Tool, "calls": len(calls), "match": s.Match}
	if len(calls) == 0 {
		details["reason"] = "тул не вызывался"
		return boolScore(s.Name(), false, details), nil
	}

	matched := 0
	var problems []string
	for i, call := range calls {
		ok, problem := s.matches(call.Arguments)
		if ok {
			matched++
			continue
		}
		problems = append(problems, fmt.Sprintf("вызов %d: %s", i+1, problem))
	}
	details["matched"] = matched
	if len(problems) > 0 {
		details["problems"] = problems
	}
	passed := matched > 0
	if s.Match == "all" {
		passed = matched == len(calls)
	}
	return boolScore(s.Name(), passed, details), nil
}

func (s ToolArgs) matches(arguments string) (bool, string) {
	var actual any
	if strings.TrimSpace(arguments) == "" {
		actual = map[string]any{}
	} else if err := json.Unmarshal([]byte(arguments), &actual); err != nil {
		return false, fmt.Sprintf("аргументы не JSON: %v", err)
	}
	if len(s.Args) > 0 && !jsonSubset(s.Args, actual) {
		return false, fmt.Sprintf("ожидалось %s, получено %s", compactJSON(s.Args), truncate(arguments, 300))
	}
	for key, want := range s.Contains {
		obj, ok := actual.(map[string]any)
		if !ok {
			return false, "аргументы не объект"
		}
		got, ok := obj[key].(string)
		if !ok {
			return false, fmt.Sprintf("аргумент %q не строка", key)
		}
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			return false, fmt.Sprintf("аргумент %q = %q, ожидалась подстрока %q", key, got, want)
		}
	}
	return true, ""
}

func buildToolArgs(params map[string]any, _ eval.CaseScope) (eval.Scorer, error) {
	if err := checkKeys(params, "tool", "args", "contains", "match"); err != nil {
		return nil, err
	}
	tool, err := stringParam(params, "tool")
	if err != nil {
		return nil, err
	}
	match := "any"
	if raw, ok := params["match"]; ok {
		value, isString := raw.(string)
		if !isString || (value != "any" && value != "all") {
			return nil, fmt.Errorf("параметр \"match\" должен быть \"any\" или \"all\"")
		}
		match = value
	}
	var args map[string]any
	if raw, ok := params["args"]; ok {
		if args, ok = normalizeJSON(raw).(map[string]any); !ok {
			return nil, fmt.Errorf("параметр \"args\" должен быть объектом")
		}
	}
	contains := map[string]string{}
	if raw, ok := params["contains"]; ok {
		object, isObject := raw.(map[string]any)
		if !isObject {
			return nil, fmt.Errorf("параметр \"contains\" должен быть объектом")
		}
		for key, value := range object {
			text, isString := value.(string)
			if !isString {
				return nil, fmt.Errorf("параметр \"contains\".%s должен быть строкой", key)
			}
			contains[key] = text
		}
	}
	if len(args) == 0 && len(contains) == 0 {
		return nil, fmt.Errorf("tool_args без \"args\" и \"contains\" ничего не проверяет")
	}
	return ToolArgs{Tool: tool, Args: args, Contains: contains, Match: match}, nil
}

// ToolSequence проверяет порядок вызовов: имена должны встретиться в трассе
// в указанном порядке (между ними допустимы другие тулы).
type ToolSequence struct {
	Names []string
}

func (s ToolSequence) Name() string { return "tool_sequence" }

func (s ToolSequence) Score(_ context.Context, t *eval.Trace) (eval.Score, error) {
	got := make([]string, 0, len(t.ToolCalls))
	next := 0
	for _, call := range t.ToolCalls {
		got = append(got, call.Name)
		if next < len(s.Names) && call.Name == s.Names[next] {
			next++
		}
	}
	return boolScore(s.Name(), next == len(s.Names), map[string]any{
		"expected": s.Names,
		"calls":    got,
		"matched":  next,
	}), nil
}

func buildToolSequence(params map[string]any, _ eval.CaseScope) (eval.Scorer, error) {
	if err := checkKeys(params, "names"); err != nil {
		return nil, err
	}
	names, err := stringsParam(params, "names")
	if err != nil {
		return nil, err
	}
	if len(names) < 2 {
		return nil, fmt.Errorf("tool_sequence требует минимум два имени")
	}
	return ToolSequence{Names: names}, nil
}

// NoToolCalls падает, если модель вызвала хоть один тул. Нужен для разговоров
// игроков между собой и внеигровых вопросов: там тулов быть не должно.
type NoToolCalls struct{}

func (NoToolCalls) Name() string { return "no_tool_calls" }

func (NoToolCalls) Score(_ context.Context, t *eval.Trace) (eval.Score, error) {
	names := make([]string, 0, len(t.ToolCalls))
	for _, call := range t.ToolCalls {
		names = append(names, call.Name)
	}
	return boolScore("no_tool_calls", len(names) == 0, map[string]any{"calls": names}), nil
}

func buildNoToolCalls(params map[string]any, _ eval.CaseScope) (eval.Scorer, error) {
	if err := checkKeys(params); err != nil {
		return nil, err
	}
	return NoToolCalls{}, nil
}

// QuotesRoll проверяет правило «броски публичны»: числа из результата
// броскового тула должны быть произнесены в ответе игрокам.
//
//	skill_check  — d20, итог и DC;
//	roll_dice    — «Итого»;
//	initiative   — итог инициативы каждого участника;
//	combat       — выпавший d20 атаки.
type QuotesRoll struct {
	Tools []string
}

// DefaultRollTools — тулы, результаты которых правила требуют цитировать.
var DefaultRollTools = []string{"skill_check", "roll_dice", "initiative", "combat"}

func (s QuotesRoll) Name() string { return "quotes_roll" }

func (s QuotesRoll) Score(_ context.Context, t *eval.Trace) (eval.Score, error) {
	tools := s.Tools
	if len(tools) == 0 {
		tools = DefaultRollTools
	}
	allowed := make(map[string]bool, len(tools))
	for _, name := range tools {
		allowed[name] = true
	}

	checked := 0
	var missing []string
	var wrong []string
	for _, call := range t.ToolCalls {
		if !allowed[call.Name] || call.Err != "" {
			continue
		}
		checked++
		if expects := rollAnchors(call.Name, call.Result); len(expects) > 0 {
			callMissing, callWrong := checkAnchors(answerWithoutMarkdown(t.FinalResponse), expects)
			for _, value := range callMissing {
				missing = append(missing, call.Name+": "+value)
			}
			for _, value := range callWrong {
				wrong = append(wrong, call.Name+": "+value)
			}
			continue
		}
		// Для тулов без якорей (инициатива) достаточно, чтобы суммы
		// участников были произнесены.
		for _, number := range rollNumbers(call.Name, call.Result) {
			if !mentionsNumber(answerWithoutMarkdown(t.FinalResponse), number) {
				missing = append(missing, fmt.Sprintf("%s: %s", call.Name, number))
			}
		}
	}

	details := map[string]any{"tools": tools, "checked_calls": checked}
	if len(missing) > 0 {
		details["missing_numbers"] = missing
	}
	if len(wrong) > 0 {
		details["wrong_numbers"] = wrong
	}
	if checked == 0 {
		details["reason"] = "не было вызовов бросковых тулов"
		return boolScore(s.Name(), false, details), nil
	}
	return boolScore(s.Name(), len(missing) == 0 && len(wrong) == 0, details), nil
}

func buildQuotesRoll(params map[string]any, _ eval.CaseScope) (eval.Scorer, error) {
	if err := checkKeys(params, "tools"); err != nil {
		return nil, err
	}
	tools := DefaultRollTools
	if _, ok := params["tools"]; ok {
		var err error
		if tools, err = stringsParam(params, "tools"); err != nil {
			return nil, err
		}
	}
	return QuotesRoll{Tools: tools}, nil
}

var (
	reD20      = regexp.MustCompile(`d20\s*=\s*(\d+)`)
	reSkillVs  = regexp.MustCompile(`=\s*(\d+)\s+vs\s+DC\s*(\d+)`)
	reTotal    = regexp.MustCompile(`Итого:\s*(\d+)`)
	reInitLine = regexp.MustCompile(`(\d+)\s*=\s*(\d+)\s*\+\s*(-?\d+)`)
	reAC       = regexp.MustCompile(`(?i)(?:кд|ac)\s*(\d+)`)
)

// reAnswer* — формы, в которых живой ответ называет число броска. Одних
// якорей «d20/итог/DC» мало: нормальные ответы пишут «выпало 7», «d20 = 7,
// +3 = 10» или «с бонусом 10», и это не должно считаться выдумкой.
var (
	reAnswerD20After  = regexp.MustCompile(`(?i)d20\s*[=:]?\s*(\d+)`)
	reAnswerD20Rolled = regexp.MustCompile(`(?i)выпало\s*(\d+)`)
	reAnswerD20Before = regexp.MustCompile(`(\d+)\s+на\s+d20`)
	reAnswerSumWord   = regexp.MustCompile(`(?i)(?:итог[а-яё]*|всего|сумма|total)\D{0,15}(\d+)`)
	reAnswerEquation  = regexp.MustCompile(`=\s*(\d+)`)
	reAnswerBonus     = regexp.MustCompile(`(?i)бонус[а-яё]*\s*(\d+)`)
	reAnswerVsDC      = regexp.MustCompile(`(\d+)\s+против\s+(?:dc|кд)`)
	reAnswerDC        = regexp.MustCompile(`(?i)(?:dc|кд|сл|сложност[а-яё]*)\s*[=:]?\s*(\d+)`)
	reAnswerRolled    = regexp.MustCompile(`(?i)(?:итог[а-яё]*|всего|сумма|total|выпало)\D{0,15}(\d+)`)
)

type anchorPattern struct {
	re    *regexp.Regexp
	group int
}

// anchorExpect — число, которое ответ должен подтвердить одной из форм.
type anchorExpect struct {
	Label    string
	Number   string
	Patterns []anchorPattern
}

func pat(re *regexp.Regexp) anchorPattern { return anchorPattern{re: re, group: 1} }

var (
	d20Patterns  = []anchorPattern{pat(reAnswerD20After), pat(reAnswerD20Rolled), pat(reAnswerD20Before)}
	sumPatterns  = []anchorPattern{pat(reAnswerSumWord), pat(reAnswerEquation), pat(reAnswerBonus), pat(reAnswerVsDC)}
	dcPatterns   = []anchorPattern{pat(reAnswerDC)}
	rollPatterns = []anchorPattern{pat(reAnswerRolled), pat(reAnswerEquation)}
)

// rollAnchors возвращает ожидаемые пары «что → число» из результата тула.
func rollAnchors(tool, result string) []anchorExpect {
	switch tool {
	case "skill_check":
		var out []anchorExpect
		if match := reD20.FindStringSubmatch(result); match != nil {
			out = append(out, anchorExpect{Label: "d20", Number: match[1], Patterns: d20Patterns})
		}
		if match := reSkillVs.FindStringSubmatch(result); match != nil {
			out = append(out,
				anchorExpect{Label: "итог", Number: match[1], Patterns: sumPatterns},
				anchorExpect{Label: "DC", Number: match[2], Patterns: dcPatterns},
			)
		}
		return out
	case "roll_dice":
		if match := reTotal.FindStringSubmatch(result); match != nil {
			return []anchorExpect{{Label: "итог", Number: match[1], Patterns: rollPatterns}}
		}
	case "combat":
		var out []anchorExpect
		if match := reD20.FindStringSubmatch(result); match != nil {
			out = append(out, anchorExpect{Label: "d20", Number: match[1], Patterns: d20Patterns})
		}
		if match := reAC.FindStringSubmatch(result); match != nil {
			out = append(out, anchorExpect{Label: "КД", Number: match[1], Patterns: dcPatterns})
		}
		return out
	}
	return nil
}

// checkAnchors сверяет ответ с результатом: число должно встретиться хотя бы
// в одной из форм. «Выдуманным» ответ считается, только если ни одно из
// найденных чисел не совпало с ожидаемым, — иначе «В итоге ... 2 минуты»
// рядом с корректным «итог 10» давало бы ложный fail.
func checkAnchors(answer string, expects []anchorExpect) (missing, wrong []string) {
	for _, expect := range expects {
		candidates := anchorCandidates(answer, expect.Patterns)
		if len(candidates) == 0 {
			missing = append(missing, fmt.Sprintf("%s %s", expect.Label, expect.Number))
			continue
		}
		if contains(candidates, expect.Number) {
			continue
		}
		wrong = append(wrong, fmt.Sprintf("%s: ожидалось %s, в ответе %s",
			expect.Label, expect.Number, strings.Join(candidates, ", ")))
	}
	return missing, wrong
}

func anchorCandidates(answer string, patterns []anchorPattern) []string {
	var out []string
	for _, pattern := range patterns {
		for _, match := range pattern.re.FindAllStringSubmatch(answer, -1) {
			if len(match) > pattern.group {
				out = append(out, match[pattern.group])
			}
		}
	}
	return dedupe(out)
}

// rollNumbers возвращает числа из результата тула без якорей (инициатива):
// правило требует назвать суммы участников.
func rollNumbers(tool, result string) []string {
	var out []string
	switch tool {
	case "skill_check":
		for _, match := range reD20.FindAllStringSubmatch(result, -1) {
			out = append(out, match[1])
		}
		for _, match := range reSkillVs.FindAllStringSubmatch(result, -1) {
			out = append(out, match[1], match[2])
		}
	case "roll_dice":
		for _, match := range reTotal.FindAllStringSubmatch(result, -1) {
			out = append(out, match[1])
		}
	case "initiative":
		// Строки вида «15 = 12 + 3»: правило требует назвать суммы
		// участников (d20 инициативы можно не пересказывать).
		for _, match := range reInitLine.FindAllStringSubmatch(result, -1) {
			out = append(out, match[1])
		}
	case "combat":
		for _, match := range reD20.FindAllStringSubmatch(result, -1) {
			out = append(out, match[1])
		}
	}
	return dedupe(out)
}

// answerWithoutMarkdown убирает markdown-выделение из чисел: живая модель
// пишет «d20 = **20**», и без нормализации число не находится, хотя оно
// процитировано (ложный провал quotes_roll).
func answerWithoutMarkdown(answer string) string {
	return strings.NewReplacer("*", "", "`", "").Replace(answer)
}

func mentionsNumber(answer, number string) bool {
	re := regexp.MustCompile(`(?:^|[^0-9])` + regexp.QuoteMeta(number) + `(?:[^0-9]|$)`)
	return re.MatchString(answer)
}

func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

// AnswerOmits падает, если в ответе есть любая из запрещённых фраз
// (регистронезависимо). Для штампов вроде «ты чувствуешь страх» или
// «этот вариант идеально подходит».
type AnswerOmits struct {
	Phrases []string
}

func (s AnswerOmits) Name() string { return "answer_omits" }

func (s AnswerOmits) Score(_ context.Context, t *eval.Trace) (eval.Score, error) {
	answer := strings.ToLower(t.FinalResponse)
	var found []string
	for _, phrase := range s.Phrases {
		if strings.Contains(answer, strings.ToLower(phrase)) {
			found = append(found, phrase)
		}
	}
	return boolScore(s.Name(), len(found) == 0, map[string]any{"found": found}), nil
}

func buildAnswerOmits(params map[string]any, _ eval.CaseScope) (eval.Scorer, error) {
	if err := checkKeys(params, "phrases"); err != nil {
		return nil, err
	}
	phrases, err := stringsParam(params, "phrases")
	if err != nil {
		return nil, err
	}
	return AnswerOmits{Phrases: phrases}, nil
}

// Judge — LLM-судья по одной рубрике. В отличие от встроенного llm_judge
// ago, в промпт попадает не только финальный ответ, но и список вызовов
// тулов: рубрики часто про то, ЧТО агент сделал, а не только написал.
type Judge struct {
	Client agoconfig.LLMClient
	Rubric string
}

func (Judge) Name() string { return "judge" }

func (j Judge) Score(ctx context.Context, t *eval.Trace) (eval.Score, error) {
	if j.Client == nil {
		return eval.Score{}, fmt.Errorf("judge: нужен LLM-клиент (живой прогон)")
	}
	req := &events.ModelRequest{Parts: []events.Input{&events.TextInput{Content: judgePrompt(j.Rubric, t)}}}
	resp, err := j.Client.Call(ctx, []events.Event{req}, nil, agoconfig.CallOptions{})
	if err != nil {
		return eval.Score{}, fmt.Errorf("judge: вызов судьи: %w", err)
	}
	var verdict struct {
		Score  *float64 `json:"score"`
		Reason string   `json:"reason"`
	}
	if err := unmarshalVerdict(resp.Content(), &verdict); err != nil {
		return eval.Score{}, err
	}
	if verdict.Score == nil {
		return eval.Score{}, fmt.Errorf("judge: в вердикте нет score")
	}
	score := clamp01(*verdict.Score)
	details := map[string]any{"reason": verdict.Reason, "rubric": j.Rubric}
	if score != *verdict.Score {
		details["raw_score"] = *verdict.Score
	}
	return eval.Score{Name: j.Name(), Value: score, Passed: score >= 0.5, Details: details}, nil
}

func judgePrompt(rubric string, t *eval.Trace) string {
	var b strings.Builder
	b.WriteString("You are a strict evaluator grading a tabletop RPG Dungeon Master bot against a single criterion.\n")
	fmt.Fprintf(&b, "Criterion: %s\n", rubric)
	b.WriteString(`Return ONLY a JSON object: {"score": <number from 0 to 1>, "reason": "<brief justification>"}.` + "\n")
	b.WriteString("Judge only this criterion. 1 means it is fully met, 0 means it is clearly violated.\n")
	if len(t.ToolCalls) > 0 {
		b.WriteString("\n## Tool calls\n")
		for _, call := range t.ToolCalls {
			fmt.Fprintf(&b, "- %s(%s)", call.Name, truncate(call.Arguments, 300))
			if call.Err != "" {
				fmt.Fprintf(&b, " -> ERROR: %s", truncate(call.Err, 300))
			} else {
				fmt.Fprintf(&b, " -> %s", truncate(call.Result, 500))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## DM reply\n")
	b.WriteString(t.FinalResponse)
	return b.String()
}

func unmarshalVerdict(text string, v any) error {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "json")
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}
	if err := json.Unmarshal([]byte(s), v); err == nil {
		return nil
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(s[start:end+1]), v); err == nil {
			return nil
		}
	}
	return fmt.Errorf("judge: в ответе нет JSON-вердикта: %.200s", text)
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

// jsonSubset: ожидаемое — подмножество фактического.
func jsonSubset(expected, actual any) bool {
	if text, ok := expected.(string); ok && text == "*" {
		return true
	}
	switch want := expected.(type) {
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range want {
			actualValue, exists := got[key]
			if !exists || !jsonSubset(value, actualValue) {
				return false
			}
		}
		return true
	case []any:
		got, ok := actual.([]any)
		if !ok {
			return false
		}
		// Каждый фактический элемент расходуется не больше одного раза:
		// иначе два ожидаемых врага «находились» бы в одном.
		used := make([]bool, len(got))
		var match func(index int) bool
		match = func(index int) bool {
			if index == len(want) {
				return true
			}
			for i, candidate := range got {
				if used[i] || !jsonSubset(want[index], candidate) {
					continue
				}
				used[i] = true
				if match(index + 1) {
					return true
				}
				used[i] = false
			}
			return false
		}
		return match(0)
	case float64:
		got, ok := actual.(float64)
		return ok && want == got
	case bool:
		got, ok := actual.(bool)
		return ok && want == got
	case string:
		got, ok := actual.(string)
		return ok && strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(got))
	case nil:
		return actual == nil
	default:
		return reflect.DeepEqual(expected, actual)
	}
}

// normalizeJSON прогоняет значение через JSON, чтобы числа в ожидаемых
// аргументах были того же типа (float64), что и в разобранных аргументах.
func normalizeJSON(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return value
	}
	return out
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

func checkKeys(params map[string]any, allowed ...string) error {
	allowedSet := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = true
	}
	var unknown []string
	for key := range params {
		if !allowedSet[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("неизвестные параметры: %s (разрешены: %s)",
			strings.Join(unknown, ", "), strings.Join(allowed, ", "))
	}
	return nil
}

func stringParam(params map[string]any, key string) (string, error) {
	raw, ok := params[key]
	if !ok {
		return "", fmt.Errorf("не задан параметр %q", key)
	}
	value, ok := raw.(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("параметр %q должен быть непустой строкой", key)
	}
	return value, nil
}

func stringsParam(params map[string]any, key string) ([]string, error) {
	raw, ok := params[key]
	if !ok {
		return nil, fmt.Errorf("не задан параметр %q", key)
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("параметр %q должен быть списком строк", key)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value, isString := item.(string)
		if !isString || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("параметр %q должен содержать только непустые строки", key)
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("параметр %q не должен быть пустым", key)
	}
	return out, nil
}
