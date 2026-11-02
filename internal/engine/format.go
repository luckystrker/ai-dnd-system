package engine

import (
	"fmt"
	"strings"
)

// Format-функции повторяют 1:1 строки, которые TS-тулы показывают игрокам
// (agent/tools/{skill_check,roll_dice,initiative}.ts): на эти форматы
// опираются регексы eval-скорера quotes_roll. Слайс 4 обязан использовать
// именно их, а не собирать текст заново.

// RollAdvantage — ярлык преимущества для roll_dice (как аргумент advantage).
type RollAdvantage string

const (
	AdvantageNormal RollAdvantage = "normal"
	AdvantageYes    RollAdvantage = "advantage"
	AdvantageNo     RollAdvantage = "disadvantage"
)

// diceGroupLine — строка одной группы в формате roll_dice.ts.
func diceGroupLine(group DiceGroupResult) string {
	if len(group.Pairs) > 0 {
		pairs := make([]string, 0, len(group.Pairs))
		for _, pair := range group.Pairs {
			pairs = append(pairs, fmt.Sprintf("%d|%d", pair[0], pair[1]))
		}
		return fmt.Sprintf("%s → [%s] = %d", group.Spec, strings.Join(pairs, ", "), group.Subtotal)
	}
	if len(group.Dropped) > 0 {
		return fmt.Sprintf("%s → [%s] (отброшено: %s) = %d",
			group.Spec, joinInts(group.Rolls), joinInts(group.Dropped), group.Subtotal)
	}
	return fmt.Sprintf("%s → [%s] = %d", group.Spec, joinInts(group.Rolls), group.Subtotal)
}

// FormatDiceRoll — полный ответ roll_dice (formatRoll из TS).
func FormatDiceRoll(result NotationResult, advantage RollAdvantage) string {
	tag := ""
	switch advantage {
	case AdvantageYes:
		tag = " (преимущество)"
	case AdvantageNo:
		tag = " (слабость)"
	}
	parts := []string{fmt.Sprintf("🎲 Бросок %s%s:", result.Notation, tag)}
	for _, group := range result.Groups {
		parts = append(parts, diceGroupLine(group))
	}
	if result.Modifier != 0 {
		sign := ""
		if result.Modifier > 0 {
			sign = "+"
		}
		parts = append(parts, fmt.Sprintf("%s%d модификатор", sign, result.Modifier))
	}
	parts = append(parts, fmt.Sprintf("Итого: %d", result.Total))
	return strings.Join(parts, "\n")
}

var abilityLabels = map[string]string{
	"str": "СИЛ",
	"dex": "ЛОВ",
	"con": "ТЕЛ",
	"int": "ИНТ",
	"wis": "МДР",
	"cha": "ХАР",
}

// FormatSkillCheck — ответ skill_check (execute из TS): полная расшифровка
// броска, тег преимущества, нат 20/1, примечания об отсутствии характеристик
// и окружении. advantage — ЭФФЕКТИВНОЕ значение после CombineAdvantage
// (TS формирует тег именно из него).
func FormatSkillCheck(character, skill string, result CheckResult, advantage *bool, statsFound bool, envReasons []string) string {
	abilityLabel, ok := abilityLabels[result.Ability]
	if !ok {
		abilityLabel = strings.ToUpper(result.Ability)
	}
	advTag := ""
	if advantage != nil {
		if *advantage {
			advTag = " (преимущество)"
		} else {
			advTag = " (помеха)"
		}
	}
	outcome := "ПРОВАЛ"
	if result.Success {
		outcome = "УСПЕХ"
	}
	if result.NaturalSuccess {
		outcome += " (натуральная 20)"
	}
	if result.NaturalFailure {
		outcome += " (натуральная 1)"
	}
	modifierSign := ""
	if result.Modifier >= 0 {
		modifierSign = "+"
	}
	noStatsNote := ""
	if !statsFound {
		noStatsNote = " [характеристики персонажа не найдены, модификатор +0]"
	}
	envNote := ""
	if len(envReasons) > 0 {
		envNote = " [" + strings.Join(envReasons, "; ") + "]"
	}
	return fmt.Sprintf(
		"🎲 %s — проверка «%s» (%s)%s: d20 = %d %s%d = %d vs DC %d -> %s%s%s",
		character, skill, abilityLabel, advTag,
		result.Roll, modifierSign, result.Modifier, result.Total, result.Difficulty,
		outcome, noStatsNote, envNote,
	)
}

// FormatInitiativeLine — строка участника в ответе initiative-тула:
// «1. Дэн [id: dan] (15 = 12 + 3, HP 10/10)» или с «враг, HP 7, КД 13».
func FormatInitiativeLine(index int, entry CombatantEntry) string {
	parts := []string{fmt.Sprintf("%d = %d + %d", entry.Total, entry.Roll, entry.Bonus)}
	if entry.Side == SideEnemy {
		hp, ac := "?", "?"
		if entry.HP != nil {
			hp = fmt.Sprintf("%d", *entry.HP)
		}
		if entry.AC != nil {
			ac = fmt.Sprintf("%d", *entry.AC)
		}
		parts = append(parts, fmt.Sprintf("враг, HP %s, КД %s", hp, ac))
	} else if entry.HP != nil {
		maxHP := "?"
		if entry.MaxHP != nil {
			maxHP = fmt.Sprintf("%d", *entry.MaxHP)
		}
		parts = append(parts, fmt.Sprintf("HP %d/%s", *entry.HP, maxHP))
	}
	return fmt.Sprintf("%d. %s [id: %s] (%s)", index+1, entry.Name, entry.ID, strings.Join(parts, ", "))
}

func joinInts(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, ", ")
}

// FormatRollError — ответ roll_dice при ошибке парсинга (catch из TS).
func FormatRollError(notation string, err error) string {
	return fmt.Sprintf("Не удалось разобрать бросок «%s»: %v", notation, err)
}

// FormatInitiative — полный ответ initiative-тула: заголовок, строки
// участников и хвост с первым ходом (без участников — короткий хвост).
//
// Строки атаки combat-тула («d20 = N + бонус против КД X») появятся в
// слайсе 4 вместе с самим тулом; их контракт закрепит format-тест тула.
func FormatInitiative(entries []CombatantEntry) string {
	lines := make([]string, 0, len(entries))
	for index, entry := range entries {
		lines = append(lines, FormatInitiativeLine(index, entry))
	}
	started := "Бой начат, раунд 1."
	if len(entries) > 0 {
		first := entries[0]
		started = fmt.Sprintf("Бой начат, раунд 1. Первый ход: %s (%s). Используй id в combat.", first.Name, first.ID)
	}
	return "Инициатива:\n" + strings.Join(lines, "\n") + "\n" + started
}
