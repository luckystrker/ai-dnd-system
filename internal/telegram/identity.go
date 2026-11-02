package telegram

import (
	"strconv"

	"github.com/luckystriker/ai-dnd-system/internal/store"
	"github.com/luckystriker/ai-dnd-system/internal/tools"
)

// TopicOf возвращает настоящий форум-топик сообщения.
//
// Осторожно: Telegram ставит message_thread_id ЛЮБОМУ реплаю (= id сообщения,
// на который ответили), а не только топикам форума. Отличить их можно только
// по флагу is_topic_message: если брать message_thread_id как есть, каждый
// реплай открывал бы новую сессию и терял кампанию.
func TopicOf(message *Message) *int {
	if message == nil || !message.IsTopicMessage || message.MessageThreadID == nil {
		return nil
	}
	topic := *message.MessageThreadID
	return &topic
}

// ChatKey — ключ сессии: один чат или форум-топик, "chatId:topic".
func ChatKey(message *Message) string {
	if message == nil {
		return ""
	}
	key := ChatIDString(message.Chat.ID) + ":"
	if topic := TopicOf(message); topic != nil {
		key += strconv.Itoa(*topic)
	}
	return key
}

// StreamID — id durable-потока истории для ключа чата.
func StreamID(chatKey string) string { return "tg:" + chatKey }

// Identity — говорящий и координаты чата из Telegram-апдейта.
type Identity struct {
	UserID          string
	Username        string
	Name            string
	ChatID          string
	ChatType        string
	MessageThreadID *int
}

// IdentityFromMessage собирает личность автора и координаты чата.
func IdentityFromMessage(message *Message) Identity {
	if message == nil {
		return Identity{}
	}
	identity := Identity{
		ChatID:   ChatIDString(message.Chat.ID),
		ChatType: message.Chat.Type,
	}
	if message.From != nil {
		identity.UserID = strconv.FormatInt(message.From.ID, 10)
		identity.Username = message.From.Username
		identity.Name = message.From.DisplayName()
	}
	identity.MessageThreadID = TopicOf(message)
	return identity
}

// FindCampaignForIdentity находит кампанию по привязанному чату/топику.
// Для реплаев вне форум-топика и сообщений без топика — откат на кампанию,
// привязанную к чату без топика (порт findCampaignForIdentity из access.ts).
func FindCampaignForIdentity(st *store.Store, identity Identity) (*store.Campaign, error) {
	if st == nil || identity.ChatID == "" {
		return nil, nil
	}
	campaign, err := st.FindByBoundChat(identity.ChatID, identity.MessageThreadID, false)
	if err != nil || campaign != nil {
		return campaign, err
	}
	if identity.MessageThreadID == nil {
		return nil, nil
	}
	return st.FindByBoundChat(identity.ChatID, nil, false)
}

// SessionFor собирает tools.Session для DI: кампания чата (может быть nil),
// говорящий и координаты чата.
func SessionFor(campaign *store.Campaign, identity Identity) *tools.Session {
	session := &tools.Session{
		UserID:          identity.UserID,
		Username:        identity.Username,
		Name:            identity.Name,
		ChatID:          identity.ChatID,
		ChatType:        identity.ChatType,
		MessageThreadID: identity.MessageThreadID,
	}
	if campaign != nil {
		session.CampaignID = campaign.ID
		session.CampaignSlug = campaign.Slug
	}
	return session
}
