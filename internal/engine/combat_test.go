package engine

import (
	"encoding/json"
	"reflect"
	"testing"
)

// initiativeSequence задаёт выпавшие d20 (1..20), как sequence в TS-тестах.
func initiativeSequence(faces ...int) RandomSource {
	values := make([]float64, 0, len(faces))
	for _, face := range faces {
		values = append(values, d20(face))
	}
	return seq(values...)
}

func TestRollInitiative(t *testing.T) {
	t.Parallel()

	t.Run("сортирует по итогу по убыванию", func(t *testing.T) {
		order := RollInitiative([]CombatantInput{
			{Name: "Микроб", Side: SideParty, Bonus: 2},
			{Name: "Инокентий", Side: SideParty, Bonus: 2},
			{Name: "Тварь", Side: SideEnemy, Bonus: 4, HP: ptr(20), AC: ptr(12)},
		}, initiativeSequence(8, 3, 16))
		names := []string{order[0].Name, order[1].Name, order[2].Name}
		if !reflect.DeepEqual(names, []string{"Тварь", "Микроб", "Инокентий"}) {
			t.Fatalf("порядок = %v", names)
		}
		if order[0].Total != 20 || order[1].Total != 10 || order[2].Total != 5 {
			t.Fatalf("итоги = %d, %d, %d", order[0].Total, order[1].Total, order[2].Total)
		}
	})

	t.Run("сохраняет hp и ac врага", func(t *testing.T) {
		order := RollInitiative([]CombatantInput{
			{Name: "Тварь", Side: SideEnemy, HP: ptr(30), AC: ptr(14), Bonus: 1},
		}, initiativeSequence(12))
		if order[0].HP == nil || *order[0].HP != 30 || order[0].AC == nil || *order[0].AC != 14 {
			t.Fatalf("получено %+v", order[0])
		}
	})

	t.Run("ничьи по имени", func(t *testing.T) {
		order := RollInitiative([]CombatantInput{
			{Name: "Бета", Side: SideParty},
			{Name: "Альфа", Side: SideParty},
		}, initiativeSequence(10, 10))
		if order[0].Name != "Альфа" || order[1].Name != "Бета" {
			t.Fatalf("порядок = %v", []string{order[0].Name, order[1].Name})
		}
	})

	t.Run("ничьи как localeCompare: регистр, ё и латиница", func(t *testing.T) {
		cases := []struct {
			names []string
			want  []string
		}{
			{[]string{"Гном", "волк"}, []string{"волк", "Гном"}},
			{[]string{"яма", "ёж"}, []string{"ёж", "яма"}},
			{[]string{"Orc", "goblin"}, []string{"goblin", "Orc"}},
			{[]string{"Волк", "волк"}, []string{"волк", "Волк"}},
		}
		for _, tc := range cases {
			order := RollInitiative([]CombatantInput{
				{Name: tc.names[0], Side: SideParty},
				{Name: tc.names[1], Side: SideParty},
			}, initiativeSequence(10, 10))
			if order[0].Name != tc.want[0] || order[1].Name != tc.want[1] {
				t.Errorf("имена %v: порядок %v, want %v", tc.names, []string{order[0].Name, order[1].Name}, tc.want)
			}
		}
	})

	t.Run("генерирует slug-id из имени", func(t *testing.T) {
		order := RollInitiative([]CombatantInput{
			{Name: "Гоблин Воин", Side: SideEnemy, HP: ptr(10), AC: ptr(12)},
		}, initiativeSequence(10))
		if order[0].ID != "гоблин-воин" {
			t.Fatalf("id = %q", order[0].ID)
		}
	})

	t.Run("дедуплицирует одинаковые slug-id суффиксами", func(t *testing.T) {
		// Разные броски дают однозначный порядок: 15, 10, 5 — id должны
		// достаться участникам в порядке ввода (гоблин, -2, -3).
		order := RollInitiative([]CombatantInput{
			{Name: "Гоблин", Side: SideEnemy, HP: ptr(7), AC: ptr(12)},
			{Name: "гоблин", Side: SideEnemy, HP: ptr(7), AC: ptr(12)},
			{Name: "ГОБЛИН", Side: SideEnemy, HP: ptr(7), AC: ptr(12)},
		}, initiativeSequence(5, 15, 10))
		if order[0].ID != "гоблин-2" || order[1].ID != "гоблин-3" || order[2].ID != "гоблин" {
			t.Fatalf("ids по порядку ходов = %q, %q, %q", order[0].ID, order[1].ID, order[2].ID)
		}
	})

	t.Run("slugify: NBSP и Unicode-цифры", func(t *testing.T) {
		order := RollInitiative([]CombatantInput{
			{Name: "Гоблин\u00a0Воин", Side: SideEnemy, HP: ptr(7), AC: ptr(12)},
			{Name: "Гоблин ²", Side: SideEnemy, HP: ptr(7), AC: ptr(12)},
		}, initiativeSequence(10, 5))
		if order[0].ID != "гоблин-воин" {
			t.Errorf("NBSP: id = %q", order[0].ID)
		}
		if order[1].ID != "гоблин-²" {
			t.Errorf("²: id = %q", order[1].ID)
		}
	})

	t.Run("использует явный id", func(t *testing.T) {
		order := RollInitiative([]CombatantInput{
			{ID: "party_1", Name: "Варвар", Side: SideParty},
		}, initiativeSequence(8))
		if order[0].ID != "party_1" {
			t.Fatalf("id = %q", order[0].ID)
		}
	})
}

