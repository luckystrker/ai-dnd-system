package telegram

import (
	"path/filepath"
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func strPtr(value string) *string { return &value }
func intPtr(value int) *int       { return &value }

func identityTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "campaigns.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func topicMessage(chatID int64, chatType string, thread *int, isTopic bool) *Message {
	return &Message{
		MessageID:       1,
		MessageThreadID: thread,
		IsTopicMessage:  isTopic,
		Chat:            Chat{ID: chatID, Type: chatType},
		From:            &User{ID: 42, FirstName: "Дэн", LastName: "Кузнец", Username: "den"},
		Text:            "Иду к двери",
	}
}

// Реплаи в супергруппе несут message_thread_id родителя, но топиком не
// являются: сессия и кампания должны остаться на чате, а не на реплае.
func TestTopicOfOnlyRealForumTopics(t *testing.T) {
	t.Parallel()

	real := topicMessage(-100, "supergroup", intPtr(7), true)
	if topic := TopicOf(real); topic == nil || *topic != 7 {
		t.Fatalf("настоящий топик потерян: %v", topic)
	}
	reply := topicMessage(-100, "supergroup", intPtr(99), false)
	if topic := TopicOf(reply); topic != nil {
		t.Fatalf("реплай принят за топик: %v", *topic)
	}
	if key := ChatKey(reply); key != "-100:" {
		t.Fatalf("ChatKey реплая = %q", key)
	}
	if key := ChatKey(real); key != "-100:7" {
		t.Fatalf("ChatKey топика = %q", key)
	}
}

func TestIdentityFromMessage(t *testing.T) {
	t.Parallel()

	message := topicMessage(-100, "supergroup", intPtr(7), true)
	identity := IdentityFromMessage(message)
	if identity.UserID != "42" || identity.Username != "den" || identity.Name != "Дэн Кузнец" {
		t.Fatalf("identity = %+v", identity)
	}
	if identity.ChatID != "-100" || identity.ChatType != "supergroup" {
		t.Fatalf("identity = %+v", identity)
	}
	if identity.MessageThreadID == nil || *identity.MessageThreadID != 7 {
		t.Fatalf("топик = %v", identity.MessageThreadID)
	}

	reply := topicMessage(-100, "supergroup", intPtr(99), false)
	if identity := IdentityFromMessage(reply); identity.MessageThreadID != nil {
		t.Fatalf("реплай дал identity с топиком: %v", *identity.MessageThreadID)
	}

	if empty := IdentityFromMessage(nil); empty.UserID != "" {
		t.Fatalf("identity без сообщения = %+v", empty)
	}
}

func TestFindCampaignForIdentityFallsBackFromTopic(t *testing.T) {
	t.Parallel()

	st := identityTestStore(t)
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Три ворона", Length: store.LengthShort, Setting: "север", Theme: "выживание",
	}, store.NewOwnerInput{UserID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.BindAndActivate(campaign.ID, "42", store.BoundChat{ChatID: "-100"}); err != nil {
		t.Fatal(err)
	}

	found, err := FindCampaignForIdentity(st, Identity{UserID: "42", ChatID: "-100"})
	if err != nil || found == nil || found.ID != campaign.ID {
		t.Fatalf("точное совпадение: %+v, %v", found, err)
	}
	// Кампания привязана к чату без топика, а сообщение пришло из топика:
	// откат к чату без топика (как findCampaignForIdentity в TS).
	found, err = FindCampaignForIdentity(st, Identity{UserID: "42", ChatID: "-100", MessageThreadID: intPtr(7)})
	if err != nil || found == nil || found.ID != campaign.ID {
		t.Fatalf("откат топика: %+v, %v", found, err)
	}
	found, err = FindCampaignForIdentity(st, Identity{UserID: "42", ChatID: "-999"})
	if err != nil || found != nil {
		t.Fatalf("чужой чат: %+v, %v", found, err)
	}
	found, err = FindCampaignForIdentity(st, Identity{UserID: "42"})
	if err != nil || found != nil {
		t.Fatalf("без чата: %+v, %v", found, err)
	}

	// Кампания в настоящем топике: точное совпадение важнее отката.
	topic := 7
	solo, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Второй поход", Length: store.LengthShort, Setting: "юг", Theme: "осада",
	}, store.NewOwnerInput{UserID: "u2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.BindAndActivate(solo.ID, "u2", store.BoundChat{ChatID: "-300", MessageThreadID: &topic}); err != nil {
		t.Fatalf("BindAndActivate topic: %v", err)
	}
	found, err = FindCampaignForIdentity(st, Identity{UserID: "u2", ChatID: "-300", MessageThreadID: intPtr(7)})
	if err != nil || found == nil || found.ID != solo.ID {
		t.Fatalf("точный топик: %+v, %v", found, err)
	}
	found, err = FindCampaignForIdentity(st, Identity{UserID: "u2", ChatID: "-300", MessageThreadID: intPtr(8)})
	if err != nil || found != nil {
		t.Fatalf("чужой топик не должен откатываться на топик-кампанию: %+v, %v", found, err)
	}
}

func TestSessionFromIdentity(t *testing.T) {
	t.Parallel()

	st := identityTestStore(t)
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Три ворона", Length: store.LengthShort, Setting: "север", Theme: "выживание",
	}, store.NewOwnerInput{UserID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	identity := Identity{
		UserID: "42", Username: "den", Name: "Дэн", ChatID: "-100",
		ChatType: "supergroup", MessageThreadID: intPtr(7),
	}
	session := SessionFor(campaign, identity)
	if session.CampaignID != campaign.ID || session.CampaignSlug != campaign.Slug {
		t.Fatalf("сессия без кампании: %+v", session)
	}
	if session.UserID != "42" || session.Username != "den" || session.Name != "Дэн" {
		t.Fatalf("сессия без личности: %+v", session)
	}
	if session.ChatID != "-100" || session.ChatType != "supergroup" {
		t.Fatalf("сессия без чата: %+v", session)
	}
	if session.MessageThreadID == nil || *session.MessageThreadID != 7 {
		t.Fatalf("сессия без топика: %+v", session)
	}
	if session.Trusted {
		t.Fatal("игроцкая сессия помечена Trusted")
	}

	// Непривязанный чат: сессия без кампании, но с личностью.
	loose := SessionFor(nil, identity)
	if loose.CampaignID != "" || loose.UserID != "42" {
		t.Fatalf("сессия без кампании = %+v", loose)
	}
}
