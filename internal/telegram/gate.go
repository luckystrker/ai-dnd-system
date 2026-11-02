package telegram

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/luckystriker/ai-dnd-system/internal/engine"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// DefaultFollowUpWindow — сколько после ответа DM сообщение игрока считается
// ответом на реплику DM, а не перепиской игроков между собой.
const DefaultFollowUpWindow = 10 * time.Minute

// gateSweepThreshold — порог, после которого MarkBotReply вычищает истёкшие
// записи awaiting. Запись нужна только на время follow-up окна; без чистки
// карта росла бы вместе с числом чатов, где бот когда-либо отвечал.
const gateSweepThreshold = 1000

// Incoming — сообщение, попавшее в очередь чата.
type Incoming struct {
	Message  *Message
	UpdateID int64
}

// Gate — дешёвый (без LLM) критерий «отвечать ли» в группе.
//
// В личке бот отвечает всегда. В группе ход LLM будит только явный сигнал:
// команда боту, @упоминание, реплай на сообщение бота, обращение к DM по роли
// («ДМ», «мастер», «ведущий»), собственный ход в бою — или первый ответ
// игрока на реплику DM в пределах DefaultFollowUpWindow. Переписка игроков
// между собой в транскрипт пишется, но LLM не будит: иначе группа платит за
// каждый OOC-обмен репликами.
type Gate struct {
	store       *store.Store
	botUsername string
	followUp    time.Duration

	mu       sync.Mutex
	awaiting map[string]time.Time
	clock    func() time.Time
}

// NewGate собирает гейт. clock nil — time.Now (подменяется в тестах).
func NewGate(st *store.Store, botUsername string, clock func() time.Time) *Gate {
	if clock == nil {
		clock = time.Now
	}
	return &Gate{
		store:       st,
		botUsername: botUsername,
		followUp:    DefaultFollowUpWindow,
		awaiting:    map[string]time.Time{},
		clock:       clock,
	}
}

// SetClock подменяет часы (тесты).
func (g *Gate) SetClock(clock func() time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clock = clock
}

// SetFollowUpWindow меняет окно «бот ждёт ответа» (тесты).
func (g *Gate) SetFollowUpWindow(window time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.followUp = window
}

// MarkBotReply отмечает, что бот ответил в чате: следующий ход игрока — ответ
// на реплику DM.
func (g *Gate) MarkBotReply(chatKey string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock()
	g.awaiting[chatKey] = now
	// Замолчавшие чаты не должны копиться вечно: запись нужна только на
	// время follow-up окна.
	if len(g.awaiting) > gateSweepThreshold {
		cutoff := now.Add(-g.followUp)
		for key, at := range g.awaiting {
			if at.Before(cutoff) {
				delete(g.awaiting, key)
			}
		}
	}
}

// ShouldReply решает, будить ли LLM для склеенного батча чата. Состояние
// «бот ждёт ответа» потребляется один раз за батч.
func (g *Gate) ShouldReply(chatKey string, batch []Incoming) bool {
	if len(batch) == 0 {
		return false
	}
	g.mu.Lock()
	waitingSince, waiting := g.awaiting[chatKey]
	delete(g.awaiting, chatKey)
	now := g.clock()
	followUp := g.followUp
	g.mu.Unlock()
	waiting = waiting && now.Sub(waitingSince) <= followUp

	for _, item := range batch {
		message := item.Message
		if message == nil || message.From == nil || message.From.IsBot {
			continue
		}
		if strings.TrimSpace(message.TextContent()) == "" && len(message.Attachments()) == 0 {
			continue
		}
		// Канал — не разговорная площадка; личка — всегда отвечаем.
		if message.Chat.Type == "channel" {
			continue
		}
		if message.Chat.Type == "private" {
			return true
		}
		if message.IsCommand(g.botUsername) {
			return true
		}
		if mentionsBot(message.TextContent(), g.botUsername) {
			return true
		}
		if replyToBot(message) {
			return true
		}
		if addressesDM(message.TextContent()) {
			return true
		}
		if g.inCombatTurn(message) {
			return true
		}
	}
	return waiting
}

// mentionsBot — @username с границей: @dnd_bot_extra не считается нашим.
func mentionsBot(text, botUsername string) bool {
	if botUsername == "" || text == "" {
		return false
	}
	needle := "@" + strings.ToLower(botUsername)
	lower := strings.ToLower(text)
	offset := 0
	for {
		index := strings.Index(lower[offset:], needle)
		if index < 0 {
			return false
		}
		end := offset + index + len(needle)
		if end >= len(lower) || !isUsernameByte(lower[end]) {
			return true
		}
		offset = end
	}
}

func isUsernameByte(char byte) bool {
	return char == '_' ||
		(char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9')
}

func replyToBot(message *Message) bool {
	return message.ReplyToMessage != nil &&
		message.ReplyToMessage.From != nil &&
		message.ReplyToMessage.From.IsBot
}

var dmAddressPattern = regexp.MustCompile(
	`(?i)(^|[^\p{L}\p{N}])(дм|dm|гм|gm|мастер|мастера|мастеру|мастером|мастере|ведущий|ведущего|ведущему|ведущим|ведущем)([^\p{L}\p{N}]|$)`,
)

// addressesDM — прямое обращение к ведущему по роли, а не к другому игроку.
func addressesDM(text string) bool {
	if text == "" {
		return false
	}
	return dmAddressPattern.MatchString(text)
}

// inCombatTurn — сейчас боевой ход персонажа этого игрока.
func (g *Gate) inCombatTurn(message *Message) bool {
	if g.store == nil {
		return false
	}
	identity := IdentityFromMessage(message)
	if identity.UserID == "" {
		return false
	}
	campaign, err := FindCampaignForIdentity(g.store, identity)
	if err != nil || campaign == nil {
		return false
	}
	snapshot, err := g.store.LoadCombatState(campaign.Slug)
	if err != nil || snapshot == nil || !snapshot.Combat.Started {
		return false
	}
	current := snapshot.Combat.Current
	if current < 0 || current >= len(snapshot.Combat.Order) {
		return false
	}
	combatant := snapshot.Combat.Order[current]
	if combatant.Side != engine.SideParty {
		return false
	}
	needle := strings.ToLower(strings.TrimSpace(combatant.Name))
	if needle == "" {
		return false
	}
	sheets, err := g.store.ListCharacters(campaign.ID)
	if err != nil {
		return false
	}
	for _, sheet := range sheets {
		if sheet.OwnerUserID == identity.UserID && strings.ToLower(strings.TrimSpace(sheet.Name)) == needle {
			return true
		}
	}
	return false
}
