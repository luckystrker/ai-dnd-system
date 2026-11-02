package engine

import "testing"

// Форматы — контракт для eval-скорера quotes_roll и для игроков: строки
// должны совпадать с TS-тулами байт-в-байт.
func TestFormatSkillCheck(t *testing.T) {
	t.Parallel()

	success := CheckResult{Roll: 14, Modifier: 3, Total: 17, Difficulty: 12, Success: true, Ability: "wis"}
	want := "🎲 Дэн — проверка «Восприятие» (МДР): d20 = 14 +3 = 17 vs DC 12 -> УСПЕХ"
	if got := FormatSkillCheck("Дэн", "Восприятие", success, nil, true, nil); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}

	advantage := true
	critical := CheckResult{
		Roll: 20, Modifier: 2, Total: 22, Difficulty: 15, Success: true,
		Ability: "dex", NaturalSuccess: true,
	}
	want = "🎲 Лиса — проверка «Скрытность» (ЛОВ) (преимущество): d20 = 20 +2 = 22 vs DC 15 -> УСПЕХ (натуральная 20) [ночь: преимущество на скрытность]"
	if got := FormatSkillCheck("Лиса", "Скрытность", critical, &advantage, true, []string{"ночь: преимущество на скрытность"}); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}

	disadvantage := false
	failure := CheckResult{
		Roll: 1, Modifier: -1, Total: 0, Difficulty: 10, Success: false,
		Ability: "str", NaturalFailure: true,
	}
	want = "🎲 Дэн — проверка «Атлетика» (СИЛ) (помеха): d20 = 1 -1 = 0 vs DC 10 -> ПРОВАЛ (натуральная 1)"
	if got := FormatSkillCheck("Дэн", "Атлетика", failure, &disadvantage, true, nil); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}

	noStats := CheckResult{Roll: 11, Modifier: 0, Total: 11, Difficulty: 11, Success: true, Ability: "wis"}
	want = "🎲 Лиса — проверка «Восприятие» (МДР): d20 = 11 +0 = 11 vs DC 11 -> УСПЕХ [характеристики персонажа не найдены, модификатор +0]"
	if got := FormatSkillCheck("Лиса", "Восприятие", noStats, nil, false, nil); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestFormatDiceRoll(t *testing.T) {
	t.Parallel()

	result := NotationResult{
		Notation: "2d6+1d8+3",
		Groups: []DiceGroupResult{
			{Spec: "2d6", Rolls: []int{4, 4}, Subtotal: 8},
			{Spec: "1d8", Rolls: []int{5}, Subtotal: 5},
		},
		Modifier: 3,
		Total:    16,
	}
	want := "🎲 Бросок 2d6+1d8+3:\n2d6 → [4, 4] = 8\n1d8 → [5] = 5\n+3 модификатор\nИтого: 16"
	if got := FormatDiceRoll(result, AdvantageNormal); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}

	advantaged := NotationResult{
		Notation: "2d20",
		Groups: []DiceGroupResult{{
			Spec: "2d20", Rolls: []int{20, 20}, Pairs: [][2]int{{20, 1}, {20, 1}}, Subtotal: 40,
		}},
		Total: 40,
	}
	want = "🎲 Бросок 2d20 (преимущество):\n2d20 → [20|1, 20|1] = 40\nИтого: 40"
	if got := FormatDiceRoll(advantaged, AdvantageYes); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}

	dropped := NotationResult{
		Notation: "4d6dl1-2",
		Groups:   []DiceGroupResult{{Spec: "4d6dl1", Rolls: []int{4, 4, 4}, Dropped: []int{4}, Subtotal: 12}},
		Modifier: -2,
		Total:    10,
	}
	want = "🎲 Бросок 4d6dl1-2 (слабость):\n4d6dl1 → [4, 4, 4] (отброшено: 4) = 12\n-2 модификатор\nИтого: 10"
	if got := FormatDiceRoll(dropped, AdvantageNo); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

func TestFormatInitiativeLine(t *testing.T) {
	t.Parallel()

	party := CombatantEntry{ID: "dan", Name: "Дэн", Side: SideParty, Bonus: 3, Roll: 12, Total: 15, HP: ptr(10), MaxHP: ptr(10)}
	want := "1. Дэн [id: dan] (15 = 12 + 3, HP 10/10)"
	if got := FormatInitiativeLine(0, party); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}

	enemy := CombatantEntry{ID: "goblin-1", Name: "Гоблин", Side: SideEnemy, Bonus: 3, Roll: 8, Total: 11, HP: ptr(7), AC: ptr(13)}
	want = "3. Гоблин [id: goblin-1] (11 = 8 + 3, враг, HP 7, КД 13)"
	if got := FormatInitiativeLine(2, enemy); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}
