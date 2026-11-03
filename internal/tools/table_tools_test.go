package tools

import (
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// addPartyMember создаёт второго игрока с персонажем: сигнал ask_player
// осмыслен только для чужого PC.
func addPartyMember(t *testing.T, env *testEnv, userID, character string) *store.CharacterSheet {
	t.Helper()
	if _, err := env.store.AddMember(env.campaign.ID, "u-dm", store.NewMemberInput{
		UserID: userID, Name: &character,
	}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	sheet, err := env.store.SaveCharacter(env.campaign.ID, userID, store.NewCharacterInput{
		Name: character, CharacterClass: "воин", Race: "человек",
		MaxHP: ptr(12), HP: ptr(12),
	})
	if err != nil {
		t.Fatalf("SaveCharacter: %v", err)
	}
	return sheet
}

// ask_player подтверждает передачу решения другому игроку и называет его.
func TestAskPlayerRegistersHandoff(t *testing.T) {
	env := newTestEnv(t)
	addPartyMember(t, env, "u-other", "Борис")

	out, err := env.deps.askPlayer(askPlayerArgs{Session: env.player, Character: "Борис"})
	if err != nil {
		t.Fatalf("askPlayer: %v", err)
	}
	result := out.(map[string]any)
	if result["waiting_for"] != "Борис" {
		t.Fatalf("waiting_for = %v, want Борис", result["waiting_for"])
	}
}

// Свой персонаж — не «решение другого игрока»: сигнал отклоняется, чтобы
// гарантия не дописывала вопрос не туда.
func TestAskPlayerRejectsOwnCharacter(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.deps.askPlayer(askPlayerArgs{Session: env.player, Character: "Ария"})
	wantToolCode(t, err, CodeInvalidArg)
}

// Неизвестное имя — not_found с подсказкой, а не молчаливая дописка.
func TestAskPlayerRejectsUnknownCharacter(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.deps.askPlayer(askPlayerArgs{Session: env.player, Character: "Гэндальф"})
	wantToolCode(t, err, CodeNotFound)
}

// Без кампании сигнал не работает: в чате без игры ждать некого.
func TestAskPlayerRequiresCampaign(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.deps.askPlayer(askPlayerArgs{
		Session: &Session{UserID: "u-player", ChatID: "chat-1"}, Character: "Ария",
	})
	wantToolCode(t, err, CodeAccessDenied)
}
