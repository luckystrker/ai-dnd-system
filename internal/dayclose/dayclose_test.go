package dayclose_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"

	"github.com/luckystriker/ai-dnd-system/internal/chronicler"
	"github.com/luckystriker/ai-dnd-system/internal/dayclose"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

type fakeArchivist struct {
	calls       []int
	campaignIDs []string
	highlights  []string
	err         error
	mutate      func(campaign *store.Campaign, day int) error
}

func (f *fakeArchivist) CloseDay(_ context.Context, campaign *store.Campaign, day int, highlights string) (chronicler.Report, error) {
	f.calls = append(f.calls, day)
	f.campaignIDs = append(f.campaignIDs, campaign.ID)
	f.highlights = append(f.highlights, highlights)
	if f.mutate != nil {
		if err := f.mutate(campaign, day); err != nil {
			return chronicler.Report{}, err
		}
	}
	return chronicler.Report{}, f.err
}

func newCampaignAtDayTwo(t *testing.T) (*store.Store, *store.Campaign) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "campaigns.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Тест", Length: store.LengthShort, Setting: "s", Theme: "t",
	}, store.NewOwnerInput{UserID: "dm-1"})
	if err != nil {
		t.Fatalf("campaign: %v", err)
	}
	author := "dm-1"
	if err := st.AppendTranscriptEntry(campaign.Slug, 1, store.TranscriptEntry{
		Kind: "player", Author: &author, Text: "Странник осматривает дверь",
	}); err != nil {
		t.Fatalf("transcript: %v", err)
	}
	if err := st.AppendTranscriptEntry(campaign.Slug, 1, store.TranscriptEntry{
		Kind: "dm", Text: "Дверь заперта, слышен скрип.",
	}); err != nil {
		t.Fatalf("transcript: %v", err)
	}
	if _, err := st.BindAndActivate(campaign.ID, "dm-1", store.BoundChat{ChatID: "-100"}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	updated, err := st.AdvanceDay(campaign.ID, "dm-1")
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	return st, updated
}

