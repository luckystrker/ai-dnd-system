package engine

import "testing"

func TestXPThresholds(t *testing.T) {
	t.Parallel()

	cases := map[int]int{
		1:  0,
		2:  300,
		5:  6500,
		10: 64000,
		20: 355000,
		0:  355000, // TS: XP_THRESHOLDS[0] undefined → порог 20
		25: 355000,
	}
	for level, want := range cases {
		if got := XPForLevel(level); got != want {
			t.Errorf("XPForLevel(%d) = %d, want %d", level, got, want)
		}
	}
}

func TestLevelForXP(t *testing.T) {
	t.Parallel()

	cases := map[int]int{
		0:      1,
		299:    1,
		300:    2,
		899:    2,
		900:    3,
		2700:   4,
		355000: 20,
		999999: 20,
	}
	for xp, want := range cases {
		if got := LevelForXP(xp); got != want {
			t.Errorf("LevelForXP(%d) = %d, want %d", xp, got, want)
		}
	}
}

func TestQuestXPPerCharacter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		difficulty QuestDifficulty
		level      int
		want       int
	}{
		{DifficultyEasy, 1, 36},    // 300 * 0.12
		{DifficultyMedium, 1, 75},  // 300 * 0.25
		{DifficultyHard, 1, 120},   // 300 * 0.4
		{DifficultyMedium, 4, 950}, // (6500-2700) * 0.25
		{DifficultyEasy, 0, 36},    // уровень клампится в 1
	}
	for _, tc := range cases {
		if got := QuestXPPerCharacter(tc.difficulty, tc.level); got != tc.want {
			t.Errorf("QuestXPPerCharacter(%q, %d) = %d, want %d", tc.difficulty, tc.level, got, tc.want)
		}
	}
}

func TestQuestGoldRange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		difficulty QuestDifficulty
		level      int
		want       [2]int
	}{
		{DifficultyEasy, 1, [2]int{25, 75}},
		{DifficultyMedium, 3, [2]int{150, 450}}, // ×1.5
		{DifficultyHard, 5, [2]int{1000, 3000}}, // ×2
		{DifficultyEasy, 0, [2]int{25, 75}},     // уровень ниже 1 не множит
	}
	for _, tc := range cases {
		if got := QuestGoldRange(tc.difficulty, tc.level); got != tc.want {
			t.Errorf("QuestGoldRange(%q, %d) = %v, want %v", tc.difficulty, tc.level, got, tc.want)
		}
	}
}

func TestMiddleOf(t *testing.T) {
	t.Parallel()

	if got := MiddleOf([2]int{25, 75}); got != 50 {
		t.Errorf("MiddleOf([25 75]) = %d", got)
	}
	if got := MiddleOf([2]int{100, 300}); got != 200 {
		t.Errorf("MiddleOf([100 300]) = %d", got)
	}
	if got := MiddleOf([2]int{1, 2}); got != 2 { // Math.round(1.5)
		t.Errorf("MiddleOf([1 2]) = %d", got)
	}
}

func TestClassHitDieFor(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"barbarian": 12,
		"Воин":      10,
		"боец":      10,
		"волшебник": 6,
		"маг":       6,
		"плут":      8,
		"unknown":   8,
	}
	for class, want := range cases {
		if got := ClassHitDieFor(class); got != want {
			t.Errorf("ClassHitDieFor(%q) = %d, want %d", class, got, want)
		}
	}
}

func TestStatModifier(t *testing.T) {
	t.Parallel()

	cases := map[int]int{10: 0, 15: 2, 20: 5, 8: -1, 7: -2, 1: -5}
	for stat, want := range cases {
		if got := StatModifier(stat); got != want {
			t.Errorf("StatModifier(%d) = %d, want %d", stat, got, want)
		}
	}
}
