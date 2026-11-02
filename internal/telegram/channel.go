package telegram

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/luckystrker/ago/stream"

	"github.com/luckystriker/ai-dnd-system/internal/dm"
	"github.com/luckystriker/ai-dnd-system/internal/store"
	"github.com/luckystriker/ai-dnd-system/internal/tools"
)

// API — методы Bot API, которые использует канал (fake в тестах).
type API interface {
	GetUpdates(ctx context.Context, offset int64, timeout time.Duration, allowed []string) ([]RawUpdate, error)
	DeleteWebhook(ctx context.Context) error
	SendMessage(ctx context.Context, chatID string, threadID *int, text, parseMode string) error
	SendRichText(ctx context.Context, chatID string, threadID *int, text string) error
	SendChatAction(ctx context.Context, chatID string, threadID *int, action string) error
	SendPhoto(ctx context.Context, chatID string, threadID *int, photo, caption string) error
	GetFile(ctx context.Context, fileID string) (FileInfo, error)
	DownloadFile(ctx context.Context, info FileInfo, maxBytes int64) ([]byte, error)
	AnswerCallbackQuery(ctx context.Context, callbackID, text string) error
}

// DMFactory собирает DM-агента чата: durable-поток истории и сессия,
// обновляемая перед каждым ходом. Постер вложений и модель задаёт вызывающий
// код (боевой конфиг или тест).
type DMFactory func(chatKey string, conversation stream.Stream, session func() *tools.Session) (*dm.DM, error)

// Config — настройки канала.
type Config struct {
	API            API
	Store          *store.Store
	BotUsername    string
	CoalesceWindow time.Duration
	Limits         Limits
	// MaxUpdateBytes защищает от аномальных payload (по умолчанию 1 МБ).
	MaxUpdateBytes int
	// MaxAttachmentBytes — политика вложений TS: 25 МБ.
	MaxAttachmentBytes int64
	// PollTimeout — long-poll getUpdates (по умолчанию 50 с).
	PollTimeout time.Duration
	// ErrorBackoff — пауза после ошибки long-poll (по умолчанию 5 с).
	ErrorBackoff time.Duration
	NewDM        DMFactory
	Logger       *slog.Logger
	Clock        func() time.Time
}

const (
	// DefaultPollTimeout — long-poll getUpdates.
	DefaultPollTimeout = 50 * time.Second
	// DefaultErrorBackoff — пауза после ошибки Telegram.
	DefaultErrorBackoff = 5 * time.Second
	// DefaultMaxUpdateBytes — предел размера входящего апдейта.
	DefaultMaxUpdateBytes = 1024 * 1024
	// DefaultHistoryEventCap — жёсткий кап durable-истории чата. Компакция
	// ago ограничивает историю по токенам (75% окна), кап — страховка от
	// раздувания таблицы: память кампании живёт в SQLite, а не в истории.
	DefaultHistoryEventCap = 600
)

// Channel — Telegram-канал: long-poll, durable-inbox, очередь чатов, гейт,
// отправка ответов.
type Channel struct {
	api     API
	store   *store.Store
	gate    *Gate
	limiter *SlidingWindowLimiter
	queue   *Queue
	log     *slog.Logger

	botUsername        string
	maxUpdateBytes     int
	maxAttachmentBytes int64
	pollTimeout        time.Duration
	errorBackoff       time.Duration
	limits             Limits
	clock              func() time.Time
	newDM              DMFactory

	mu sync.Mutex
	// chats — агенты по ключу чата. Выселение не делаем: оно должно
	// согласовываться с идущим ходом и очередью чата, иначе Close() агента
	// оборвёт ход. История при этом durable (SQLite), так что при росте
	// аудитории eviction можно добавить отдельной задачей; пока чистка — в Stop.
	chats map[string]*chatSession

	runCtx    context.Context
	runCancel context.CancelFunc
}

// chatSession — DM-агент чата и текущая сессия говорящего.
type chatSession struct {
	key  string
	dm   *dm.DM
	mu   sync.Mutex
	last *tools.Session
}

