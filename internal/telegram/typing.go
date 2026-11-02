package telegram

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/luckystriker/ai-dnd-system/internal/tools"
)

// TypingThrottle — Telegram держит индикатор «печатает…» около пяти секунд,
// поэтому чаще раза в 4 секунды его отправлять бессмысленно.
const TypingThrottle = 4 * time.Second

// typingSweepThreshold — порог, после которого Activity вычищает записи last,
// которым больше не нужен троттл. Без чистки карта росла бы вместе с числом
// чатов, где бот когда-либо печатал.
const typingSweepThreshold = 1000

// ChatActionSender — минимум Bot API, нужный индикатору набора (его
// реализует и Client, и fake в тестах).
type ChatActionSender interface {
	SendChatAction(ctx context.Context, chatID string, threadID *int, action string) error
}

// TypingNotifier показывает «печатает…» в чате текущей сессии. Вызовы
// приходят из middleware хода (старт и каждый тул) и троттлятся.
type TypingNotifier struct {
	api      ChatActionSender
	throttle time.Duration
	mu       sync.Mutex
	last     map[string]time.Time
	now      func() time.Time
}

// NewTypingNotifier создаёт нотификатор.
func NewTypingNotifier(api ChatActionSender) *TypingNotifier {
	return &TypingNotifier{
		api: api, throttle: TypingThrottle, last: map[string]time.Time{}, now: time.Now,
	}
}

// SetClock подменяет часы (тесты).
func (n *TypingNotifier) SetClock(clock func() time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.now = clock
}

// Activity возвращает callback для dm.Config.OnToolActivity: он читает
// актуальную сессию хода (говорящий и чат меняются).
func (n *TypingNotifier) Activity(session func() *tools.Session) func() {
	return func() {
		if n == nil || n.api == nil || session == nil {
			return
		}
		current := session()
		if current == nil || current.ChatID == "" || current.ChatType == "channel" {
			return
		}
		key := current.ChatID
		if current.MessageThreadID != nil {
			key += ":" + strconv.Itoa(*current.MessageThreadID)
		}
		n.mu.Lock()
		now := n.now()
		if last, ok := n.last[key]; ok && now.Sub(last) < n.throttle {
			n.mu.Unlock()
			return
		}
		n.last[key] = now
		if len(n.last) > typingSweepThreshold {
			cutoff := now.Add(-10 * n.throttle)
			for chat, at := range n.last {
				if at.Before(cutoff) {
					delete(n.last, chat)
				}
			}
		}
		n.mu.Unlock()

		threadID := current.MessageThreadID
		chatID := current.ChatID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = n.api.SendChatAction(ctx, chatID, threadID, "typing")
		}()
	}
}