func sampleOrder() CombatOrder {
	return CombatOrder{
		Started: true,
		Round:   1,
		Current: 0,
		Acted:   true,
		Order: []CombatantEntry{
			{ID: "тварь", Name: "Тварь", Side: SideEnemy, Bonus: 4, Roll: 16, Total: 20},
			{ID: "микроб", Name: "Микроб", Side: SideParty, Bonus: 2, Roll: 8, Total: 10},
			{ID: "инокентий", Name: "Инокентий", Side: SideParty, Bonus: 2, Roll: 3, Total: 5},
		},
	}
}

func TestNextCombatant(t *testing.T) {
	t.Parallel()

	order := sampleOrder()

	t.Run("переходит к следующему участнику", func(t *testing.T) {
		next := NextCombatant(order, func(CombatantEntry) bool { return true })
		if next.Current != 1 || next.Round != 1 {
			t.Fatalf("получено %+v", next)
		}
	})

	t.Run("сбрасывает acted", func(t *testing.T) {
		next := NextCombatant(order, func(CombatantEntry) bool { return true })
		if next.Acted {
			t.Fatal("acted должен сброситься")
		}
	})

	t.Run("увеличивает раунд на замыкании", func(t *testing.T) {
		last := order
		last.Current = 2
		next := NextCombatant(last, func(CombatantEntry) bool { return true })
		if next.Current != 0 || next.Round != 2 {
			t.Fatalf("получено %+v", next)
		}
	})

	t.Run("пропускает мёртвых врагов", func(t *testing.T) {
		skipped := NextCombatant(CombatOrder{Started: true, Round: 1, Current: 1, Order: order.Order},
			func(entry CombatantEntry) bool { return entry.Name != "Инокентий" })
		if skipped.Current != 0 || skipped.Round != 2 {
			t.Fatalf("получено %+v", skipped)
		}
	})

	t.Run("пропускает партию с нулевым hp", func(t *testing.T) {
		extended := order
		extended.Order = append(append([]CombatantEntry(nil), order.Order...),
			CombatantEntry{ID: "труп", Name: "Труп", Side: SideParty, HP: ptr(0)})
		next := NextCombatant(extended, func(entry CombatantEntry) bool {
			return entry.HP == nil || *entry.HP > 0
		})
		if next.Current != 1 {
			t.Fatalf("получено %+v", next)
		}
	})

	t.Run("без живых порядок не меняется", func(t *testing.T) {
		next := NextCombatant(order, func(CombatantEntry) bool { return false })
		if next.Current != 0 || next.Round != 1 {
			t.Fatalf("получено %+v", next)
		}
	})

	t.Run("бой не начат — ничего не делает", func(t *testing.T) {
		next := NextCombatant(EmptyCombatOrder(), func(CombatantEntry) bool { return true })
		if next.Current != -1 || next.Started {
			t.Fatalf("получено %+v", next)
		}
	})

	t.Run("испорченный current не паникует", func(t *testing.T) {
		broken := order
		broken.Current = -5
		next := NextCombatant(broken, func(CombatantEntry) bool { return true })
		if next.Current < 0 || next.Current >= len(next.Order) {
			t.Fatalf("current = %d", next.Current)
		}
	})

	t.Run("снимает dodge с того, чей ход начинается", func(t *testing.T) {
		dodging := order
		dodging.Order = append([]CombatantEntry(nil), order.Order...)
		dodging.Order[1].Dodging = true
		next := NextCombatant(dodging, func(CombatantEntry) bool { return true })
		if next.Current != 1 || next.Order[1].Dodging {
			t.Fatalf("получено %+v", next)
		}
	})

	t.Run("сохраняет dodge у остальных", func(t *testing.T) {
		dodging := order
		dodging.Order = append([]CombatantEntry(nil), order.Order...)
		for i := range dodging.Order {
			dodging.Order[i].Dodging = true
		}
		next := NextCombatant(dodging, func(CombatantEntry) bool { return true })
		if next.Order[1].Dodging || !next.Order[2].Dodging {
			t.Fatalf("dodge: %v, %v", next.Order[1].Dodging, next.Order[2].Dodging)
		}
	})
}

