package evalgame

import (
	"context"
	"strings"
	"testing"

	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"

	"github.com/luckystrker/ago/eval"
)

func TestJSONSubset(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		expected any
		actual   any
		want     bool
	}{
		{"объект-подмножество", map[string]any{"a": 1.0}, map[string]any{"a": 1.0, "b": 2.0}, true},
		{"чужой ключ", map[string]any{"a": 1.0}, map[string]any{"b": 1.0}, false},
		{"строка без регистра", map[string]any{"name": "дэн"}, map[string]any{"name": " Дэн "}, true},
		{"массив ищет каждый элемент", map[string]any{"xs": []any{"a", "b"}}, map[string]any{"xs": []any{"c", "b", "a"}}, true},
		{"массиву не хватает элемента", map[string]any{"xs": []any{"a", "z"}}, map[string]any{"xs": []any{"a", "b"}}, false},
		{"wildcard", map[string]any{"hp": "*"}, map[string]any{"hp": 7.0}, true},
		{"bool", map[string]any{"advantage": true}, map[string]any{"advantage": true}, true},
		{"bool не совпал", map[string]any{"advantage": true}, map[string]any{"advantage": false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := jsonSubset(tc.expected, tc.actual); got != tc.want {
				t.Fatalf("jsonSubset(%v, %v) = %v, want %v", tc.expected, tc.actual, got, tc.want)
			}
		})
	}
}

func TestToolArgsScorer(t *testing.T) {
	t.Parallel()

	trace := &eval.Trace{ToolCalls: []eval.ToolCall{
		{Name: "initiative", Arguments: `{"combatants":[{"name":"Дэн","side":"party"},{"name":"Гоблин","side":"enemy","hp":7,"ac":13}]}`},
	}}

	pass := ToolArgs{
		Tool: "initiative",
		Args: map[string]any{"combatants": []any{
			map[string]any{"name": "Дэн", "side": "party"},
			map[string]any{"side": "enemy", "hp": "*", "ac": "*"},
		}},
	}
	score, err := pass.Score(context.Background(), trace)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !score.Passed {
		t.Fatalf("tool_args должен пройти, детали: %v", score.Details)
	}

	missingEnemy := ToolArgs{
		Tool: "initiative",
		Args: map[string]any{"combatants": []any{
			map[string]any{"name": "Дэн", "side": "party"},
			map[string]any{"side": "enemy", "hp": "*", "ac": "*"},
			map[string]any{"side": "enemy", "hp": "*", "ac": "*"},
		}},
	}
	score, _ = missingEnemy.Score(context.Background(), trace)
	if score.Passed {
		t.Fatal("tool_args не должен проходить, когда участник пропущен")
	}

	contains := ToolArgs{Tool: "roll_dice", Contains: map[string]string{"notation": "1d20"}}
	diceTrace := &eval.Trace{ToolCalls: []eval.ToolCall{{Name: "roll_dice", Arguments: `{"notation":"1d20+4"}`}}}
	if score, _ = contains.Score(context.Background(), diceTrace); !score.Passed {
		t.Fatalf("contains должен найти подстроку, детали: %v", score.Details)
	}

	noCall := ToolArgs{Tool: "combat", Args: map[string]any{"action": "attack"}}
	if score, _ = noCall.Score(context.Background(), trace); score.Passed {
		t.Fatal("tool_args должен падать, если тул не вызывался")
	}
}

