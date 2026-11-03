package dm

import (
	"context"
	"strings"
	"testing"

	agotest "github.com/luckystrker/ago/testing"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// Обе гарантии в настоящей цепочке middleware: цитата броска и вопрос
// другому игроку. Вопрос обязан остаться последней фразой ответа (PLAN 41).
func TestGuaranteesComposeQuestionLast(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	hp, maxHP := 12, 12
	if _, err := st.AddMember(campaign.ID, "dm-1", store.NewMemberInput{UserID: "player-2"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if _, err := st.SaveCharacter(campaign.ID, "player-2", store.NewCharacterInput{
		Name: "Лиса", CharacterClass: "плут", Race: "человек", HP: &hp, MaxHP: &maxHP,
	}); err != nil {
		t.Fatalf("SaveCharacter Лиса: %v", err)
	}

	var prompts []PlayerPromptAppend
	cfg := agotest.NewTestConfig(
		toolCall("c1", "skill_check", `{"character_name":"Дэн","skill":"Восприятие","difficulty":12}`),
		toolCall("c2", "ask_player", `{"character":"Лиса"}`),
		plain("Дэн замирает у алтаря."),
	)
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		OnPlayerPrompt: func(item PlayerPromptAppend) { prompts = append(prompts, item) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	reply, err := d.Ask(context.Background(), "Иду к алтарю.")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	trimmed := strings.TrimSpace(reply)
	if !strings.HasSuffix(trimmed, "Лиса, что делаешь?") {
		t.Fatalf("вопрос не последняя фраза ответа:\n%s", reply)
	}
	if !strings.Contains(trimmed, "d20 = ") {
		t.Fatalf("нет цитаты броска:\n%s", reply)
	}
	if len(prompts) != 1 || prompts[0].Character != "Лиса" {
		t.Fatalf("счётчик гарантии вопроса = %+v", prompts)
	}
}

// Если модель уже задала вопрос, а цитата броска дописана после него в
// скобках, вопрос в ответе есть — второй раз не дописываем.
func TestGuaranteeKeepsQuestionBeforeQuote(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	hp, maxHP := 12, 12
	if _, err := st.AddMember(campaign.ID, "dm-1", store.NewMemberInput{UserID: "player-2"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if _, err := st.SaveCharacter(campaign.ID, "player-2", store.NewCharacterInput{
		Name: "Лиса", CharacterClass: "плут", Race: "человек", HP: &hp, MaxHP: &maxHP,
	}); err != nil {
		t.Fatalf("SaveCharacter Лиса: %v", err)
	}

	var prompts []PlayerPromptAppend
	cfg := agotest.NewTestConfig(
		toolCall("c1", "skill_check", `{"character_name":"Дэн","skill":"Восприятие","difficulty":12}`),
		toolCall("c2", "ask_player", `{"character":"Лиса"}`),
		plain("Дэн замирает у алтаря. Лиса, что делаешь?"),
	)
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		OnPlayerPrompt: func(item PlayerPromptAppend) { prompts = append(prompts, item) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	reply, err := d.Ask(context.Background(), "Иду к алтарю.")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got := strings.Count(reply, "что делаешь?"); got != 1 {
		t.Fatalf("вопрос продублирован (%d):\n%s", got, reply)
	}
	if len(prompts) != 0 {
		t.Fatalf("срабатывание без дописки: %+v", prompts)
	}
}
