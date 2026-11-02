package tools

import (
	"strings"
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func TestRollDiceTool(t *testing.T) {
	env := newTestEnv(t)
	env.deps.Random = randomSeq(0.5)

	result, err := env.deps.rollDice(rollDiceArgs{Notation: "1d20+5"})
	if err != nil {
		t.Fatalf("rollDice: %v", err)
	}
	want := "🎲 Бросок 1d20+5:\n1d20 → [11] = 11\n+5 модификатор\nИтого: 16"
	if result != want {
		t.Fatalf("\n got %q\nwant %q", result, want)
	}

	advantaged, err := env.deps.rollDice(rollDiceArgs{Notation: "1d20", Advantage: ptr("advantage")})
	if err != nil {
		t.Fatalf("advantage: %v", err)
	}
	if !strings.Contains(advantaged.(string), "(преимущество)") {
		t.Fatalf("result = %q", advantaged)
	}

	_, err = env.deps.rollDice(rollDiceArgs{Notation: "2d6+abc"})
	toolErr := wantToolCode(t, err, CodeInvalidArg)
	if !strings.Contains(toolErr.Message, "Не удалось разобрать бросок") {
		t.Fatalf("message = %q", toolErr.Message)
	}
}

func TestSkillCheckUsesSheetStats(t *testing.T) {
	env := newTestEnv(t)
	env.deps.Random = randomSeq(0.5)

	result, err := env.deps.skillCheck(env.player, skillCheckArgs{
		CharacterName: "Ария", Skill: "Акробатика", Difficulty: 12,
	})
	if err != nil {
		t.Fatalf("skillCheck: %v", err)
	}
	want := "🎲 Ария — проверка «Акробатика» (ЛОВ): d20 = 11 +3 = 14 vs DC 12 -> УСПЕХ"
	if result != want {
		t.Fatalf("\n got %q\nwant %q", result, want)
	}
}

func TestSkillCheckEnvironmentAndLowStreak(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.store.SetEnvironment(env.campaign.ID, store.EnvironmentPatch{
		TimeOfDay: ptr(store.TimeNight), Weather: ptr("туман"),
	}); err != nil {
		t.Fatal(err)
	}
	env.deps.Random = randomSeq(0.5)
	result, err := env.deps.skillCheck(env.player, skillCheckArgs{
		CharacterName: "Ария", Skill: "Восприятие", Difficulty: 10,
	})
	if err != nil {
		t.Fatalf("skillCheck: %v", err)
	}
	if !strings.Contains(result.(string), "(помеха)") || !strings.Contains(result.(string), "ночь: помеха") {
		t.Fatalf("result = %q", result)
	}

	// Две низкие грани подряд включают «добрый» d20: следующий бросок не ниже 11.
	env.deps.Random = randomSeq(0)
	for i := 0; i < 2; i++ {
		if _, err := env.deps.skillCheck(env.player, skillCheckArgs{
			CharacterName: "Ария", Skill: "Акробатика", Difficulty: 5,
		}); err != nil {
			t.Fatal(err)
		}
	}
	result, err = env.deps.skillCheck(env.player, skillCheckArgs{
		CharacterName: "Ария", Skill: "Акробатика", Difficulty: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.(string), "d20 = 11") {
		t.Fatalf("анти-серия не сработала: %q", result)
	}
	if history := env.deps.Dice.Recent(env.player.DiceKey()); len(history) != 4 {
		t.Fatalf("history = %v", history)
	}
}

func TestSkillCheckCannotActForOtherCharacter(t *testing.T) {
	env := newTestEnv(t)
	other, err := env.store.SaveCharacter(env.campaign.ID, "u-dm", store.NewCharacterInput{
		Name: "Гэндальф", CharacterClass: "волшебник", Race: "майар", Stats: map[string]float64{"wis": 18},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = other
	_, err = env.deps.skillCheck(env.player, skillCheckArgs{
		CharacterName: "Гэндальф", Skill: "Восприятие", Difficulty: 10,
	})
	wantToolCode(t, err, CodeAccessDenied)
}

func TestInitiativeAndCombatAttack(t *testing.T) {
	env := newTestEnv(t)
	// Инициатива: Ария 15, гоблин 10. Атака: крит 20, урон 1d6=4 → x2 = 8.
	env.deps.Random = randomSeq(d20Face(15), d20Face(10), 0.999999, 0.5)

	result, err := env.deps.initiative(env.player, initiativeArgs{Combatants: []initiativeCombatantArg{
		{Name: "Ария", Side: "party"},
		{Name: "Гоблин", Side: "enemy", HP: ptr(1), AC: ptr(10)},
	}})
	if err != nil {
		t.Fatalf("initiative: %v", err)
	}
	text := result.(string)
	if !strings.Contains(text, "Инициатива:") || !strings.Contains(text, "Ария [id: ") {
		t.Fatalf("initiative = %q", text)
	}
	snapshot, err := env.store.LoadCombatState(env.campaign.Slug)
	if err != nil || snapshot == nil {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	if len(snapshot.Enemies) != 1 || snapshot.Enemies[0].HP != 1 {
		t.Fatalf("enemies = %+v", snapshot.Enemies)
	}

	attacked, err := env.deps.combat(env.player, combatArgs{
		Action: "attack", Attacker: ptr("Ария"), Enemy: ptr("Гоблин"),
	})
	if err != nil {
		t.Fatalf("attack: %v", err)
	}
	attackText := attacked.(string)
	if !strings.Contains(attackText, "КРИТ") || !strings.Contains(attackText, "Все враги повержены") {
		t.Fatalf("attack = %q", attackText)
	}
	if snapshot, _ := env.store.LoadCombatState(env.campaign.Slug); snapshot != nil {
		t.Fatalf("бой не очищен: %+v", snapshot)
	}
}

func TestCombatDamagePersistsSheet(t *testing.T) {
	env := newTestEnv(t)
	env.deps.Random = randomSeq(d20Face(15), d20Face(10))
	if _, err := env.deps.initiative(env.dm, initiativeArgs{Combatants: []initiativeCombatantArg{
		{Name: "Ария", Side: "party"},
		{Name: "Гоблин", Side: "enemy", HP: ptr(10), AC: ptr(10)},
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := env.deps.combat(env.player, combatArgs{
		Action: "damage", Target: ptr("Ария"), Amount: ptr(4),
	})
	if err != nil {
		t.Fatalf("damage: %v", err)
	}
	if !strings.Contains(result.(string), "урон 4 HP → 6/10") {
		t.Fatalf("result = %q", result)
	}
	sheets, _ := env.store.ListCharacters(env.campaign.ID)
	if sheets[0].HP == nil || *sheets[0].HP != 6 {
		t.Fatalf("sheet hp = %+v", sheets[0].HP)
	}
	snapshot, _ := env.store.LoadCombatState(env.campaign.Slug)
	if snapshot == nil || snapshot.Combat.Order[0].HP == nil || *snapshot.Combat.Order[0].HP != 6 {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	healed, err := env.deps.combat(env.player, combatArgs{
		Action: "damage", Target: ptr("Ария"), Amount: ptr(2), Heal: ptr(true),
	})
	if err != nil {
		t.Fatalf("heal: %v", err)
	}
	if !strings.Contains(healed.(string), "лечение 2 HP → 8/10") {
		t.Fatalf("heal = %q", healed)
	}
}

func TestCombatDodgeNextStatusEnd(t *testing.T) {
	env := newTestEnv(t)
	env.deps.Random = randomSeq(d20Face(15), d20Face(10))
	if _, err := env.deps.initiative(env.player, initiativeArgs{Combatants: []initiativeCombatantArg{
		{Name: "Ария", Side: "party"},
		{Name: "Гоблин", Side: "enemy", HP: ptr(10), AC: ptr(10)},
	}}); err != nil {
		t.Fatal(err)
	}
	status, err := env.deps.combat(env.player, combatArgs{Action: "status"})
	if err != nil || !strings.Contains(status.(string), "Порядок ходов:") {
		t.Fatalf("status = %v, %v", status, err)
	}
	dodged, err := env.deps.combat(env.player, combatArgs{Action: "dodge", Attacker: ptr("Ария")})
	if err != nil {
		t.Fatalf("dodge: %v", err)
	}
	if !strings.Contains(dodged.(string), "уклоняется") {
		t.Fatalf("dodge = %q", dodged)
	}
	next, err := env.deps.combat(env.player, combatArgs{Action: "next"})
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if !strings.Contains(next.(string), "Ход переходит дальше") {
		t.Fatalf("next = %q", next)
	}
	ended, err := env.deps.combat(env.player, combatArgs{Action: "end"})
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if !strings.Contains(ended.(string), "Бой окончен") {
		t.Fatalf("end = %q", ended)
	}
	if snapshot, _ := env.store.LoadCombatState(env.campaign.Slug); snapshot != nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	_, err = env.deps.combat(env.player, combatArgs{Action: "attack", Attacker: ptr("Ария"), Enemy: ptr("Гоблин")})
	wantToolCode(t, err, CodeConflict)
}

func TestLevelUp(t *testing.T) {
	env := newTestEnv(t)

	result, err := env.deps.levelUp(env.player, levelUpArgs{
		Character: "Ария", Level: ptr(2),
		Ability: &levelUpAbilityArg{Name: "Хитрость", Description: "Бонусное действие."},
	})
	if err != nil {
		t.Fatalf("levelUp: %v", err)
	}
	character := result.(map[string]any)["character"].(map[string]any)
	if character["level"] != 2 || character["maxHp"] != 20 || character["gainedAbility"] != "Хитрость" || character["hpGain"] != 10 {
		t.Fatalf("character = %+v", character)
	}
	sheets, _ := env.store.ListCharacters(env.campaign.ID)
	if len(sheets[0].Abilities) != 1 || sheets[0].Abilities[0].Name != "Хитрость" {
		t.Fatalf("abilities = %+v", sheets[0].Abilities)
	}
	_, err = env.deps.levelUp(env.player, levelUpArgs{Character: "Ария", Level: ptr(2)})
	wantToolCode(t, err, CodeConflict)
}
