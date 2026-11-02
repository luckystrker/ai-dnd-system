package engine

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// SkillAbilityMap — английские навыки → аббревиатура характеристики.
var SkillAbilityMap = map[string]string{
	"athletics":       "str",
	"acrobatics":      "dex",
	"sleight_of_hand": "dex",
	"stealth":         "dex",
	"arcana":          "int",
	"history":         "int",
	"investigation":   "int",
	"nature":          "int",
	"religion":        "int",
	"animal_handling": "wis",
	"insight":         "wis",
	"medicine":        "wis",
	"perception":      "wis",
	"survival":        "wis",
	"deception":       "cha",
	"intimidation":    "cha",
	"performance":     "cha",
	"persuasion":      "cha",
}

// skillRuAliases — русские названия навыков: агент часто передаёт
// «Восприятие» или «Восприятие (Perception)».
var skillRuAliases = map[string]string{
	"атлетика":          "athletics",
	"акробатика":        "acrobatics",
	"ловкость рук":      "sleight_of_hand",
	"скрытность":        "stealth",
	"магия":             "arcana",
	"аркана":            "arcana",
	"история":           "history",
	"расследование":     "investigation",
	"природа":           "nature",
	"религия":           "religion",
	"уход за животными": "animal_handling",
	"проницательность":  "insight",
	"медицина":          "medicine",
	"восприятие":        "perception",
	"выживание":         "survival",
	"обман":             "deception",
	"запугивание":       "intimidation",
	"выступление":       "performance",
	"убеждение":         "persuasion",
}

var (
	parenPattern     = regexp.MustCompile(`\(([^)]+)\)`)
	spaceDashPattern = regexp.MustCompile(`[\s\p{Z}-]+`)
)

// ResolveSkillAbility разрешает название навыка в аббревиатуру характеристики.
// Понимает английские и русские названия, включая «Восприятие (Perception)».
// Неизвестный навык консервативно разрешается в "str".
func ResolveSkillAbility(skill string) string {
	name := strings.ToLower(strings.TrimSpace(skill))
	candidates := []string{name}
	if paren := parenPattern.FindStringSubmatch(name); paren != nil {
		candidates = append(candidates, strings.TrimSpace(paren[1]))
	}
	for _, candidate := range candidates {
		underscored := spaceDashPattern.ReplaceAllString(candidate, "_")
		mapped, ok := SkillAbilityMap[underscored]
		if !ok {
			mapped, ok = skillRuAliases[candidate]
		}
		if ok {
			if canonical, exists := SkillAbilityMap[mapped]; exists {
				return canonical
			}
			return mapped
		}
	}
	return "str"
}

// AbilityAliases — полные и сокращённые названия характеристик: листы
// хранят strength, dexterity, ...
var AbilityAliases = map[string]string{
	"strength":     "str",
	"str":          "str",
	"dexterity":    "dex",
	"dex":          "dex",
	"constitution": "con",
	"con":          "con",
	"intelligence": "int",
	"int":          "int",
	"wisdom":       "wis",
	"wis":          "wis",
	"charisma":     "cha",
	"cha":          "cha",
}

// AbilityScore достаёт числовое значение характеристики по любому написанию.
// Строковые значения приводятся к числу; нечисловые игнорируются. Если в
// листе есть несколько написаний одной характеристики, выбирается первое по
// алфавиту ключей — порядок вставки map в Go не сохраняется, поэтому нужен
// стабильный детерминированный выбор (в TS побеждал порядок вставки).
func AbilityScore(stats map[string]any, ability string) (int, bool) {
	key := abilityAliasesOf(ability)
	keys := make([]string, 0, len(stats))
	for rawKey := range stats {
		keys = append(keys, rawKey)
	}
	sort.Strings(keys)
	for _, rawKey := range keys {
		if abilityAliasesOf(rawKey) != key {
			continue
		}
		parsed, ok := numericValue(stats[rawKey])
		if !ok {
			continue
		}
		// Зажимаем в диапазон int32: конечные 1e300 иначе переполняют int.
		parsed = math.Max(-math.MaxInt32, math.Min(float64(math.MaxInt32), parsed))
		return int(parsed), true // Math.trunc
	}
	return 0, false
}

func abilityAliasesOf(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if alias, ok := AbilityAliases[normalized]; ok {
		return alias
	}
	return normalized
}

// AbilityModifier — модификатор характеристики: floor((score-10)/2).
// Без значения — 0 (эквивалент счёта 10).
func AbilityModifier(stats map[string]any, ability string) int {
	score := 10
	if value, ok := AbilityScore(stats, ability); ok {
		score = value
	}
	return int(math.Floor(float64(score-10) / 2))
}

// ProficiencyBonus — бонус мастерства: +2 на 1-4, +3 на 5-8, +4 на 9-12,
// +5 на 13-16, +6 на 17-20. Невалидный уровень трактуется как 1.
func ProficiencyBonus(level int) int {
	if level < 1 {
		level = 1
	}
	return (level-1)/4 + 2
}

