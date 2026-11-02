package engine

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func ptr[T any](value T) *T { return &value }

// seq отдаёт заданные значения по порядку, а после исчерпания повторяет
// последнее — так в TS-тестах работали постоянные лямбды `() => 0.5`.
func seq(values ...float64) RandomSource {
	index := 0
	return func() float64 {
		if len(values) == 0 {
			return 0
		}
		if index >= len(values) {
			return values[len(values)-1]
		}
		value := values[index]
		index++
		return value
	}
}

// d20 превращает грань 1..20 в значение источника [0,1), как в TS-тестах.
func d20(face int) float64 { return (float64(face) - 0.5) / 20 }

func TestRollDice(t *testing.T) {
	t.Parallel()

	t.Run("суммирует броски", func(t *testing.T) {
		result, err := RollDice(6, 3, seq(0.5), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Rolls, []int{4, 4, 4}) || result.Total != 12 {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("по умолчанию одна кость", func(t *testing.T) {
		result, err := RollDice(6, 1, seq(0), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rolls) != 1 || result.Total != 1 {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("минимальный рандом даёт единицы", func(t *testing.T) {
		result, _ := RollDice(20, 2, seq(0), nil)
		if !reflect.DeepEqual(result.Rolls, []int{1, 1}) || result.Total != 2 {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("рандом около единицы даёт максимум", func(t *testing.T) {
		result, _ := RollDice(20, 2, seq(0.999999), nil)
		if !reflect.DeepEqual(result.Rolls, []int{20, 20}) {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("отклоняет неверные грани", func(t *testing.T) {
		for _, sides := range []int{0, -6} {
			if _, err := RollDice(sides, 1, seq(0.5), nil); err == nil || !strings.Contains(err.Error(), "at least one side") {
				t.Fatalf("sides=%d: %v", sides, err)
			}
		}
	})

	t.Run("отклоняет неверное количество", func(t *testing.T) {
		for _, count := range []int{0, -1, 101} {
			_, err := RollDice(6, count, seq(0.5), nil)
			if err == nil {
				t.Fatalf("count=%d должен падать", count)
			}
		}
	})

	t.Run("грани в допустимом диапазоне", func(t *testing.T) {
		result, err := RollDice(8, 100, seq(0.1234, 0.9876, 0.5), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, roll := range result.Rolls {
			if roll < 1 || roll > 8 {
				t.Fatalf("roll %d вне диапазона", roll)
			}
		}
	})
}

func TestRollDiceAdvantage(t *testing.T) {
	t.Parallel()

	t.Run("преимущество берёт большее и пишет пары", func(t *testing.T) {
		result, _ := RollDice(20, 1, seq(0, 0.999999), ptr(true))
		if !reflect.DeepEqual(result.Rolls, []int{20}) {
			t.Fatalf("rolls = %v", result.Rolls)
		}
		if !reflect.DeepEqual(result.Pairs, [][2]int{{20, 1}}) || result.Total != 20 {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("помеха берёт меньшее", func(t *testing.T) {
		result, _ := RollDice(20, 1, seq(0.5, 0), ptr(false))
		if result.Rolls[0] != 1 || !reflect.DeepEqual(result.Pairs, [][2]int{{1, 11}}) {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("преимущество игнорируется для не-d20", func(t *testing.T) {
		result, _ := RollDice(6, 2, seq(0.5), ptr(true))
		if !reflect.DeepEqual(result.Rolls, []int{4, 4}) || result.Pairs != nil {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("nil ведёт себя как обычный бросок", func(t *testing.T) {
		result, _ := RollDice(20, 2, seq(0.5), nil)
		if !reflect.DeepEqual(result.Rolls, []int{11, 11}) || result.Pairs != nil {
			t.Fatalf("получено %+v", result)
		}
	})
}

func TestSkillCheck(t *testing.T) {
	t.Parallel()

	stats := map[string]any{"strength": 16, "dexterity": 8, "wisdom": 14}

	t.Run("считает модификатор и успех", func(t *testing.T) {
		result := SkillCheck(stats, "perception", 12, nil, seq(0.5))
		if result.Roll != 11 || result.Modifier != 2 || result.Total != 13 || result.Margin != 1 || !result.Success || result.Ability != "wis" {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("провал ниже сложности", func(t *testing.T) {
		result := SkillCheck(stats, "strength", 20, nil, seq(0))
		if result.Success || result.Margin != -16 {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("натуральная 20 — успех при любом DC", func(t *testing.T) {
		result := SkillCheck(stats, "strength", 100, nil, seq(0.999999))
		if result.Roll != 20 || !result.NaturalSuccess || !result.Success {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("натуральная 1 — провал при любом DC", func(t *testing.T) {
		result := SkillCheck(stats, "strength", 1, nil, seq(0))
		if result.Roll != 1 || !result.NaturalFailure || result.Success {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("преимущество берёт большее", func(t *testing.T) {
		result := SkillCheck(stats, "strength", 10, ptr(true), seq(0, 0.999999))
		if result.Roll != 20 {
			t.Fatalf("roll = %d", result.Roll)
		}
	})

	t.Run("помеха берёт меньшее", func(t *testing.T) {
		result := SkillCheck(stats, "strength", 10, ptr(false), seq(0.5, 0))
		if result.Roll != 1 {
			t.Fatalf("roll = %d", result.Roll)
		}
	})

	t.Run("без характеристик модификатор 0", func(t *testing.T) {
		result := SkillCheck(map[string]any{}, "athletics", 11, nil, seq(0.5))
		if result.Modifier != 0 || result.Total != 11 || !result.Success {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("неизвестный навык — сила", func(t *testing.T) {
		result := SkillCheck(map[string]any{"strength": 10}, "unheard_of_skill", 10, nil, seq(0.5))
		if result.Ability != "str" {
			t.Fatalf("ability = %q", result.Ability)
		}
	})

	t.Run("строковые характеристики приводятся к числу", func(t *testing.T) {
		result := SkillCheck(map[string]any{"str": "20"}, "athletics", 15, nil, seq(0.5))
		if result.Modifier != 5 || result.Total != 16 {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("нечисловые характеристики игнорируются", func(t *testing.T) {
		result := SkillCheck(map[string]any{"str": "banana"}, "athletics", 10, nil, seq(0.5))
		if result.Modifier != 0 {
			t.Fatalf("modifier = %d", result.Modifier)
		}
	})
}

func TestResolveSkillAbility(t *testing.T) {
	t.Parallel()

	cases := []struct {
		skill string
		want  string
	}{
		{"athletics", "str"},
		{"stealth", "dex"},
		{"perception", "wis"},
		{"persuasion", "cha"},
		{"arcana", "int"},
		{"athletic", "str"},
		{"Восприятие", "wis"},
		{"восприятие", "wis"},
		{"Восприятие (Perception)", "wis"},
		{"Ловкость рук", "dex"},
		{"Убеждение", "cha"},
		{"расследование", "int"},
		{"unknown", "str"},
		{"", "str"},
		{"  perception  ", "wis"},
	}
	for _, tc := range cases {
		if got := ResolveSkillAbility(tc.skill); got != tc.want {
			t.Errorf("ResolveSkillAbility(%q) = %q, want %q", tc.skill, got, tc.want)
		}
	}
}

func TestAbilityScore(t *testing.T) {
	t.Parallel()

	stats := map[string]any{"strength": 15, "DEX": 12, "Wisdom": 9}
	cases := []struct {
		ability string
		want    int
		found   bool
	}{
		{"strength", 15, true},
		{"STR", 15, true},
		{"dexterity", 12, true},
		{"dex", 12, true},
		{"wisdom", 9, true},
		{"charisma", 0, false},
	}
	for _, tc := range cases {
		got, found := AbilityScore(stats, tc.ability)
		if got != tc.want || found != tc.found {
			t.Errorf("AbilityScore(%q) = (%d, %v), want (%d, %v)", tc.ability, got, found, tc.want, tc.found)
		}
	}
	if _, found := AbilityScore(map[string]any{}, "str"); found {
		t.Error("пустые характеристики не должны находить значение")
	}
	if got, _ := AbilityScore(map[string]any{"str": 15.7}, "str"); got != 15 {
		t.Errorf("дробное значение = %d, want 15 (trunc)", got)
	}

	// ±Inf/NaN и переполнение отклоняются, как Number.isFinite в TS.
	for _, value := range []any{"inf", "Infinity", "-Infinity", "nan", "1e400", math.Inf(1), math.Inf(-1), math.NaN()} {
		if _, found := AbilityScore(map[string]any{"str": value}, "str"); found {
			t.Errorf("значение %v должно игнорироваться", value)
		}
	}

	// Несколько написаний одной характеристики: выбор детерминирован
	// (первый по алфавиту ключ), а не зависит от обхода map.
	stats = map[string]any{"strength": 14, "str": 12}
	first, found := AbilityScore(stats, "str")
	if !found || first != 12 {
		t.Fatalf("AbilityScore = (%d, %v), want (12, true): ключ \"str\" идёт первым", first, found)
	}
	for i := 0; i < 50; i++ {
		if got, _ := AbilityScore(stats, "str"); got != first {
			t.Fatalf("недетерминированный выбор: %d != %d", got, first)
		}
	}
}

func TestAbilityModifier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		stats map[string]any
		stat  string
		want  int
	}{
		{map[string]any{"strength": 15}, "str", 2},
		{map[string]any{"strength": 10}, "str", 0},
		{map[string]any{"strength": 8}, "str", -1},
		{map[string]any{"dexterity": 20}, "dex", 5},
		{map[string]any{}, "str", 0},
	}
	for _, tc := range cases {
		if got := AbilityModifier(tc.stats, tc.stat); got != tc.want {
			t.Errorf("AbilityModifier(%v, %q) = %d, want %d", tc.stats, tc.stat, got, tc.want)
		}
	}
}

func TestProficiencyBonus(t *testing.T) {
	t.Parallel()

	cases := map[int]int{1: 2, 4: 2, 5: 3, 9: 4, 13: 5, 17: 6, 20: 6, 0: 2}
	for level, want := range cases {
		if got := ProficiencyBonus(level); got != want {
			t.Errorf("ProficiencyBonus(%d) = %d, want %d", level, got, want)
		}
	}
}

func TestParseDiceNotation(t *testing.T) {
	t.Parallel()

	success := []struct {
		spec string
		want ParsedNotation
	}{
		{"4d20", ParsedNotation{Groups: []DiceGroup{{Count: 4, Sides: 20}}}},
		{"d8", ParsedNotation{Groups: []DiceGroup{{Count: 1, Sides: 8}}}},
		{"2d6+1d8+3", ParsedNotation{Groups: []DiceGroup{{Count: 2, Sides: 6}, {Count: 1, Sides: 8}}, Modifier: 3}},
		{"2d4-1", ParsedNotation{Groups: []DiceGroup{{Count: 2, Sides: 4}}, Modifier: -1}},
		{" 2D6 + 1D8 ", ParsedNotation{Groups: []DiceGroup{{Count: 2, Sides: 6}, {Count: 1, Sides: 8}}}},
		{"4d6kh3", ParsedNotation{Groups: []DiceGroup{{Count: 4, Sides: 6, KeepHigh: ptr(3)}}}},
		{"4d6dl1", ParsedNotation{Groups: []DiceGroup{{Count: 4, Sides: 6, DropLow: ptr(1)}}}},
		{"8d6!", ParsedNotation{Groups: []DiceGroup{{Count: 8, Sides: 6, Explode: true}}}},
		{"3d20kl1", ParsedNotation{Groups: []DiceGroup{{Count: 3, Sides: 20, KeepLow: ptr(1)}}}},
		{"3d20dh1", ParsedNotation{Groups: []DiceGroup{{Count: 3, Sides: 20, DropHigh: ptr(1)}}}},
		{"4d6kh3+2", ParsedNotation{Groups: []DiceGroup{{Count: 4, Sides: 6, KeepHigh: ptr(3)}}, Modifier: 2}},
	}
	for _, tc := range success {
		got, err := ParseDiceNotation(tc.spec)
		if err != nil {
			t.Errorf("ParseDiceNotation(%q): %v", tc.spec, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseDiceNotation(%q) = %+v, want %+v", tc.spec, got, tc.want)
		}
	}

	failures := []struct {
		spec string
		want string
	}{
		{"", "Пустая нотация"},
		{"   ", "Пустая нотация"},
		{"2d6+abc", "Недопустимые символы"},
		{"roll 1d20", "Недопустимые символы"},
		{"0d6", "≥ 1"},
		{"2d0", "≥ 1"},
		{"101d6", "Слишком много костей"},
		{"1d6+1d6+1d6+1d6+1d6+1d6", "Слишком много групп"},
		{"4d6kh3dl1", "нельзя комбинировать keep"},
		{"4d6kh2kl1", "нельзя одновременно kh и kl"},
		{"4d6kh0", "kh должен быть"},
		{"4d6kh4", "kh должен быть"},
		{"4d6dl4", "dl должен быть"},
	}
	for _, tc := range failures {
		_, err := ParseDiceNotation(tc.spec)
		if err == nil {
			t.Errorf("ParseDiceNotation(%q) должен падать", tc.spec)
			continue
		}
		// TS-сообщения начинаются с заглавной; в Go — со строчной.
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
			t.Errorf("ParseDiceNotation(%q): ошибка %q не содержит %q", tc.spec, err, tc.want)
		}
	}

	// Три сообщения, которые roll_dice показывает LLM, — байт-в-байт как TS.
	exact := []struct {
		spec string
		want string
	}{
		{"", "Пустая нотация броска «». Пример: «2d6+1d8+3»."},
		{"2d6+abc", "Недопустимые символы в нотации «2d6+abc». Допускаются цифры, d, +, -, kh/kl/dh/dl, !."},
		{"1d6+", "Неправильная нотация броска «1d6+». Пример: «2d6+1d8+3»."},
	}
	for _, tc := range exact {
		_, err := ParseDiceNotation(tc.spec)
		if err == nil || err.Error() != tc.want {
			t.Errorf("ParseDiceNotation(%q) = %v, want %q", tc.spec, err, tc.want)
		}
	}
}

func TestRollDiceNotation(t *testing.T) {
	t.Parallel()

	t.Run("суммирует группы и модификатор", func(t *testing.T) {
		result, err := RollDiceNotation("2d6+1d8+3", NotationOptions{Random: seq(0.5)})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Groups) != 2 || result.Groups[0].Subtotal != 8 || result.Groups[1].Subtotal != 5 || result.Modifier != 3 || result.Total != 16 {
			t.Fatalf("получено %+v", result)
		}
	})

	t.Run("преимущество действует только на d20", func(t *testing.T) {
		random := seq(0, 0.999999, 0, 0.999999, 0.5)
		result, err := RollDiceNotation("2d20+1d6", NotationOptions{Random: random, Advantage: ptr(true)})
		if err != nil {
			t.Fatal(err)
		}
		d20Group, d6Group := result.Groups[0], result.Groups[1]
		if !reflect.DeepEqual(d20Group.Rolls, []int{20, 20}) {
			t.Fatalf("d20 rolls = %v", d20Group.Rolls)
		}
		if !reflect.DeepEqual(d20Group.Pairs, [][2]int{{20, 1}, {20, 1}}) || d20Group.Subtotal != 40 {
			t.Fatalf("d20 = %+v", d20Group)
		}
		if d6Group.Rolls[0] != 4 || d6Group.Pairs != nil || result.Total != 44 {
			t.Fatalf("d6/итог = %+v / %d", d6Group, result.Total)
		}
	})

	t.Run("отрицательный модификатор уменьшает итог", func(t *testing.T) {
		result, _ := RollDiceNotation("1d6-2", NotationOptions{Random: seq(0.5)})
		if result.Total != 2 {
			t.Fatalf("total = %d", result.Total)
		}
	})

	t.Run("kh оставляет N лучших и отдаёт отброшенные", func(t *testing.T) {
		result, _ := RollDiceNotation("4d6kh3", NotationOptions{Random: seq(0.5)})
		group := result.Groups[0]
		if !reflect.DeepEqual(group.Rolls, []int{4, 4, 4}) || !reflect.DeepEqual(group.Dropped, []int{4}) || group.Subtotal != 12 {
			t.Fatalf("получено %+v", group)
		}
	})

	t.Run("dl1 эквивалентно kh3", func(t *testing.T) {
		random := seq((3-1)/6.0+1e-9, (6-1)/6.0+1e-9, (1-1)/6.0+1e-9, (4-1)/6.0+1e-9)
		result, _ := RollDiceNotation("4d6dl1", NotationOptions{Random: random})
		group := result.Groups[0]
		rolls := append([]int(nil), group.Rolls...)
		sortInts(rolls)
		if !reflect.DeepEqual(rolls, []int{3, 4, 6}) || group.Subtotal != 13 {
			t.Fatalf("получено %+v", group)
		}
	})

	t.Run("kl1 оставляет минимум", func(t *testing.T) {
		random := seq((3-1)/6.0+1e-9, (6-1)/6.0+1e-9, (1-1)/6.0+1e-9)
		result, _ := RollDiceNotation("3d6kl1", NotationOptions{Random: random})
		group := result.Groups[0]
		dropped := append([]int(nil), group.Dropped...)
		sortInts(dropped)
		if !reflect.DeepEqual(group.Rolls, []int{1}) || !reflect.DeepEqual(dropped, []int{3, 6}) || group.Subtotal != 1 {
			t.Fatalf("получено %+v", group)
		}
	})

	t.Run("взрыв добавляет кости до лимита", func(t *testing.T) {
		result, _ := RollDiceNotation("1d6!", NotationOptions{Random: seq(0.999999)})
		group := result.Groups[0]
		if len(group.Rolls) > 11 {
			t.Fatalf("слишком много костей: %d", len(group.Rolls))
		}
		for _, roll := range group.Rolls {
			if roll != 6 {
				t.Fatalf("roll = %d", roll)
			}
		}
		if group.Subtotal != len(group.Rolls)*6 {
			t.Fatalf("subtotal = %d", group.Subtotal)
		}
	})

	t.Run("взрыв останавливается на не-максимуме", func(t *testing.T) {
		random := seq((6-1)/6.0+1e-9, (6-1)/6.0+1e-9, (3-1)/6.0+1e-9)
		result, _ := RollDiceNotation("1d6!", NotationOptions{Random: random})
		group := result.Groups[0]
		if !reflect.DeepEqual(group.Rolls, []int{6, 6, 3}) || group.Subtotal != 15 {
			t.Fatalf("получено %+v", group)
		}
	})
}

func sortInts(values []int) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func TestMakeLuckyRandom(t *testing.T) {
	t.Parallel()

	t.Run("значение в [0, 1)", func(t *testing.T) {
		lucky := MakeLuckyRandom(seq(0.7), false)
		if value := lucky(); value < 0 || value >= 1 {
			t.Fatalf("lucky() = %v вне диапазона", value)
		}
	})

	t.Run("при серии грань не ниже LowStreakFloor", func(t *testing.T) {
		lucky := MakeLuckyRandom(seq(0), true)
		face := int(lucky()*20) + 1
		if face < LowStreakFloor {
			t.Fatalf("face %d ниже пола %d", face, LowStreakFloor)
		}
	})

	t.Run("не опускает ниже обычного низкого броска", func(t *testing.T) {
		// r=0.1 → грань 2; reroll-chance потребляет base()=0.0 < 0.5,
		// второй base()=0.9 → max(0.1, 0.9)=0.9 → грань 19.
		lucky := MakeLuckyRandom(seq(0.1, 0, 0.9), false)
		face := int(lucky()*20) + 1
		if face < 2 {
			t.Fatalf("face %d ниже обычного низкого броска", face)
		}
	})

	t.Run("точные значения при детерминированном источнике", func(t *testing.T) {
		if got := MakeLuckyRandom(seq(0.1, 0, 0.9), false)(); got != 0.9 {
			t.Errorf("низкий бросок с перебросом = %v, want 0.9", got)
		}
		// Серия: minUnit=0.5, r=0 → 0.5 + 0.5*base(); base()=0.5 → 0.75.
		if got := MakeLuckyRandom(seq(0, 0.5), true)(); got != 0.75 {
			t.Errorf("серия = %v, want 0.75", got)
		}
		if got := MakeLuckyRandom(seq(0.1, 0.9), false)(); got != 0.1 {
			t.Errorf("низкий бросок без переброса = %v, want 0.1", got)
		}
	})

	t.Run("не низкий бросок не потребляет второй вызов", func(t *testing.T) {
		calls := 0
		source := func() float64 {
			calls++
			return 0.7
		}
		lucky := MakeLuckyRandom(source, false)
		if got := lucky(); got != 0.7 {
			t.Fatalf("lucky() = %v", got)
		}
		if calls != 1 {
			t.Fatalf("источник вызван %d раз, want 1", calls)
		}
	})
}

func TestIsLowStreak(t *testing.T) {
	t.Parallel()

	cases := []struct {
		history []int
		want    bool
	}{
		{[]int{3, 5}, true},
		{[]int{2, 4, 6}, true},
		{[]int{3, 12}, false},
		{[]int{3}, false},
		{[]int{1}, false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := IsLowStreak(tc.history); got != tc.want {
			t.Errorf("IsLowStreak(%v) = %v, want %v", tc.history, got, tc.want)
		}
	}
}