func (c *chatSession) session() *tools.Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		return &tools.Session{}
	}
	return c.last
}

func (c *chatSession) setSession(session *tools.Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = session
}

// NewChannel собирает канал.
func NewChannel(cfg Config) (*Channel, error) {
	if cfg.API == nil {
		return nil, errors.New("telegram: api is required")
	}
	if cfg.Store == nil {
		return nil, errors.New("telegram: store is required")
	}
	if cfg.NewDM == nil {
		return nil, errors.New("telegram: NewDM factory is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	window := cfg.CoalesceWindow
	if window <= 0 {
		window = DefaultCoalesceWindow
	}
	limits := cfg.Limits
	defaults := DefaultLimits()
	if limits.Window <= 0 {
		limits.Window = defaults.Window
	}
	if limits.ChatPerMinute <= 0 {
		limits.ChatPerMinute = defaults.ChatPerMinute
	}
	if limits.UserPerMinute <= 0 {
		limits.UserPerMinute = defaults.UserPerMinute
	}
	maxUpdate := cfg.MaxUpdateBytes
	if maxUpdate <= 0 {
		maxUpdate = DefaultMaxUpdateBytes
	}
	maxAttachment := cfg.MaxAttachmentBytes
	if maxAttachment <= 0 {
		maxAttachment = DefaultMaxDownloadBytes
	}
	pollTimeout := cfg.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = DefaultPollTimeout
	}
	backoff := cfg.ErrorBackoff
	if backoff <= 0 {
		backoff = DefaultErrorBackoff
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	channel := &Channel{
		api:                cfg.API,
		store:              cfg.Store,
		gate:               NewGate(cfg.Store, cfg.BotUsername, clock),
		limiter:            NewSlidingWindowLimiter(),
		log:                logger,
		botUsername:        cfg.BotUsername,
		maxUpdateBytes:     maxUpdate,
		maxAttachmentBytes: maxAttachment,
		pollTimeout:        pollTimeout,
		errorBackoff:       backoff,
		limits:             limits,
		clock:              clock,
		chats:              map[string]*chatSession{},
	}
	channel.queue, _ = NewQueue(QueueConfig{
		Window: window, Handler: channel.handleBatch, Logger: logger,
	})
	channel.newDM = cfg.NewDM
	return channel, nil
}

// Sender реализует tools.ImagePoster через Bot API и не отправляет картинку,
// если ход уже завершился (нейтральный ответ/отмена).
type Sender struct {
	api API
}

// NewSender создаёт постер.
func NewSender(api API) *Sender { return &Sender{api: api} }

// ErrTurnOver — ход завершился, отправка не выполняется.
var ErrTurnOver = errors.New("telegram: turn is over")

// PostImage публикует картинку сцены.
func (s *Sender) PostImage(ctx context.Context, chatID string, messageThreadID *int, imageURL, caption string) error {
	if turn := TurnFrom(ctx); turn != nil && !turn.Active() {
		return ErrTurnOver
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.api.SendPhoto(ctx, chatID, messageThreadID, imageURL, caption)
}

// Turn — маркер одного хода. Канал закрывает его сразу после возврата из
// агента и до отправки финального текста: тул, переживший дедлайн, уже не
// отправит картинку после нейтрального ответа.
type Turn struct {
	mu     sync.Mutex
	closed bool
}

// NewTurn создаёт открытый маркер хода.
func NewTurn() *Turn { return &Turn{} }

// Close закрывает маркер.
func (t *Turn) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
}

// Active — ход ещё можно доставлять.
func (t *Turn) Active() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.closed
}

type turnContextKey struct{}

// WithTurn кладёт маркер хода в контекст; его видят тулы через
// agocontext.TurnContext.
func WithTurn(ctx context.Context, turn *Turn) context.Context {
	return context.WithValue(ctx, turnContextKey{}, turn)
}

// TurnFrom достаёт маркер хода из контекста.
func TurnFrom(ctx context.Context) *Turn {
	turn, _ := ctx.Value(turnContextKey{}).(*Turn)
	return turn
}