func TestQuotesRollScorer(t *testing.T) {
	t.Parallel()

	trace := &eval.Trace{
		ToolCalls: []eval.ToolCall{{
			Name:   "skill_check",
			Result: "🎲 Дэн — проверка «Восприятие» (МДР): d20 = 14 +3 = 17 vs DC 12 -> УСПЕХ",
		}},
		FinalResponse: "Дэн замечает царапины: проверка Восприятия — d20 14, итог 17 против DC 12, успех.",
	}
	score, err := QuotesRoll{}.Score(context.Background(), trace)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !score.Passed {
		t.Fatalf("quotes_roll должен пройти, детали: %v", score.Details)
	}

	trace.FinalResponse = "Дэн замечает царапины на полу."
	score, _ = QuotesRoll{}.Score(context.Background(), trace)
	if score.Passed {
		t.Fatal("quotes_roll не должен проходить, когда числа броска не произнесены")
	}
	missing, ok := score.Details["missing_numbers"].([]string)
	if !ok || len(missing) == 0 {
		t.Fatalf("ожидался список непроцитированных чисел, детали: %v", score.Details)
	}

	noRolls := &eval.Trace{FinalResponse: "Дэн ничего не проверяет."}
	if score, _ = (QuotesRoll{}).Score(context.Background(), noRolls); score.Passed {
		t.Fatal("quotes_roll должен падать без вызовов бросковых тулов")
	}
}

func TestQuotesRollInitiative(t *testing.T) {
	t.Parallel()

	trace := &eval.Trace{
		ToolCalls: []eval.ToolCall{{
			Name:   "initiative",
			Result: "Инициатива:\n1. Дэн [id: dan] (15 = 12 + 3, HP 10/10)\n2. Гоблин [id: goblin] (11 = 8 + 3, враг, HP 7, КД 13)",
		}},
		FinalResponse: "Порядок: Дэн — 15, гоблин — 11. Первым ходит Дэн.",
	}
	if score, _ := (QuotesRoll{Tools: []string{"initiative"}}).Score(context.Background(), trace); !score.Passed {
		t.Fatalf("инициатива должна цитироваться полностью, детали: %v", score.Details)
	}

	trace.FinalResponse = "Первым ходит Дэн."
	if score, _ := (QuotesRoll{Tools: []string{"initiative"}}).Score(context.Background(), trace); score.Passed {
		t.Fatal("quotes_roll не должен проходить без сумм инициативы")
	}
}

func TestAnswerOmits(t *testing.T) {
	t.Parallel()

	trace := &eval.Trace{FinalResponse: "Ты чувствуешь, как холодок бежит по спине."}
	score, _ := AnswerOmits{Phrases: []string{"Ты чувствуешь", "холодок бежит"}}.Score(context.Background(), trace)
	if score.Passed {
		t.Fatal("answer_omits должен найти запрещённые фразы")
	}
	if found, ok := score.Details["found"].([]string); !ok || len(found) != 2 {
		t.Fatalf("ожидались обе фразы, детали: %v", score.Details)
	}

	trace.FinalResponse = "Дверь холодная, из щели тянет сквозняком."
	if score, _ = (AnswerOmits{Phrases: []string{"ты чувствуешь"}}).Score(context.Background(), trace); !score.Passed {
		t.Fatal("answer_omits не должен падать на чистом ответе")
	}
}

func TestJudgeScorer(t *testing.T) {
	t.Parallel()

	client := agotest.NewTestConfig(&events.ModelResponse{
		Message: &events.ModelMessage{Content: `{"score": 0.8, "reason": "criterion met"}`},
	})
	score, err := Judge{Client: client, Rubric: "world only"}.Score(context.Background(), &eval.Trace{FinalResponse: "Таверна гудит."})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !score.Passed || score.Value != 0.8 {
		t.Fatalf("неожиданный вердикт судьи: %+v", score)
	}

	// Судья видит не только ответ, но и вызовы тулов.
	prompt := judgePrompt("no roll_dice", &eval.Trace{
		FinalResponse: "Бросок.",
		ToolCalls:     []eval.ToolCall{{Name: "roll_dice", Arguments: `{"notation":"1d20"}`}},
	})
	for _, want := range []string{"roll_dice", "1d20", "Бросок.", "no roll_dice"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("в промпте судьи нет %q:\n%s", want, prompt)
		}
	}

	// Без клиента судья не притворяется, что справился.
	if _, err := (Judge{Rubric: "x"}).Score(context.Background(), &eval.Trace{}); err == nil {
		t.Fatal("judge без клиента должен возвращать ошибку")
	}
}