func TestEmptyCombatOrder(t *testing.T) {
	t.Parallel()

	order := EmptyCombatOrder()
	if order.Acted || order.Started || order.Current != -1 || order.Round != 1 || len(order.Order) != 0 {
		t.Fatalf("получено %+v", order)
	}
}

func TestWeaponDamageDice(t *testing.T) {
	t.Parallel()

	inventory := []string{"короткий меч (1d6)", "факел", "лук длинный (1d8)", "зелье лечения (1d4)"}

	cases := []struct {
		items  []string
		weapon string
		want   string
		found  bool
	}{
		{inventory, "меч", "1d6", true},
		{inventory, "лук", "1d8", true},
		{inventory, "", "1d6", true},
		{[]string{"магическая палочка"}, "палочка", "", false},
		{[]string{"факел", "верёвка"}, "", "", false},
		{nil, "", "", false},
		{inventory, "нет такого", "", false},
		{[]string{"Короткий Меч (1d6)"}, "меч", "1d6", true},
	}
	for _, tc := range cases {
		got, found := WeaponDamageDice(tc.items, tc.weapon)
		if got != tc.want || found != tc.found {
			t.Errorf("WeaponDamageDice(%v, %q) = (%q, %v), want (%q, %v)", tc.items, tc.weapon, got, found, tc.want, tc.found)
		}
	}
}

func TestCombatSerialization(t *testing.T) {
	t.Parallel()

	order := sampleOrder()
	order.Order[0].HP = ptr(0)
	order.Order[1].MaxHP = ptr(12)
	order.Order[2].Dodging = true
	order.Order[2].AC = ptr(13)

	serialized := SerializeCombatOrder(order)
	data, err := json.Marshal(serialized)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	restored, ok := DeserializeCombatOrder(raw)
	if !ok {
		t.Fatal("не разобрался собственный снимок")
	}
	if !reflect.DeepEqual(restored, order) {
		t.Fatalf("round-trip:\n got %+v\nwant %+v", restored, order)
	}

	// hp:0 должен сохраняться, а ложный dodging — не появляться в JSON.
	if order.Order[0].HP == nil || *order.Order[0].HP != 0 {
		t.Fatalf("hp=0 потерялся: %+v", order.Order[0])
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	entries := fields["order"].([]any)
	first := entries[0].(map[string]any)
	if _, exists := first["dodging"]; exists {
		t.Fatalf("dodging=false не должен попадать в JSON: %v", first)
	}
	if first["hp"] != float64(0) {
		t.Fatalf("hp=0 не сериализован: %v", first)
	}
}

func TestDeserializeCombatRejectsGarbage(t *testing.T) {
	t.Parallel()

	for _, data := range []any{
		nil,
		"строка",
		42,
		map[string]any{},
		map[string]any{"id": "x", "name": "X", "side": "neutral", "bonus": 0.0, "roll": 1.0, "total": 1.0},
		map[string]any{"id": "x", "name": "X", "side": "party", "bonus": 0.0, "roll": 1.0},
	} {
		if _, ok := DeserializeCombatant(data); ok {
			t.Errorf("DeserializeCombatant(%v) должен отклоняться", data)
		}
	}

	if _, ok := DeserializeCombatOrder(map[string]any{"started": true, "round": 1.0, "current": 0.0, "acted": false}); ok {
		t.Error("порядок без order должен отклоняться")
	}
	if _, ok := DeserializeCombatOrder(map[string]any{
		"started": true, "round": 1.0, "current": 0.0, "acted": false,
		"order": []any{map[string]any{"id": ""}},
	}); ok {
		t.Error("битый участник должен отклонять весь порядок")
	}

	validEntry := map[string]any{"id": "dan", "name": "Дэн", "side": "party", "bonus": 0.0, "roll": 10.0, "total": 10.0}
	for _, current := range []float64{-5, -2, 1, 99} {
		if _, ok := DeserializeCombatOrder(map[string]any{
			"started": true, "round": 1.0, "current": current, "acted": false,
			"order": []any{validEntry},
		}); ok {
			t.Errorf("current=%v должен отклоняться", current)
		}
	}
	// Пустой порядок допускает только current=-1.
	if _, ok := DeserializeCombatOrder(map[string]any{
		"started": false, "round": 1.0, "current": -1.0, "acted": false, "order": []any{},
	}); !ok {
		t.Error("пустой порядок с current=-1 должен приниматься")
	}
}
