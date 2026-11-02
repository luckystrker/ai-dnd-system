// Package tools — игровые тулы DM и летописца поверх internal/engine и
// internal/store: одна реализация, два toolkit-а.
package tools

import (
	"context"
	"os"
	"strconv"
	"sync"

	"github.com/luckystriker/ai-dnd-system/internal/engine"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// Session — контекст вызова тула, который приходит через DI (aginject), а не
// из аргументов модели: кампания сессии, говорящий, чат/топик.
type Session struct {
	CampaignID      string
	CampaignSlug    string
	UserID          string
	Name            string
	Username        string
	ChatID          string
	MessageThreadID *int
	// Trusted — автоматизация (летописец): кампания задана кодом при запуске,
	// проверка членства не нужна, но и админ-тулы ей недоступны (их нет в
	// ChroniclerToolkit).
	Trusted bool
}

// DiceKey — ключ истории d20 для «доброго» псевдорандома: сессия чата (или
// кампания для автоматизации).
func (s *Session) DiceKey() string {
	if s == nil {
		return ""
	}
	key := s.CampaignID
	if s.ChatID != "" {
		key += ":" + s.ChatID
	}
	if s.MessageThreadID != nil {
		key += ":" + strconv.Itoa(*s.MessageThreadID)
	}
	if key == "" {
		key = "global"
	}
	return key
}

// ImageGenerator генерирует картинку сцены и возвращает URL.
type ImageGenerator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// ImagePoster публикует картинку в чат (реализация — Telegram, слайс 6).
type ImagePoster interface {
	PostImage(ctx context.Context, chatID string, messageThreadID *int, imageURL, caption string) error
}

// DayCloser — точка расширения advance_day: детерминированный запуск
// летописца появится в слайсе 5, тул уже умеет его вызывать.
type DayCloser interface {
	CloseDay(ctx context.Context, campaign *store.Campaign, closedDay int) error
}

// Deps — зависимости тулов. В тестах подменяются Random, Images, Poster,
// Dice и DayCloser; DB — временная SQLite.
type Deps struct {
	DB        *store.Store
	Random    engine.RandomSource
	Dice      *DiceHistory
	Images    ImageGenerator
	Poster    ImagePoster
	DayCloser DayCloser
	// CombatLocks сериализует read-modify-write боевого снимка по кампании:
	// ago исполняет вызовы одного ответа параллельно.
	CombatLocks *KeyedMutex
}

func (d Deps) withDefaults() Deps {
	if d.Random == nil {
		d.Random = engine.DefaultRandom
	}
	if d.Dice == nil {
		d.Dice = NewDiceHistory()
	}
	if d.CombatLocks == nil {
		d.CombatLocks = NewKeyedMutex()
	}
	if d.Images == nil {
		if config := RunwareFromEnv(os.Getenv); config != nil {
			d.Images = NewRunwareClient(*config)
		}
	}
	return d
}

// lockCampaign берёт замок кампании и возвращает unlock.
func (d Deps) lockCampaign(campaignID string) func() {
	if d.CombatLocks == nil {
		return func() {}
	}
	return d.CombatLocks.Lock(campaignID)
}

func (d Deps) random() engine.RandomSource {
	if d.Random == nil {
		return engine.DefaultRandom
	}
	return d.Random
}

// DiceHistory — последние грани d20 по сессии чата (для MakeLuckyRandom).
// В памяти процесса: как gameState-сессия в TS, история не является памятью
// кампании и теряется при рестарте — это допустимо решением владельца.
type DiceHistory struct {
	mu     sync.Mutex
	values map[string][]int
}

// NewDiceHistory создаёт пустую историю.
func NewDiceHistory() *DiceHistory {
	return &DiceHistory{values: map[string][]int{}}
}

// Recent возвращает копию последних бросков.
func (h *DiceHistory) Recent(key string) []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	values := h.values[key]
	out := make([]int, len(values))
	copy(out, values)
	return out
}

// Append добавляет бросок, оставляя последние 4 (как TS slice(-4)).
func (h *DiceHistory) Append(key string, roll int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	values := append(h.values[key], roll)
	if len(values) > 4 {
		values = values[len(values)-4:]
	}
	h.values[key] = values
}

// KeyedMutex — набор мьютексов по ключу (кампании). Записи не удаляются:
// число кампаний мало, а гонка с удалением сложнее пользы.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewKeyedMutex создаёт пустой набор.
func NewKeyedMutex() *KeyedMutex {
	return &KeyedMutex{locks: map[string]*sync.Mutex{}}
}

// Lock блокирует ключ до вызова возвращённой функции.
func (k *KeyedMutex) Lock(key string) func() {
	k.mu.Lock()
	lock, ok := k.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		k.locks[key] = lock
	}
	k.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}
