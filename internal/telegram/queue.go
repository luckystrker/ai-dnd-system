package telegram

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// ErrQueueClosed — очередь остановлена; новые сообщения приходят из inbox
// после рестарта.
var ErrQueueClosed = errors.New("telegram: chat queue is closed")

// DefaultCoalesceWindow — окно склейки сообщений чата. 2.5 секунды: две
// быстрые реплики одного игрока почти всегда приходят с интервалом 1–3 с,
// а задержка ответа остаётся в пределах восприятия.
const DefaultCoalesceWindow = 2500 * time.Millisecond

// Batch — склеенный ход одного чата: сообщения в порядке поступления.
type Batch struct {
	Key      string
	Messages []Incoming
}

// Last — последнее сообщение батча (по нему авторизуется ход).
func (b Batch) Last() *Message {
	if len(b.Messages) == 0 {
		return nil
	}
	return b.Messages[len(b.Messages)-1].Message
}

// ChatID — id чата батча.
func (b Batch) ChatID() string {
	if last := b.Last(); last != nil {
		return ChatIDString(last.Chat.ID)
	}
	return ""
}

// ThreadID — форум-топик батча.
func (b Batch) ThreadID() *int { return TopicOf(b.Last()) }

// BatchHandler выполняет один ход чата.
type BatchHandler func(ctx context.Context, batch Batch) error

// QueueConfig — настройки очереди.
type QueueConfig struct {
	Window  time.Duration
	Handler BatchHandler
	Logger  *slog.Logger
}

type chatQueue struct {
	pending []Incoming
	running bool
}

// Queue — очередь чатов: ходы одного чата строго последовательны, разные
// чаты идут параллельно. Сообщения, пришедшие за окно склейки или во время
// идущего хода, склеиваются в один ход.
//
// Склейка сознательно делается ПЕРЕД ходом, а не через run.Enqueue во время
// него: ago возвращает из Ask только последний ModelResponse, поэтому
// подмешанный в середине хода ввод дал бы второй ответ, а первый остался бы
// неотправленным. Очередь же гарантирует один текст на склейку.
type Queue struct {
	window  time.Duration
	handler BatchHandler
	log     *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	chats  map[string]*chatQueue
	closed bool
	wg     sync.WaitGroup
}

// NewQueue создаёт очередь.
func NewQueue(cfg QueueConfig) (*Queue, error) {
	if cfg.Handler == nil {
		return nil, errors.New("telegram: queue handler is required")
	}
	window := cfg.Window
	if window < 0 {
		window = 0
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Queue{
		window: window, handler: cfg.Handler, log: logger,
		ctx: ctx, cancel: cancel, chats: map[string]*chatQueue{},
	}, nil
}

// Enqueue добавляет сообщение в очередь чата.
func (q *Queue) Enqueue(item Incoming) error {
	if item.Message == nil {
		return nil
	}
	key := ChatKey(item.Message)
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return ErrQueueClosed
	}
	state, ok := q.chats[key]
	if !ok {
		state = &chatQueue{}
		q.chats[key] = state
	}
	state.pending = append(state.pending, item)
	start := !state.running
	if start {
		state.running = true
		q.wg.Add(1)
	}
	q.mu.Unlock()
	if start {
		go q.work(key, state)
	}
	return nil
}

// work — воркер одного чата. Первый батч ждёт окно склейки, следующие
// (накопившиеся за время хода) запускаются сразу.
func (q *Queue) work(key string, state *chatQueue) {
	defer q.wg.Done()
	first := true
	for {
		if first {
			timer := time.NewTimer(q.window)
			select {
			case <-q.ctx.Done():
				timer.Stop()
				q.finish(key, state)
				return
			case <-timer.C:
			}
			first = false
		}
		q.mu.Lock()
		if len(state.pending) == 0 || q.closed {
			q.mu.Unlock()
			q.finish(key, state)
			return
		}
		items := state.pending
		state.pending = nil
		q.mu.Unlock()

		batch := Batch{Key: key, Messages: items}
		if err := q.handler(q.ctx, batch); err != nil && !errors.Is(err, context.Canceled) {
			q.log.Error("telegram: turn failed", "chat", key, "error", err)
		}
		if q.ctx.Err() != nil {
			q.finish(key, state)
			return
		}
	}
}

func (q *Queue) finish(key string, state *chatQueue) {
	q.mu.Lock()
	defer q.mu.Unlock()
	state.running = false
	if len(state.pending) == 0 && q.chats[key] == state {
		delete(q.chats, key)
	}
}

// Stop останавливает очередь: новые ходы не стартуют, идущий ход
// дожидается. Если ctx истёк раньше — контекст ходов отменяется, и Stop ждёт
// их завершения.
func (q *Queue) Stop(ctx context.Context) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true
	q.mu.Unlock()

	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		q.cancel()
		<-done
		return ctx.Err()
	}
}

// Len — сколько сообщений ждёт в очереди (тесты/диагностика).
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	total := 0
	for _, state := range q.chats {
		total += len(state.pending)
	}
	return total
}