func queueWith(t *testing.T, st *store.Store, fake *fakeArchivist) *dayclose.Queue {
	t.Helper()
	queue, err := dayclose.New(dayclose.Config{
		Store: st,
		NewArchivist: func(*store.Campaign) (dayclose.Archivist, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	return queue
}

func TestQueueClosesDayThroughArchivist(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	fake := &fakeArchivist{}
	queue := queueWith(t, st, fake)
	if err := queue.CloseDay(context.Background(), campaign, 1); err != nil {
		t.Fatalf("CloseDay: %v", err)
	}
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0] != 1 {
		t.Fatalf("вызовы летописца = %v, want [1]", fake.calls)
	}
}

func TestQueueEnqueueIsIdempotent(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	fake := &fakeArchivist{}
	queue := queueWith(t, st, fake)
	for i := 0; i < 3; i++ {
		if err := queue.Enqueue(campaign.ID, 1, ""); err != nil {
			t.Fatalf("Enqueue %d: %v", i+1, err)
		}
	}
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("день закрыт %d раз, want 1", len(fake.calls))
	}
}

func TestQueueWritesFallbackDigestOnFailure(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	fake := &fakeArchivist{err: errors.New("модель недоступна")}
	queue := queueWith(t, st, fake)
	if err := queue.Enqueue(campaign.ID, 1, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := queue.RunPending(context.Background()); err == nil {
		t.Fatal("ожидалась ошибка летописца")
	}
	record, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if record == nil || record.Summary == nil || !strings.HasPrefix(*record.Summary, store.AutoDigestMark) {
		t.Fatalf("fallback-дайджест не записан: %+v", record)
	}
	if !strings.Contains(*record.Summary, "Дверь заперта") {
		t.Fatalf("дайджест не содержит события дня: %q", *record.Summary)
	}
}

// Летописец «отработал» без ошибки, но ничего не записал: день всё равно не
// остаётся без саммари.
func TestQueueWritesFallbackWhenArchivistWritesNothing(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	fake := &fakeArchivist{}
	queue := queueWith(t, st, fake)
	if err := queue.Enqueue(campaign.ID, 1, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	record, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if record == nil || record.Summary == nil || !strings.HasPrefix(*record.Summary, store.AutoDigestMark) {
		t.Fatalf("fallback-дайджест не записан: %+v", record)
	}
}

// Fallback не затирает уже записанное настоящее саммари.
func TestQueueFallbackKeepsExistingSummary(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	if err := st.SetDaySummary(campaign.Slug, 1, "настоящее саммари летописца"); err != nil {
		t.Fatalf("SetDaySummary: %v", err)
	}
	fake := &fakeArchivist{err: errors.New("сбой")}
	queue := queueWith(t, st, fake)
	if err := queue.Enqueue(campaign.ID, 1, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	_ = queue.RunPending(context.Background())
	record, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if record.Summary == nil || *record.Summary != "настоящее саммари летописца" {
		t.Fatalf("саммари затёрто: %+v", record.Summary)
	}
}

// После рестарта очередь дочитывает дни без саммари: durable-состояние — это
// отсутствие summary в campaign_days, отдельной таблицы нет.
func TestQueueScanGapsFindsUnclosedDays(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	fake := &fakeArchivist{}
	queue := queueWith(t, st, fake)
	queue.ScanGaps()
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0] != 1 {
		t.Fatalf("ScanGaps не подхватил день 1: %v", fake.calls)
	}

	if err := st.SetDaySummary(campaign.Slug, 1, "настоящее саммари"); err != nil {
		t.Fatalf("SetDaySummary: %v", err)
	}
	fake.calls = nil
	queue.ScanGaps()
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("закрытый день снова в очереди: %v", fake.calls)
	}
}

// День без записей закрывать нечем: летописец не вызывается и денег не тратит.
func TestQueueScanGapsSkipsEmptyDays(t *testing.T) {
	t.Parallel()

	st, _ := newCampaignAtDayTwo(t)
	// Новая кампания: день 2 наступил, но день 1 пуст.
	empty, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Пустой день", Length: store.LengthShort, Setting: "s", Theme: "t",
	}, store.NewOwnerInput{UserID: "dm-1"})
	if err != nil {
		t.Fatalf("campaign: %v", err)
	}
	if _, err := st.BindAndActivate(empty.ID, "dm-1", store.BoundChat{ChatID: "-200"}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := st.AdvanceDay(empty.ID, "dm-1"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	fake := &fakeArchivist{}
	queue := queueWith(t, st, fake)
	queue.ScanGaps()
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	for index, campaignID := range fake.campaignIDs {
		if campaignID == empty.ID {
			t.Fatalf("пустой день %d попал в очередь", fake.calls[index])
		}
	}
	// День 1 у кампании с транскриптом должен быть в очереди.
	if len(fake.calls) != 1 || fake.calls[0] != 1 {
		t.Fatalf("вызовы летописца = %v, want [1]", fake.calls)
	}
}

// Очередь без фабрики летописца всё равно закрывает день дайджестом.
func TestQueueWithoutArchivistFallsBack(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	queue, err := dayclose.New(dayclose.Config{Store: st})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if err := queue.Enqueue(campaign.ID, 1, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	record, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if record == nil || record.Summary == nil {
		t.Fatalf("день без саммари: %+v", record)
	}
}

// Сквозной сценарий: очередь выполняет настоящего летописца, и день получает
// саммари и ключевое событие из БД (а не из слов модели).
func TestQueueRunsRealChronicler(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	cfg := agotest.NewTestConfig(
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "c1", Name: "update_day_summary",
			Arguments: `{"day":1,"summary":"Партия вошла в трактир.","headline":"Трактир"}`,
		}}}},
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "c2", Name: "append_key_event",
			Arguments: `{"event":"Марта обещала помочь.","day":1}`,
		}}}},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Готово."}},
	)
	queue, err := dayclose.New(dayclose.Config{
		Store: st,
		NewArchivist: func(c *store.Campaign) (dayclose.Archivist, error) {
			return chronicler.New(chronicler.Config{Model: cfg, Store: st, CampaignID: c.ID})
		},
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if err := queue.CloseDay(context.Background(), campaign, 1); err != nil {
		t.Fatalf("CloseDay: %v", err)
	}
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	record, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if record == nil || record.Summary == nil || !strings.Contains(*record.Summary, "трактир") {
		t.Fatalf("саммари не записано: %+v", record)
	}
	permanent, regular, err := st.SplitKeyEvents(campaign.Slug)
	if err != nil {
		t.Fatalf("SplitKeyEvents: %v", err)
	}
	if len(permanent)+len(regular) != 1 {
		t.Fatalf("ключевые события = %v / %v, want 1", permanent, regular)
	}
}

type blockingArchivist struct{ started chan struct{} }

func (b *blockingArchivist) CloseDay(ctx context.Context, _ *store.Campaign, _ int, _ string) (chronicler.Report, error) {
	close(b.started)
	<-ctx.Done()
	return chronicler.Report{}, ctx.Err()
}

// Остановка очереди посреди работы летописца не пишет авто-дайджест: день
// остаётся без саммари и закрывается при следующем старте.
func TestQueueStopDoesNotWriteFallback(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	blocking := &blockingArchivist{started: make(chan struct{})}
	// Фабрика возвращает блокирующего летописца.
	queue, err := dayclose.New(dayclose.Config{
		Store: st,
		NewArchivist: func(*store.Campaign) (dayclose.Archivist, error) {
			return blocking, nil
		},
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue.Start(parent)
	if err := queue.Enqueue(campaign.ID, 1, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	<-blocking.started
	queue.Close()

	record, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if record != nil && record.Summary != nil {
		t.Fatalf("остановка записала дайджест навсегда: %+v", record.Summary)
	}

	// Следующий запуск подхватывает день и закрывает его настоящим саммари.
	restart := &fakeArchivist{mutate: func(c *store.Campaign, day int) error {
		return st.SetDaySummary(c.Slug, day, "настоящее саммари после рестарта")
	}}
	next := queueWith(t, st, restart)
	next.ScanGaps()
	if err := next.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending после рестарта: %v", err)
	}
	record, err = st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if record == nil || record.Summary == nil || *record.Summary != "настоящее саммари после рестарта" {
		t.Fatalf("день не закрыт после рестарта: %+v", record)
	}
}

// slowArchivist блокирует первый вызов, чтобы тест успел поставить тот же
// день в очередь во время закрытия.
type slowArchivist struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (s *slowArchivist) CloseDay(context.Context, *store.Campaign, int, string) (chronicler.Report, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == 1 {
		close(s.started)
		<-s.release
	}
	return chronicler.Report{}, nil
}

func (s *slowArchivist) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// Пока день закрывается, повторный Enqueue того же дня не запускает второй
// летописец (регрессия: pop удалял ключ до выполнения, и дубль проходил).
func TestQueueDeduplicatesDayInFlight(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	slow := &slowArchivist{started: make(chan struct{}), release: make(chan struct{})}
	queue, err := dayclose.New(dayclose.Config{
		Store: st,
		NewArchivist: func(*store.Campaign) (dayclose.Archivist, error) {
			return slow, nil
		},
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue.Start(ctx) // ScanGaps ставит день 1
	select {
	case <-slow.started:
	case <-time.After(5 * time.Second):
		t.Fatal("летописец не запустился")
	}
	if err := queue.Enqueue(campaign.ID, 1, ""); err != nil {
		t.Fatalf("Enqueue во время закрытия: %v", err)
	}
	close(slow.release)

	// Ждём, пока очередь закончит день и напишет авто-дайджест.
	deadline := time.Now().Add(5 * time.Second)
	for {
		record, err := st.ReadDay(campaign.Slug, 1)
		if err != nil {
			t.Fatalf("ReadDay: %v", err)
		}
		if record != nil && record.Summary != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("день не закрыт после снятия блокировки: %+v", record)
		}
		time.Sleep(10 * time.Millisecond)
	}
	queue.Close()
	if calls := slow.count(); calls != 1 {
		t.Fatalf("летописец вызван %d раз, want 1: дубль во время закрытия", calls)
	}
}

// panicOnceArchivist паникует на первом вызове: проверяет, что ключ дня
// снимается и паника не залипает в очереди.
type panicOnceArchivist struct {
	mu    sync.Mutex
	calls int
}

func (p *panicOnceArchivist) CloseDay(context.Context, *store.Campaign, int, string) (chronicler.Report, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		panic("archivist boom")
	}
	return chronicler.Report{}, nil
}

func (p *panicOnceArchivist) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// Паника воркера в RunPending уходит вызывающему, но ключ дня снимается:
// повторная постановка того же дня снова выполняется, а не схлопывается.
func TestQueueReleasesDayAfterPanic(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	flaky := &panicOnceArchivist{}
	queue := queueWithPanic(t, st, flaky)
	if err := queue.CloseDay(context.Background(), campaign, 1); err != nil {
		t.Fatalf("CloseDay: %v", err)
	}
	func() {
		defer func() {
			if rec := recover(); rec == nil {
				t.Fatal("ожидалась паника воркера")
			}
		}()
		_ = queue.RunPending(context.Background())
	}()
	if flaky.count() != 1 {
		t.Fatalf("вызовов летописца = %d, want 1", flaky.count())
	}
	if err := queue.CloseDay(context.Background(), campaign, 1); err != nil {
		t.Fatalf("повторный CloseDay: %v", err)
	}
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending после паники: %v", err)
	}
	if flaky.count() != 2 {
		t.Fatalf("повторный прогон не выполнился: вызовов = %d, want 2", flaky.count())
	}
}

func queueWithPanic(t *testing.T, st *store.Store, archivist *panicOnceArchivist) *dayclose.Queue {
	t.Helper()
	queue, err := dayclose.New(dayclose.Config{
		Store: st,
		NewArchivist: func(*store.Campaign) (dayclose.Archivist, error) {
			return archivist, nil
		},
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	return queue
}

// BackfillMax ограничивает число полных прогонов летописца на старт: старые
// дни обрабатываются от новых к старым.
func TestQueueScanGapsRespectsBackfillLimit(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	for day := 2; day <= 30; day++ {
		if err := st.AppendTranscriptEntry(campaign.Slug, day, store.TranscriptEntry{
			Kind: "dm", Text: "Событие дня.",
		}); err != nil {
			t.Fatalf("transcript day %d: %v", day, err)
		}
		if _, err := st.AdvanceDay(campaign.ID, "dm-1"); err != nil {
			t.Fatalf("advance day %d: %v", day, err)
		}
	}
	fake := &fakeArchivist{}
	queue, err := dayclose.New(dayclose.Config{
		Store:       st,
		BackfillMax: 2,
		NewArchivist: func(*store.Campaign) (dayclose.Archivist, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	queue.ScanGaps()
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("вызовов летописца = %d, want 2", len(fake.calls))
	}
	if fake.calls[0] != 30 || fake.calls[1] != 29 {
		t.Fatalf("закрыты дни %v, want [30 29] (от новых к старым)", fake.calls)
	}
}

// День, закрываемый задним числом, получает предупреждение не затирать
// актуальное состояние мира.
func TestQueueWarnsOnRetroactiveDay(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	for day := 2; day <= 5; day++ {
		if err := st.AppendTranscriptEntry(campaign.Slug, day, store.TranscriptEntry{
			Kind: "dm", Text: "Событие дня.",
		}); err != nil {
			t.Fatalf("transcript day %d: %v", day, err)
		}
		if _, err := st.AdvanceDay(campaign.ID, "dm-1"); err != nil {
			t.Fatalf("advance day %d: %v", day, err)
		}
	}
	fake := &fakeArchivist{}
	queue := queueWith(t, st, fake)
	if err := queue.Enqueue(campaign.ID, 1, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(fake.highlights) != 1 || !strings.Contains(fake.highlights[0], "задним числом") {
		t.Fatalf("нет предупреждения: %v", fake.highlights)
	}
	if !strings.Contains(fake.highlights[0], "день 6") {
		t.Fatalf("в предупреждении нет текущего дня: %q", fake.highlights[0])
	}
}

// Старые дни без саммари получают только дайджест; летописец — не более
// BackfillMax раз и только для последних BackfillDays дней.
func TestQueueBackfillsOldDaysWithDigest(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	for day := 2; day <= 30; day++ {
		if err := st.AppendTranscriptEntry(campaign.Slug, day, store.TranscriptEntry{
			Kind: "dm", Text: "Событие дня.",
		}); err != nil {
			t.Fatalf("transcript day %d: %v", day, err)
		}
		if _, err := st.AdvanceDay(campaign.ID, "dm-1"); err != nil {
			t.Fatalf("advance day %d: %v", day, err)
		}
	}
	fake := &fakeArchivist{}
	queue, err := dayclose.New(dayclose.Config{Store: st, NewArchivist: func(*store.Campaign) (dayclose.Archivist, error) {
		return fake, nil
	}})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	queue.ScanGaps()
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(fake.calls) != 2 || fake.calls[0] != 30 || fake.calls[1] != 29 {
		t.Fatalf("летописец зван %v, want [30 29]", fake.calls)
	}
	for day := 1; day <= 30; day++ {
		record, err := st.ReadDay(campaign.Slug, day)
		if err != nil {
			t.Fatalf("ReadDay %d: %v", day, err)
		}
		if record == nil || record.Summary == nil {
			t.Fatalf("день %d остался без саммари", day)
		}
	}
}

// Авто-дайджест в окне возраста один раз повышается летописцем; после
// неудачной попытки маркер делает её окончательной и переживает рестарт.
func TestQueueUpgradesAutoDigestOnce(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	if err := st.SetDaySummary(campaign.Slug, 1, store.AutoDigestMark+" старый дайджест"); err != nil {
		t.Fatalf("SetDaySummary: %v", err)
	}
	fake := &fakeArchivist{mutate: func(c *store.Campaign, day int) error {
		return st.SetDaySummary(c.Slug, day, "настоящее саммари")
	}}
	queue := queueWith(t, st, fake)
	queue.ScanGaps()
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	record, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if record.Summary == nil || *record.Summary != "настоящее саммари" {
		t.Fatalf("дайджест не повышен: %+v", record.Summary)
	}

	// Вторая попытка: летописец ничего не пишет — дайджест помечается
	// провалившимся и больше не трогается.
	if err := st.SetDaySummary(campaign.Slug, 1, store.AutoDigestMark+" снова дайджест"); err != nil {
		t.Fatalf("SetDaySummary: %v", err)
	}
	failing := &fakeArchivist{}
	second := queueWith(t, st, failing)
	second.ScanGaps()
	if err := second.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending 2: %v", err)
	}
	if len(failing.calls) != 1 {
		t.Fatalf("попыток повышения = %d, want 1", len(failing.calls))
	}
	record, err = st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if record.Summary == nil || !strings.Contains(*record.Summary, "повышение до саммари не удалось") {
		t.Fatalf("нет маркера неудачи: %+v", record.Summary)
	}
	failing.calls = nil
	second.ScanGaps()
	if err := second.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending 3: %v", err)
	}
	if len(failing.calls) != 0 {
		t.Fatalf("провалившийся дайджест снова в очереди: %v", failing.calls)
	}
}

// Дайджест старше окна возраста окончателен: летописец его не трогает.
func TestQueueOldDigestIsFinal(t *testing.T) {
	t.Parallel()

	st, campaign := newCampaignAtDayTwo(t)
	for day := 2; day <= 9; day++ {
		if _, err := st.AdvanceDay(campaign.ID, "dm-1"); err != nil {
			t.Fatalf("advance day %d: %v", day, err)
		}
	}
	if err := st.SetDaySummary(campaign.Slug, 1, store.AutoDigestMark+" старый дайджест"); err != nil {
		t.Fatalf("SetDaySummary: %v", err)
	}
	fake := &fakeArchivist{}
	queue := queueWith(t, st, fake)
	queue.ScanGaps()
	if err := queue.RunPending(context.Background()); err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("старый дайджест поднят летописцем: %v", fake.calls)
	}
}