func TestJudgeToleratesFencedVerdict(t *testing.T) {
	t.Parallel()

	client := agotest.NewTestConfig(&events.ModelResponse{
		Message: &events.ModelMessage{Content: "```json\n{\"score\": 1, \"reason\": \"ok\"}\n```"},
	})
	score, err := Judge{Client: client, Rubric: "x"}.Score(context.Background(), &eval.Trace{})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !score.Passed {
		t.Fatalf("огороженный вердикт должен разбираться: %+v", score)
	}
}

func TestToolOKScorer(t *testing.T) {
	t.Parallel()

	clean := &eval.Trace{ToolCalls: []eval.ToolCall{
		{Name: "combat", Arguments: `{"action":"damage","target":"Дэн","amount":4}`, Result: "Дэн: урон 4 HP → 6/10."},
	}}
	score, err := ToolOK{Tool: "combat"}.Score(context.Background(), clean)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !score.Passed {
		t.Fatalf("tool_ok должен проходить на чистом результате, детали: %v", score.Details)
	}

	// Отказ по авторизации в тексте результата — ровно баг групповой записи.
	denied := &eval.Trace{ToolCalls: []eval.ToolCall{{
		Name:      "combat",
		Arguments: `{"action":"damage","target":"Дэн","amount":4}`,
		Result:    "Это действие доступно только администратору (dm) кампании.",
	}}}
	score, _ = (ToolOK{Tool: "combat"}).Score(context.Background(), denied)
	if score.Passed {
		t.Fatal("tool_ok не должен проходить на отказе в результате")
	}
	if refused, ok := score.Details["refused"].([]string); !ok || len(refused) == 0 {
		t.Fatalf("ожидалась причина отказа, детали: %v", score.Details)
	}

	// Ошибка через Err трассы.
	errored := &eval.Trace{ToolCalls: []eval.ToolCall{{Name: "combat", Err: "tool: boom"}}}
	if score, _ = (ToolOK{Tool: "combat"}).Score(context.Background(), errored); score.Passed {
		t.Fatal("tool_ok не должен проходить на Err")
	}

	// ok:false в JSON-результате.
	jsonRefused := &eval.Trace{ToolCalls: []eval.ToolCall{{Name: "grant_character", Result: `{"ok":false,"error":"access_denied"}`}}}
	if score, _ = (ToolOK{Tool: "grant_character"}).Score(context.Background(), jsonRefused); score.Passed {
		t.Fatal("tool_ok не должен проходить на {ok:false}")
	}

	// Успешный JSON-результат с «запрещено»/«ошибка» в тексте — не отказ:
	// upsert_npc/create_quest эхом возвращают заметку, написанную моделью.
	successEcho := &eval.Trace{ToolCalls: []eval.ToolCall{{
		Name:   "upsert_npc",
		Result: `{"ok":true,"npc":{"name":"Купец","memory":"здесь колдовать запрещено, но ошибка была его"},"note":"error в описании"}`,
	}}}
	if score, _ = (ToolOK{Tool: "upsert_npc"}).Score(context.Background(), successEcho); !score.Passed {
		t.Fatalf("успешная запись с «запрещено» в заметке должна проходить, детали: %v", score.Details)
	}

	// Пустое поле error — не отказ.
	emptyError := &eval.Trace{ToolCalls: []eval.ToolCall{{Name: "create_quest", Result: `{"ok":true,"error":""}`}}}
	if score, _ = (ToolOK{Tool: "create_quest"}).Score(context.Background(), emptyError); !score.Passed {
		t.Fatalf("пустой error не должен считаться отказом, детали: %v", score.Details)
	}

	// Аргументы не совпали — вызов не считается.
	if score, _ = (ToolOK{Tool: "combat", Args: map[string]any{"action": "attack"}}).Score(context.Background(), clean); score.Passed {
		t.Fatal("tool_ok не должен проходить, когда подходящего по аргументам вызова нет")
	}

	// Второй вызов чистый — общий результат pass.
	mixed := &eval.Trace{ToolCalls: []eval.ToolCall{
		{Name: "combat", Result: "Сейчас ход Лисы — атаковать может только участник, чей ход наступил."},
		{Name: "combat", Arguments: `{"action":"next"}`, Result: "Ход переходит дальше."},
	}}
	if score, _ = (ToolOK{Tool: "combat"}).Score(context.Background(), mixed); !score.Passed {
		t.Fatalf("tool_ok должен проходить, если хотя бы один вызов чистый, детали: %v", score.Details)
	}
}

