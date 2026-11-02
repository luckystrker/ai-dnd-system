package telegram

import (
	"strconv"
	"testing"
	"time"

	"github.com/luckystriker/ai-dnd-system/internal/engine"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func gateMessage(chatID int64, chatType string, userID int64, username, text string) *Message {
	message := topicMessage(chatID, chatType, nil, false)
	message.From = &User{ID: userID, IsBot: false, FirstName: username, Username: username}
	message.Text = text
	return message
}

func newGate(t *testing.T) (*Gate, *store.Store, *store.Campaign) {
	t.Helper()
	st := identityTestStore(t)
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Три ворона", Length: store.LengthShort, Setting: "север", Theme: "выживание",
	}, store.NewOwnerInput{UserID: "100"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.BindAndActivate(campaign.ID, "100", store.BoundChat{ChatID: "-100"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveCharacter(campaign.ID, "100", store.NewCharacterInput{
		Name: "Дэн", CharacterClass: "воин", Race: "человек",
	}); err != nil {
		t.Fatal(err)
	}
	gate := NewGate(st, "dnd_bot", nil)
	return gate, st, campaign
}

func TestGatePrivateAndChannels(t *testing.T) {
	t.Parallel()

	gate, _, _ := newGate(t)
	messages := []Incoming{{Message: gateMessage(1, "private", 100, "den", "Иду к двери")}}
	if !gate.ShouldReply("1:", messages) {
		t.Fatal("в личке бот должен отвечать всегда")
	}
	channel := []Incoming{{Message: gateMessage(-5, "channel", 100, "den", "новость")}}
	if gate.ShouldReply("-5:", channel) {
		t.Fatal("канал не должен будить LLM")
	}
	bot := Incoming{Message: gateMessage(-100, "supergroup", 9, "other_bot", "я бот")}
	bot.Message.From.IsBot = true
	if gate.ShouldReply("-100:", []Incoming{bot}) {
		t.Fatal("сообщение бота не должно будить LLM")
	}
}

func TestGateExplicitSignals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
	}{
		{"command", "/start"},
		{"mention", "@dnd_bot, что тут происходит?"},
		{"mention in sentence", "Эй @DND_BOT подскажи"},
		{"addressed dm", "ДМ, а конь ещё жив?"},
		{"addressed master", "Мастер, можно я залезу на стену?"},
		{"addressed gm", "gm, сколько до рассвета?"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gate, _, _ := newGate(t)
			message := gateMessage(-100, "supergroup", 100, "den", tc.text)
			if !gate.ShouldReply("-100:", []Incoming{{Message: message}}) {
				t.Fatalf("%q не разбудил LLM", tc.text)
			}
		})
	}
}

// Упоминание другого бота и команда чужому боту — не наш сигнал.
func TestGateIgnoresForeignMentionsAndCommands(t *testing.T) {
	t.Parallel()

	gate, _, _ := newGate(t)
	for _, text := range []string{"@other_bot привет", "/start@other_bot", "Привет, днд бот"} {
		message := gateMessage(-100, "supergroup", 100, "den", text)
		if gate.ShouldReply("-100:", []Incoming{{Message: message}}) {
			t.Fatalf("%q разбудил LLM", text)
		}
	}
}

func TestGateReplyToBot(t *testing.T) {
	t.Parallel()

	gate, _, _ := newGate(t)
	message := gateMessage(-100, "supergroup", 100, "den", "Дверь со скрипом")
	message.ReplyToMessage = &Message{
		MessageID: 5, Text: "Ты стоишь у двери",
		From: &User{ID: 777, IsBot: true, FirstName: "DM"},
	}
	if !gate.ShouldReply("-100:", []Incoming{{Message: message}}) {
		t.Fatal("реплай боту не разбудил LLM")
	}
	// Реплай другому игроку — обычная переписка, если бот не ждёт ответа.
	message.ReplyToMessage.From = &User{ID: 101, FirstName: "Лиса"}
	if gate.ShouldReply("-100:", []Incoming{{Message: message}}) {
		t.Fatal("реплай игроку разбудил LLM без других сигналов")
	}
}

