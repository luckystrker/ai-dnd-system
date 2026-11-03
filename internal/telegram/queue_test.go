package telegram

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// recorder — обработчик батчей для тестов очереди.
type recorder struct {
	mu      sync.Mutex
	batches []Batch
	block   chan struct{}
	started chan string
}

func newRecorder() *recorder {
	return &recorder{block: make(chan struct{}), started: make(chan string, 16)}
}

func (r *recorder) handle(_ context.Context, batch Batch) error {
	r.mu.Lock()
	r.batches = append(r.batches, batch)
	block := r.block
	r.mu.Unlock()
	select {
	case r.started <- batch.Key:
	default:
	}
	if block != nil {
		<-block
	}
	return nil
}

func (r *recorder) setBlock(block chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.block = block
}

func (r *recorder) snapshot() []Batch {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Batch, len(r.batches))
	copy(out, r.batches)
	return out
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches = nil
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("условие не наступило за 2с")
}

func incoming(chatID int64, userID int64, text string) Incoming {
	return Incoming{Message: gateMessage(chatID, "supergroup", userID, "den", text)}
}

// Сообщения одного чата обрабатываются строго по порядку и склеиваются в
// один ход, а сообщение, пришедшее во время хода, ждёт его конца.
func TestQueueSerializesOneChat(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	rec.setBlock(nil)
	queue, err := NewQueue(QueueConfig{Window: 100 * time.Millisecond, Handler: rec.handle})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(incoming(-1, 1, "первое")); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(incoming(-1, 1, "второе")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(rec.snapshot()) == 1 })
	batch := rec.snapshot()[0]
	if len(batch.Messages) != 2 {
		t.Fatalf("сообщений в батче = %d, want 2", len(batch.Messages))
	}
	if batch.Messages[0].Message.Text != "первое" || batch.Messages[1].Message.Text != "второе" {
		t.Fatalf("порядок нарушен: %+v", batch.Messages)
	}

	// Теперь ход идёт; сообщение, пришедшее во время хода, ждёт его конца.
	rec.reset()
	block := make(chan struct{})
	rec.setBlock(block)
	if err := queue.Enqueue(incoming(-1, 1, "третье")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(rec.snapshot()) == 1 })
	if err := queue.Enqueue(incoming(-1, 1, "четвёртое")); err != nil {
		t.Fatal(err)
	}
	close(block)
	waitFor(t, func() bool { return len(rec.snapshot()) == 2 })
	second := rec.snapshot()[1]
	if len(second.Messages) != 1 || second.Messages[0].Message.Text != "четвёртое" {
		t.Fatalf("сообщение во время хода не склеилось в следующий ход: %+v", second.Messages)
	}
	if err := queue.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// Разные чаты идут параллельно.
func TestQueueParallelChats(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	queue, err := NewQueue(QueueConfig{Window: time.Millisecond, Handler: rec.handle})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(incoming(-1, 1, "чат один")); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(incoming(-2, 2, "чат два")); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case key := <-rec.started:
			seen[key] = true
		case <-deadline:
			t.Fatalf("параллельные чаты не стартовали: %v", seen)
		}
	}
	close(rec.block)
	if err := queue.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// Падение обработчика не блокирует следующие ходы того же чата.
func TestQueueKeepsGoingAfterHandlerError(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		texts []string
	)
	handler := func(_ context.Context, batch Batch) error {
		mu.Lock()
		texts = append(texts, batch.Messages[0].Message.TextContent())
		first := len(texts) == 1
		mu.Unlock()
		if first {
			return errors.New("boom")
		}
		return nil
	}
	queue, err := NewQueue(QueueConfig{Window: time.Millisecond, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(incoming(-1, 1, "раз")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(texts) == 1
	})
	if err := queue.Enqueue(incoming(-1, 1, "два")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(texts) == 2
	})
	if err := queue.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Stop не теряет идущий ход: дожидается обработчика.
func TestQueueStopWaitsForRunningTurn(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	queue, err := NewQueue(QueueConfig{Window: time.Millisecond, Handler: rec.handle})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(incoming(-1, 1, "ход")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rec.started:
	case <-time.After(time.Second):
		t.Fatal("ход не стартовал")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- queue.Stop(context.Background()) }()
	select {
	case err := <-stopped:
		t.Fatalf("Stop вернулся до конца хода: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(rec.block)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop не дождался хода")
	}
}

func TestQueueEnqueueAfterStopFails(t *testing.T) {
	t.Parallel()

	queue, err := NewQueue(QueueConfig{Window: time.Millisecond, Handler: func(context.Context, Batch) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(incoming(-1, 1, "поздно")); !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("ошибка = %v, want ErrQueueClosed", err)
	}
}

// Stop не отменяет уже идущий ход: его контекст остаётся живым до конца.
func TestQueueStopLetsRunningTurnFinish(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var ctxErr error
	handler := func(ctx context.Context, _ Batch) error {
		close(started)
		<-release
		ctxErr = ctx.Err()
		return nil
	}
	queue, err := NewQueue(QueueConfig{Window: time.Millisecond, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(incoming(-1, 1, "ход")); err != nil {
		t.Fatal(err)
	}
	<-started
	stopped := make(chan error, 1)
	go func() { stopped <- queue.Stop(context.Background()) }()
	time.Sleep(10 * time.Millisecond)
	close(release)
	if err := <-stopped; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if ctxErr != nil {
		t.Fatalf("контекст хода отменён до конца: %v", ctxErr)
	}
}
