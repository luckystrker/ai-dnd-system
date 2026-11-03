package engine

import "testing"

func TestCombineAdvantage(t *testing.T) {
	t.Parallel()

	if got := CombineAdvantage(nil, nil); got != nil {
		t.Errorf("все nil: %v, want nil", got)
	}
	if got := CombineAdvantage(); got != nil {
		t.Errorf("без слоёв: %v, want nil", got)
	}
	if got := CombineAdvantage(nil, ptr(true)); got == nil || !*got {
		t.Errorf("единственное преимущество: %v", got)
	}
	if got := CombineAdvantage(ptr(false), nil); got == nil || *got {
		t.Errorf("единственная помеха: %v", got)
	}
	if got := CombineAdvantage(ptr(true), ptr(false)); got != nil {
		t.Errorf("преимущество + помеха: %v, want nil", got)
	}
	if got := CombineAdvantage(ptr(false), ptr(true)); got != nil {
		t.Errorf("помеха + преимущество: %v, want nil", got)
	}
	if got := CombineAdvantage(ptr(true), ptr(true)); got == nil || !*got {
		t.Errorf("два преимущества: %v", got)
	}
	if got := CombineAdvantage(ptr(false), ptr(false)); got == nil || *got {
		t.Errorf("две помехи: %v", got)
	}
}

func TestEnvironmentModifiersForCheck(t *testing.T) {
	t.Parallel()

	t.Run("ночь: помеха на Восприятие", func(t *testing.T) {
		mod := EnvironmentModifiersForCheck(Environment{TimeOfDay: "night"}, "Perception")
		if mod.Advantage == nil || *mod.Advantage || len(mod.Reasons) == 0 {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("ночь: преимущество на Скрытность", func(t *testing.T) {
		mod := EnvironmentModifiersForCheck(Environment{TimeOfDay: "night"}, "Stealth")
		if mod.Advantage == nil || !*mod.Advantage {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("ночь + туман: две помехи остаются помехой", func(t *testing.T) {
		mod := EnvironmentModifiersForCheck(Environment{TimeOfDay: "night", Weather: "туман"}, "Perception")
		if mod.Advantage == nil || *mod.Advantage || len(mod.Reasons) == 0 {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("день и ясно — без модификатора", func(t *testing.T) {
		mod := EnvironmentModifiersForCheck(Environment{TimeOfDay: "day", Weather: "ясно"}, "Investigation")
		if mod.Advantage != nil || len(mod.Reasons) != 0 {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("туман: помеха на Выживание", func(t *testing.T) {
		mod := EnvironmentModifiersForCheck(Environment{Weather: "туман"}, "Survival")
		if mod.Advantage == nil || *mod.Advantage {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("русские названия навыков", func(t *testing.T) {
		mod := EnvironmentModifiersForCheck(Environment{TimeOfDay: "night"}, "Восприятие")
		if mod.Advantage == nil || *mod.Advantage {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("ночь не влияет на Атлетику", func(t *testing.T) {
		mod := EnvironmentModifiersForCheck(Environment{TimeOfDay: "night"}, "Athletics")
		if mod.Advantage != nil {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("пустое окружение — без модификатора", func(t *testing.T) {
		mod := EnvironmentModifiersForCheck(Environment{}, "Perception")
		if mod.Advantage != nil || len(mod.Reasons) != 0 {
			t.Fatalf("получено %+v", mod)
		}
	})
}

func TestEnvironmentModifiersForAttack(t *testing.T) {
	t.Parallel()

	t.Run("сильный ветер: помеха на дальнюю атаку", func(t *testing.T) {
		mod := EnvironmentModifiersForAttack(Environment{Weather: "сильный ветер"}, "ranged")
		if mod.Advantage == nil || *mod.Advantage || len(mod.Reasons) == 0 {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("шторм: помеха на дальнюю атаку", func(t *testing.T) {
		mod := EnvironmentModifiersForAttack(Environment{Weather: "шторм"}, "ranged")
		if mod.Advantage == nil || *mod.Advantage {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("ближний бой не страдает от ветра", func(t *testing.T) {
		mod := EnvironmentModifiersForAttack(Environment{Weather: "сильный ветер"}, "melee")
		if mod.Advantage != nil {
			t.Fatalf("получено %+v", mod)
		}
	})

	t.Run("ясно — без модификатора", func(t *testing.T) {
		mod := EnvironmentModifiersForAttack(Environment{Weather: "ясно"}, "ranged")
		if mod.Advantage != nil {
			t.Fatalf("получено %+v", mod)
		}
	})
}
