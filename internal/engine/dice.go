// Package engine — чистая механика D&D 5e без eve-рантайма, SQLite и тулов:
// броски и нотации, проверки навыков, окружение, порядок ходов в бою и
// таблицы наград. Порт agent/lib/engine/{dnd5e,combat}.ts и agent/lib/rewards.ts.
//
// Источник случайности инжектируется (RandomSource), поэтому тесты
// детерминированы. «Добрый» d20 (MakeLuckyRandom) — решение владельца:
// применяется только к skill_check и сохраняется 1:1.
package engine

import (
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RandomSource возвращает число в [0, 1), как Math.random.
type RandomSource func() float64

// DefaultRandom — обычный источник для продакшена (Math.random в TS).
var DefaultRandom RandomSource = rand.Float64

func randomOr(random RandomSource) RandomSource {
	if random == nil {
		return DefaultRandom
	}
	return random
}

// DiceResult — результат броска нескольких одинаковых костей.
type DiceResult struct {
	Rolls []int
	Total int
	// Pairs — оба значения каждой d20 при advantage/disadvantage
	// (выбранное первым, отброшенное вторым). nil без advantage.
	Pairs [][2]int
}

// DiceGroup — одна группа кубиков в нотации («2d6» в «2d6+1d8+3»).
// Модификаторы keep/drop — указатели: nil означает «не задан», а kh0 должен
// отличаться от отсутствия модификатора и падать на валидации.
type DiceGroup struct {
	Count    int
	Sides    int
	KeepHigh *int
	KeepLow  *int
	DropHigh *int
	DropLow  *int
	Explode  bool
}

// ParsedNotation — распарсенная нотация: группы и плоский модификатор.
type ParsedNotation struct {
	Groups   []DiceGroup
	Modifier int
}

// DiceGroupResult — результат одной группы.
type DiceGroupResult struct {
	// Spec — исходная спецификация группы, например «2d6» или «4d6kh3».
	Spec string
	// Rolls — учитываемые значения (после keep/drop; при advantage — выбранные).
	Rolls []int
	// Dropped — отброшенные keep/drop значения (показываются игроку).
	Dropped []int
	// Pairs — оба значения d20 при advantage/disadvantage.
	Pairs    [][2]int
	Subtotal int
}

// NotationResult — полный результат броска по нотации.
type NotationResult struct {
	Notation string
	Groups   []DiceGroupResult
	Modifier int
	Total    int
}

// CheckResult — результат проверки навыка.
type CheckResult struct {
	Roll           int
	Modifier       int
	Total          int
	Difficulty     int
	Success        bool
	Margin         int
	Ability        string
	NaturalSuccess bool
	NaturalFailure bool
}

// Настройки «доброго» псевдорандома (только для skill_check).
const (
	// LuckyRerollChance — шанс перебросить низкий d20 и взять большее.
	LuckyRerollChance = 0.5
	// LowRollThreshold — грань ниже этого значения считается низкой.
	LowRollThreshold = 10
	// LowStreakWindow — сколько последних d20 учитывается в серии неудач.
	LowStreakWindow = 2
	// LowStreakFloor — минимальная грань следующего броска при серии.
	LowStreakFloor = 11
)

func randomInt(sides int, random RandomSource) int {
	return int(math.Floor(random()*float64(sides))) + 1
}

// MakeLuckyRandom — «добрый» источник случайности для d20: слегка сдвигает
// распределение вверх и ломает серии низких бросков. Применяется только к
// skill_check; боевые броски, урон и инициатива остаются на честном рандоме.
func MakeLuckyRandom(base RandomSource, recentLowStreak bool) RandomSource {
	source := randomOr(base)
	return func() float64 {
		r := source()
		if recentLowStreak {
			// floor = LowStreakFloor на d20 → r_min = (floor-1)/20.
			minUnit := float64(LowStreakFloor-1) / 20
			return math.Max(r, minUnit+source()*(1-minUnit))
		}
		face := int(math.Floor(r*20)) + 1
		if face < LowRollThreshold && source() < LuckyRerollChance {
			return math.Max(r, source())
		}
		return r
	}
}

// IsLowStreak — true, если последние LowStreakWindow граней d20 все ниже
// LowRollThreshold. Пустая/короткая история — не серия (безопасный дефолт
// для старых сессий без diceHistory).
func IsLowStreak(history []int) bool {
	if len(history) < LowStreakWindow {
		return false
	}
	recent := history[len(history)-LowStreakWindow:]
	for _, value := range recent {
		if value >= LowRollThreshold {
			return false
		}
	}
	return true
}

// RollDice бросает count костей с sides гранями. advantage/disadvantage
// действует только на d20: каждая кость бросается дважды, берётся max/min.
func RollDice(sides, count int, random RandomSource, advantage *bool) (DiceResult, error) {
	if sides < 1 {
		return DiceResult{}, fmt.Errorf("dice must have at least one side")
	}
	if count < 1 {
		return DiceResult{}, fmt.Errorf("dice count must be at least one")
	}
	if count > 100 {
		return DiceResult{}, fmt.Errorf("dice count must not exceed 100")
	}
	source := randomOr(random)
	useAdv := advantage != nil && sides == 20
	result := DiceResult{}
	if useAdv {
		result.Pairs = make([][2]int, 0, count)
	}
	for i := 0; i < count; i++ {
		if useAdv {
			a := randomInt(sides, source)
			b := randomInt(sides, source)
			pick, drop := a, b
			if *advantage {
				pick, drop = max(a, b), min(a, b)
			} else {
				pick, drop = min(a, b), max(a, b)
			}
			result.Rolls = append(result.Rolls, pick)
			result.Pairs = append(result.Pairs, [2]int{pick, drop})
		} else {
			result.Rolls = append(result.Rolls, randomInt(sides, source))
		}
	}
	for _, roll := range result.Rolls {
		result.Total += roll
	}
	return result, nil
}

// explodeLimit — лимит рекурсивных взрывных костей на группу.
const explodeLimit = 10

// groupSpec — строковое представление группы с модификаторами («4d6kh3»).
func groupSpec(group DiceGroup) string {
	spec := fmt.Sprintf("%dd%d", group.Count, group.Sides)
	if group.KeepHigh != nil {
		spec += fmt.Sprintf("kh%d", *group.KeepHigh)
	}
	if group.KeepLow != nil {
		spec += fmt.Sprintf("kl%d", *group.KeepLow)
	}
	if group.DropHigh != nil {
		spec += fmt.Sprintf("dh%d", *group.DropHigh)
	}
	if group.DropLow != nil {
		spec += fmt.Sprintf("dl%d", *group.DropLow)
	}
	if group.Explode {
		spec += "!"
	}
	return spec
}

var (
	notationAllowed = regexp.MustCompile(`^[0-9dkhl!+\-]+$`)
	notationTerm    = regexp.MustCompile(`[+-]?[^+-]+`)
	notationGroup   = regexp.MustCompile(`^([0-9]*)d([0-9]+)((kh[0-9]+|kl[0-9]+|dh[0-9]+|dl[0-9]+|!)*)$`)
	notationMod     = regexp.MustCompile(`kh[0-9]+|kl[0-9]+|dh[0-9]+|dl[0-9]+|!`)
)

// ParseDiceNotation разбирает нотацию: «4d20», «2d6+1d8+3», «1d20+5», «d8»,
// «2d4-1», «4d6kh3», «4d6dl1», «8d6!».
func ParseDiceNotation(spec string) (ParsedNotation, error) {
	normalized := strings.ToLower(strings.Join(strings.Fields(spec), ""))
	if normalized == "" {
		return ParsedNotation{}, fmt.Errorf("Пустая нотация броска «%s». Пример: «2d6+1d8+3»", spec)
	}
	if !notationAllowed.MatchString(normalized) {
		return ParsedNotation{}, fmt.Errorf("Недопустимые символы в нотации «%s». Допускаются цифры, d, +, -, kh/kl/dh/dl, !", spec)
	}

	terms := notationTerm.FindAllString(normalized, -1)
	if len(terms) == 0 || strings.Join(terms, "") != normalized {
		return ParsedNotation{}, fmt.Errorf("Неправильная нотация броска «%s». Пример: «2d6+1d8+3»", spec)
	}

	parsed := ParsedNotation{}
	for _, term := range terms {
		sign := 1
		body := term
		if strings.HasPrefix(term, "-") {
			sign = -1
			body = term[1:]
		} else if strings.HasPrefix(term, "+") {
			body = term[1:]
		}

		groupMatch := notationGroup.FindStringSubmatch(body)
		if groupMatch != nil {
			if sign == -1 {
				return ParsedNotation{}, fmt.Errorf("Группа костей не может быть отрицательной в «%s».", spec)
			}
			count := 1
			if groupMatch[1] != "" {
				parsedCount, err := strconv.Atoi(groupMatch[1])
				if err != nil {
					return ParsedNotation{}, fmt.Errorf("Неправильная нотация броска «%s» (терм «%s» не распознан).", spec, body)
				}
				count = parsedCount
			}
			sides, err := strconv.Atoi(groupMatch[2])
			if err != nil {
				return ParsedNotation{}, fmt.Errorf("Неправильная нотация броска «%s» (терм «%s» не распознан).", spec, body)
			}
			if count < 1 {
				return ParsedNotation{}, fmt.Errorf("Количество костей должно быть ≥ 1 в «%s».", spec)
			}
			if sides < 1 {
				return ParsedNotation{}, fmt.Errorf("Количество граней должно быть ≥ 1 в «%s».", spec)
			}
			group := DiceGroup{Count: count, Sides: sides}
			for _, token := range notationMod.FindAllString(groupMatch[3], -1) {
				if token == "!" {
					group.Explode = true
					continue
				}
				value, _ := strconv.Atoi(token[2:])
				switch token[:2] {
				case "kh":
					group.KeepHigh = &value
				case "kl":
					group.KeepLow = &value
				case "dh":
					group.DropHigh = &value
				case "dl":
					group.DropLow = &value
				}
			}
			if err := validateKeepDrop(group, spec); err != nil {
				return ParsedNotation{}, err
			}
			parsed.Groups = append(parsed.Groups, group)
			continue
		}

		if isDigits(body) {
			value, err := strconv.Atoi(body)
			if err != nil {
				return ParsedNotation{}, fmt.Errorf("Неправильная нотация броска «%s» (терм «%s» не распознан).", spec, body)
			}
			parsed.Modifier += sign * value
			continue
		}
		return ParsedNotation{}, fmt.Errorf("Неправильная нотация броска «%s» (терм «%s» не распознан).", spec, body)
	}

	if len(parsed.Groups) == 0 {
		return ParsedNotation{}, fmt.Errorf("В нотации «%s» нет костей. Пример: «2d6+1d8+3».", spec)
	}
	if len(parsed.Groups) > 5 {
		return ParsedNotation{}, fmt.Errorf("Слишком много групп костей в «%s» (максимум 5).", spec)
	}
	totalCount := 0
	for _, group := range parsed.Groups {
		totalCount += group.Count
	}
	if totalCount > 100 {
		return ParsedNotation{}, fmt.Errorf("Слишком много костей в «%s»: %d (максимум 100).", spec, totalCount)
	}
	return parsed, nil
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// validateKeepDrop повторяет ограничения TS: keep и drop несовместимы,
// kh+kl нельзя, диапазон N — от 1 до count-1.
func validateKeepDrop(group DiceGroup, spec string) error {
	if group.KeepHigh != nil && group.KeepLow != nil {
		return fmt.Errorf("В «%s» нельзя одновременно kh и kl.", spec)
	}
	keeps := 0
	if group.KeepHigh != nil {
		keeps++
	}
	if group.KeepLow != nil {
		keeps++
	}
	drops := 0
	if group.DropHigh != nil {
		drops++
	}
	if group.DropLow != nil {
		drops++
	}
	if keeps > 0 && drops > 0 {
		return fmt.Errorf("В «%s» нельзя комбинировать keep (kh/kl) и drop (dh/dl).", spec)
	}
	check := func(value *int, kind string) error {
		if value == nil {
			return nil
		}
		if *value < 1 || *value >= group.Count {
			return fmt.Errorf("В «%s» %s должен быть от 1 до %d.", spec, kind, group.Count-1)
		}
		return nil
	}
	if err := check(group.KeepHigh, "kh"); err != nil {
		return err
	}
	if err := check(group.KeepLow, "kl"); err != nil {
		return err
	}
	if err := check(group.DropHigh, "dh"); err != nil {
		return err
	}
	return check(group.DropLow, "dl")
}

// applyKeepDrop оставляет N лучших/худших (keep) или убирает N (drop).
// Порядок в kept — исходный порядок значений.
func applyKeepDrop(values []int, group DiceGroup) (kept []int, dropped []int) {
	if group.KeepHigh == nil && group.KeepLow == nil && group.DropHigh == nil && group.DropLow == nil {
		return values, nil
	}
	order := make([]int, len(values))
	for i := range values {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		left, right := order[a], order[b]
		if values[left] != values[right] {
			return values[left] > values[right]
		}
		return left < right
	})

	keptIndex := make(map[int]bool, len(values))
	switch {
	case group.KeepHigh != nil:
		for _, index := range order[:*group.KeepHigh] {
			keptIndex[index] = true
		}
	case group.KeepLow != nil:
		for _, index := range order[len(order)-*group.KeepLow:] {
			keptIndex[index] = true
		}
	case group.DropHigh != nil:
		removed := make(map[int]bool, *group.DropHigh)
		for _, index := range order[:*group.DropHigh] {
			removed[index] = true
		}
		for i := range values {
			keptIndex[i] = !removed[i]
		}
	case group.DropLow != nil:
		removed := make(map[int]bool, *group.DropLow)
		for _, index := range order[len(order)-*group.DropLow:] {
			removed[index] = true
		}
		for i := range values {
			keptIndex[i] = !removed[i]
		}
	default:
		for i := range values {
			keptIndex[i] = true
		}
	}

	for i, value := range values {
		if keptIndex[i] {
			kept = append(kept, value)
		} else {
			dropped = append(dropped, value)
		}
	}
	return kept, dropped
}

// rollGroupWithExplode бросает группу; грань == sides добавляет ещё одну
// кость (рекурсивно, с лимитом explodeLimit).
func rollGroupWithExplode(sides, count int, explode bool, random RandomSource) []int {
	out := make([]int, 0, count)
	for i := 0; i < count; i++ {
		face := randomInt(sides, random)
		out = append(out, face)
		if !explode {
			continue
		}
		for guard := 0; face == sides && guard < explodeLimit; guard++ {
			face = randomInt(sides, random)
			out = append(out, face)
		}
	}
	return out
}

// NotationOptions — параметры броска по нотации.
type NotationOptions struct {
	Random    RandomSource
	Advantage *bool
}

// RollDiceNotation бросает кубики по нотации. advantage/disadvantage
// применяется только к d20-группам без keep/drop; прочие кости честные.
func RollDiceNotation(spec string, options NotationOptions) (NotationResult, error) {
	random := randomOr(options.Random)
	parsed, err := ParseDiceNotation(spec)
	if err != nil {
		return NotationResult{}, err
	}

	groups := make([]DiceGroupResult, 0, len(parsed.Groups))
	for _, group := range parsed.Groups {
		hasKeepDrop := group.KeepHigh != nil || group.KeepLow != nil || group.DropHigh != nil || group.DropLow != nil
		useAdv := options.Advantage != nil && group.Sides == 20 && !hasKeepDrop
		if useAdv {
			result, rollErr := RollDice(group.Sides, group.Count, random, options.Advantage)
			if rollErr != nil {
				return NotationResult{}, rollErr
			}
			groups = append(groups, DiceGroupResult{
				Spec:     groupSpec(group),
				Rolls:    result.Rolls,
				Pairs:    result.Pairs,
				Subtotal: result.Total,
			})
			continue
		}
		values := rollGroupWithExplode(group.Sides, group.Count, group.Explode, random)
		kept, dropped := applyKeepDrop(values, group)
		groupResult := DiceGroupResult{Spec: groupSpec(group), Rolls: kept}
		for _, value := range kept {
			groupResult.Subtotal += value
		}
		if len(dropped) > 0 {
			groupResult.Dropped = dropped
		}
		groups = append(groups, groupResult)
	}

	result := NotationResult{Notation: spec, Groups: groups, Modifier: parsed.Modifier}
	for _, group := range groups {
		result.Total += group.Subtotal
	}
	result.Total += parsed.Modifier
	return result, nil
}