// Переписка игроков между собой не будит LLM, но первый ответ на реплику DM —
// будит: DM задал вопрос и ждёт хода.
func TestGatePlayerChatVersusFollowUp(t *testing.T) {
	t.Parallel()

	gate, _, _ := newGate(t)
	first := Incoming{Message: gateMessage(-100, "supergroup", 100, "den", "Давай сначала осмотрим двор.")}
	if gate.ShouldReply("-100:", []Incoming{first}) {
		t.Fatal("реплика игрока без сигналов разбудила LLM (бот не ждёт ответа)")
	}

	gate.MarkBotReply("-100:")
	if !gate.ShouldReply("-100:", []Incoming{first}) {
		t.Fatal("ответ игрока после реплики DM не разбудил LLM")
	}
	// После первого ответа игроки общаются между собой — LLM не будим.
	second := Incoming{Message: gateMessage(-100, "supergroup", 101, "lisa", "Согласна, двор безопаснее.")}
	if gate.ShouldReply("-100:", []Incoming{second}) {
		t.Fatal("переписка игроков разбудила LLM")
	}

	// Окно ожидания ответа истекает: старый ответ DM не будит LLM вечно.
	now := time.Unix(1_700_000_000, 0)
	gate.SetClock(func() time.Time { return now })
	gate.MarkBotReply("-100:")
	later := now.Add(DefaultFollowUpWindow + time.Minute)
	gate.SetClock(func() time.Time { return later })
	if gate.ShouldReply("-100:", []Incoming{second}) {
		t.Fatal("просроченное ожидание ответа разбудило LLM")
	}
}

func TestGateCombatTurn(t *testing.T) {
	t.Parallel()

	gate, st, campaign := newGate(t)
	combat := engine.EmptyCombatOrder()
	combat.Started = true
	combat.Round = 1
	combat.Current = 0
	combat.Order = []engine.CombatantEntry{{ID: "den", Name: "Дэн", Side: engine.SideParty}, {ID: "goblin", Name: "Гоблин", Side: engine.SideEnemy}}
	if err := st.SaveCombatState(campaign.Slug, store.CombatSnapshot{Combat: combat, Enemies: []store.SerializedEnemy{{ID: "goblin", Name: "Гоблин", HP: 7, AC: 13}}}); err != nil {
		t.Fatalf("SaveCombatState: %v", err)
	}
	inTurn := Incoming{Message: gateMessage(-100, "supergroup", 100, "den", "Атакую гоблина")}
	if !gate.ShouldReply("-100:", []Incoming{inTurn}) {
		t.Fatal("ход игрока в бою не разбудил LLM")
	}
	// Сейчас ходит персонаж другого игрока.
	combat.Order[0].Name = "Лиса"
	if err := st.SaveCombatState(campaign.Slug, store.CombatSnapshot{Combat: combat, Enemies: []store.SerializedEnemy{{ID: "goblin", Name: "Гоблин", HP: 7, AC: 13}}}); err != nil {
		t.Fatal(err)
	}
	if gate.ShouldReply("-100:", []Incoming{inTurn}) {
		t.Fatal("не свой боевой ход разбудил LLM")
	}
}

func TestGateBatchWithAnySignalWakes(t *testing.T) {
	t.Parallel()

	gate, _, _ := newGate(t)
	den := Incoming{Message: gateMessage(-100, "supergroup", 100, "den", "Осматриваю двор.")}
	lisa := Incoming{Message: gateMessage(-100, "supergroup", 101, "lisa", "ДМ, а что во дворе?")}
	if !gate.ShouldReply("-100:", []Incoming{den, lisa}) {
		t.Fatal("сигнал во втором сообщении склейки не разбудил LLM")
	}
}

func TestGateNilAndEmpty(t *testing.T) {
	t.Parallel()

	gate, _, _ := newGate(t)
	if gate.ShouldReply("-100:", nil) {
		t.Fatal("пустой батч разбудил LLM")
	}
	message := gateMessage(-100, "supergroup", 100, "den", "   ")
	if gate.ShouldReply("-100:", []Incoming{{Message: message}}) {
		t.Fatal("пустое сообщение разбудило LLM")
	}
}

func TestGateMentionBounds(t *testing.T) {
	t.Parallel()

	gate, _, _ := newGate(t)
	message := gateMessage(-100, "supergroup", 100, "den", "Пишу @dnd_bot_extra и всё")
	if gate.ShouldReply("-100:", []Incoming{{Message: message}}) {
		t.Fatal("упоминание с другим username посчитано нашим")
	}
}

// Замолчавшие чаты не копятся в awaiting вечно.
func TestGateSweepsStaleAwaiting(t *testing.T) {
	t.Parallel()

	gate, _, _ := newGate(t)
	gate.SetFollowUpWindow(time.Minute)
	now := time.Unix(1_700_000_000, 0)
	gate.SetClock(func() time.Time { return now })
	for index := 0; index < 1000; index++ {
		gate.MarkBotReply("chat-" + strconv.Itoa(index))
	}
	later := now.Add(2 * time.Minute)
	gate.SetClock(func() time.Time { return later })
	gate.MarkBotReply("fresh")
	gate.mu.Lock()
	size := len(gate.awaiting)
	gate.mu.Unlock()
	if size != 1 {
		t.Fatalf("awaiting = %d, want 1 после чистки", size)
	}
}