func TestQuotesRollAnchors(t *testing.T) {
	t.Parallel()

	result := "🎲 Дэн — проверка «Восприятие» (МДР): d20 = 7 +3 = 10 vs DC 15 -> ПРОВАЛ"

	// Формы, которыми живой DM честно цитирует бросок; все должны проходить.
	good := []string{
		"Бросок d20+3: выпало 7, итого 10 против DC 15.",
		"Выпало 7 на d20, с бонусом 10 — не дотягивает до DC 15.",
		"d20 = 7, +3 = 10 против DC 15.",
		"В итоге ты теряешь 2 минуты. d20 = 7, итог 10 против DC 15.",
		"d20 7, итог 10 против DC 15 — провал.",
	}
	for _, answer := range good {
		trace := &eval.Trace{
			ToolCalls:     []eval.ToolCall{{Name: "skill_check", Result: result}},
			FinalResponse: answer,
		}
		if score, _ := (QuotesRoll{}).Score(context.Background(), trace); !score.Passed {
			t.Errorf("корректный ответ %q должен проходить, детали: %v", answer, score.Details)
		}
	}

	// Числа в ответе есть, но рядом с якорями — выдуманные.
	trace := &eval.Trace{
		ToolCalls:     []eval.ToolCall{{Name: "skill_check", Result: result}},
		FinalResponse: "Бросок 12, итог 14 против 9 — провал. В комнате 7 факелов, до рассвета 15 минут.",
	}
	score, _ := (QuotesRoll{}).Score(context.Background(), trace)
	if score.Passed {
		t.Fatal("выдуманные числа у якорей должны валить quotes_roll")
	}
	wrong, ok := score.Details["wrong_numbers"].([]string)
	if !ok || len(wrong) == 0 {
		t.Fatalf("ожидался список неверных чисел, детали: %v", score.Details)
	}

	// Якорь отсутствует вовсе — тоже fail.
	trace.FinalResponse = "Дэн ничего не понимает."
	if score, _ = (QuotesRoll{}).Score(context.Background(), trace); score.Passed {
		t.Fatal("quotes_roll должен падать без якорей")
	}
}

func TestRegistryRejectsBadParams(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(nil)
	for _, tc := range []struct {
		name   string
		params map[string]any
	}{
		{"tool_args", map[string]any{"tool": "combat"}},
		{"tool_args", map[string]any{"tool": "combat", "args": map[string]any{"a": 1}, "typo": true}},
		{"tool_ok", map[string]any{"args": map[string]any{"a": 1}}},
		{"tool_ok", map[string]any{"tool": "combat", "typo": true}},
		{"tools_not_called", map[string]any{}},
		{"tool_sequence", map[string]any{"names": []any{"initiative"}}},
		{"quotes_roll", map[string]any{"tools": "skill_check"}},
		{"answer_omits", map[string]any{"phrases": []any{}}},
		{"judge", map[string]any{}},
	} {
		if _, err := registry.Build(tc.name, tc.params, eval.CaseScope{}); err == nil {
			t.Errorf("Build(%q, %v) должен падать", tc.name, tc.params)
		}
	}
}