// SkillCheck — проверка навыка d20 + модификатор против DC. Натуральная 20 —
// всегда успех, натуральная 1 — всегда провал. Бросает два d20 всегда (второй
// нужен только при advantage/disadvantage), как TS-версия.
func SkillCheck(stats map[string]any, skill string, difficulty int, advantage *bool, random RandomSource) CheckResult {
	source := randomOr(random)
	ability := ResolveSkillAbility(skill)
	modifier := AbilityModifier(stats, ability)
	first := randomInt(20, source)
	second := randomInt(20, source)
	roll := first
	if advantage != nil {
		if *advantage {
			roll = max(first, second)
		} else {
			roll = min(first, second)
		}
	}
	total := roll + modifier
	naturalSuccess := roll == 20
	naturalFailure := roll == 1
	return CheckResult{
		Roll:           roll,
		Modifier:       modifier,
		Total:          total,
		Difficulty:     difficulty,
		Success:        naturalSuccess || (!naturalFailure && total >= difficulty),
		Margin:         total - difficulty,
		Ability:        ability,
		NaturalSuccess: naturalSuccess,
		NaturalFailure: naturalFailure,
	}
}

// Environment — контекст окружения: фрагмент кампании, влияющий на броски.
type Environment struct {
	TimeOfDay string
	Weather   string
}

// EnvironmentModifiers — итоговое преимущество/помеха и причины для игрока.
type EnvironmentModifiers struct {
	Advantage *bool
	Reasons   []string
}

var visualSkills = map[string]bool{"perception": true, "investigation": true, "survival": true}

// normalizeSkillKey нормализует название навыка в английский ключ.
func normalizeSkillKey(skill string) string {
	name := strings.ToLower(strings.TrimSpace(skill))
	if paren := parenPattern.FindStringSubmatch(name); paren != nil {
		inner := spaceDashPattern.ReplaceAllString(strings.TrimSpace(paren[1]), "_")
		if _, ok := SkillAbilityMap[inner]; ok {
			return inner
		}
	}
	underscored := spaceDashPattern.ReplaceAllString(name, "_")
	if _, ok := SkillAbilityMap[underscored]; ok {
		return underscored
	}
	if alias, ok := skillRuAliases[name]; ok {
		return alias
	}
	if alias, ok := skillRuAliases[underscored]; ok {
		return alias
	}
	return underscored
}

var (
	reducedVisibility = []string{"туман", "мгла", "дым", "пелена", "метель", "fog", "mist", "haze", "smoke", "blizzard"}
	strongWind        = []string{"ветер", "ветр", "шторм", "буря", "ураган", "storm", "wind", "gale", "hurricane"}
)

func weatherMatches(weather string, patterns []string) bool {
	lower := strings.ToLower(weather)
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// CombineAdvantage — правило 5e: преимущество и помеха взаимно отменяются,
// иначе берётся единственное влияние; nil-слои игнорируются.
func CombineAdvantage(layers ...*bool) *bool {
	var hasAdvantage, hasDisadvantage bool
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		if *layer {
			hasAdvantage = true
		} else {
			hasDisadvantage = true
		}
	}
	if hasAdvantage && hasDisadvantage {
		return nil
	}
	if hasAdvantage {
		value := true
		return &value
	}
	if hasDisadvantage {
		value := false
		return &value
	}
	return nil
}

// EnvironmentModifiersForCheck — влияние времени суток и погоды на проверку.
// Ночью — помеха на зрительные проверки и преимущество на скрытность;
// плохая видимость — помеха на зрительные проверки.
func EnvironmentModifiersForCheck(env Environment, skill string) EnvironmentModifiers {
	result := EnvironmentModifiers{}
	key := normalizeSkillKey(skill)
	isVisual := visualSkills[key]

	if env.TimeOfDay == "night" {
		if isVisual {
			disadvantage := false
			result.Advantage = CombineAdvantage(result.Advantage, &disadvantage)
			result.Reasons = append(result.Reasons, "ночь: помеха на зрительную проверку")
		}
		if key == "stealth" {
			advantage := true
			result.Advantage = CombineAdvantage(result.Advantage, &advantage)
			result.Reasons = append(result.Reasons, "ночь: преимущество на скрытность")
		}
	}

	if isVisual && env.Weather != "" && weatherMatches(env.Weather, reducedVisibility) {
		disadvantage := false
		result.Advantage = CombineAdvantage(result.Advantage, &disadvantage)
		result.Reasons = append(result.Reasons, env.Weather+": плохая видимость, помеха на зрительную проверку")
	}

	return result
}

// EnvironmentModifiersForAttack — сильный ветер даёт помеху на дальние атаки.
func EnvironmentModifiersForAttack(env Environment, attackType string) EnvironmentModifiers {
	result := EnvironmentModifiers{}
	if attackType == "ranged" && env.Weather != "" && weatherMatches(env.Weather, strongWind) {
		disadvantage := false
		result.Advantage = &disadvantage
		result.Reasons = append(result.Reasons, env.Weather+": сильный ветер, помеха на дальнюю атаку")
	}
	return result
}
