package engine

import (
	"math"
	"strings"
)

// QuestDifficulty — сложность квеста.
type QuestDifficulty string

const (
	DifficultyEasy   QuestDifficulty = "easy"
	DifficultyMedium QuestDifficulty = "medium"
	DifficultyHard   QuestDifficulty = "hard"
)

// XPThresholds — накопительные пороги XP на уровень (упрощённый D&D 5e).
var XPThresholds = [...]int{
	0, // уровень 1
	0, 300, 900, 2700, 6500, 14000, 23000, 34000, 48000, 64000,
	85000, 100000, 120000, 140000, 165000, 195000, 225000, 265000, 305000, 355000,
}

// XPForLevel — порог XP для уровня; для level < 1 или level >= 20 возвращает
// порог 20 (как `XP_THRESHOLDS[level] ?? XP_THRESHOLDS[20]` в TS).
func XPForLevel(level int) int {
	if level < 1 || level >= len(XPThresholds) {
		return XPThresholds[20]
	}
	return XPThresholds[level]
}

// LevelForXP — какой уровень соответствует накопленному XP (максимум 20).
func LevelForXP(xp int) int {
	level := 1
	for candidate := 1; candidate < len(XPThresholds); candidate++ {
		if xp >= XPThresholds[candidate] {
			level = candidate
		}
	}
	return level
}

// QuestXPShare — доля пути до следующего уровня, которую приносит квест.
var QuestXPShare = map[QuestDifficulty]float64{
	DifficultyEasy:   0.12,
	DifficultyMedium: 0.25,
	DifficultyHard:   0.4,
}

// QuestXPPerCharacter — XP каждому персонажу за квест по сложности и уровню.
func QuestXPPerCharacter(difficulty QuestDifficulty, partyLevel int) int {
	clamped := min(19, max(1, partyLevel))
	span := XPForLevel(clamped+1) - XPForLevel(clamped)
	return max(10, int(math.Round(float64(span)*QuestXPShare[difficulty])))
}

// QuestGoldBase — базовый диапазон золота на партию по сложности.
var QuestGoldBase = map[QuestDifficulty][2]int{
	DifficultyEasy:   {25, 75},
	DifficultyMedium: {100, 300},
	DifficultyHard:   {500, 1500},
}

// QuestGoldRange — диапазон золота: базовый × множитель уровня партии.
func QuestGoldRange(difficulty QuestDifficulty, partyLevel int) [2]int {
	base := QuestGoldBase[difficulty]
	multiplier := 1 + float64(max(0, partyLevel-1))*0.25
	return [2]int{
		int(math.Round(float64(base[0]) * multiplier)),
		int(math.Round(float64(base[1]) * multiplier)),
	}
}

// MiddleOf — середина диапазона (для выдачи без броска).
func MiddleOf(span [2]int) int {
	return int(math.Round(float64(span[0]+span[1]) / 2))
}

// ClassHitDie — кость хитов по классу.
var ClassHitDie = map[string]int{
	"barbarian": 12,
	"fighter":   10,
	"paladin":   10,
	"ranger":    10,
	"artificer": 8,
	"bard":      8,
	"cleric":    8,
	"druid":     8,
	"monk":      8,
	"rogue":     8,
	"warlock":   8,
	"wizard":    6,
	"sorcerer":  6,
}

// classAliases — русские названия классов → канонический ключ.
var classAliases = map[string]string{
	"варвар":       "barbarian",
	"воин":         "fighter",
	"боец":         "fighter",
	"паладин":      "paladin",
	"следопыт":     "ranger",
	"рейнджер":     "ranger",
	"изобретатель": "artificer",
	"бард":         "bard",
	"жрец":         "cleric",
	"клирик":       "cleric",
	"друид":        "druid",
	"монах":        "monk",
	"плут":         "rogue",
	"вор":          "rogue",
	"колдун":       "warlock",
	"чародей":      "sorcerer",
	"волшебник":    "wizard",
	"маг":          "wizard",
}

// ClassHitDieFor — кость хитов класса; неизвестные классы получают d8.
func ClassHitDieFor(characterClass string) int {
	key := classAliases[trimLower(characterClass)]
	if key == "" {
		key = trimLower(characterClass)
	}
	if die, ok := ClassHitDie[key]; ok {
		return die
	}
	return 8
}

// StatModifier — модификатор характеристики: floor((stat-10)/2).
func StatModifier(stat int) int {
	return int(math.Floor(float64(stat-10) / 2))
}

func trimLower(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
