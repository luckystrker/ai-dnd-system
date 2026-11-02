// Package dayclose — детерминированное закрытие игрового дня.
//
// Очередь в процессе живёт поверх durable-состояния кампании: сам факт
// «день закрыт» — это непустые summary или headline в campaign_days. Новых
// таблиц нет, схема БД остаётся 1:1 с TS. После рестарта ScanGaps дочитывает
// дни, у которых саммари так и не появилось (отсутствие summary и есть
// durable-задача), поэтому недозакрытый день не теряется.
//
// Провал летописца или его «пустой» прогон закрывается детерминированным
// дайджестом (store.BuildDayDigest) — день никогда не остаётся без саммари.
// Повторных попыток летописца внутри процесса нет намеренно: сбой обычно
// детерминированный (ключ, контекст), а платный повтор удваивает стоимость;
// падение процесса лечится ScanGaps при следующем старте.
package dayclose

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/luckystriker/ai-dnd-system/internal/chronicler"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// Archivist — закрытие дня летописцем. Интерфейс, чтобы очередь тестировалась
// без LLM.
type Archivist interface {
	CloseDay(ctx context.Context, campaign *store.Campaign, day int, highlights string) (chronicler.Report, error)
}

// ArchivistFactory строит летописца под конкретную кампанию: очередь
// многокампанейная, а сессия летописца привязана к одной кампании.
type ArchivistFactory func(campaign *store.Campaign) (Archivist, error)

// Config — настройки очереди.
type Config struct {
	Store *store.Store
	// NewArchivist nil — дни закрываются только fallback-дайджестом.
	NewArchivist ArchivistFactory
	// Timeout ограничивает один прогон летописца; ноль — 3 минуты.
	Timeout time.Duration
	// Logger по умолчанию slog.Default().
	Logger *slog.Logger
	// DigestSentences — сколько предложений брать в fallback-дайджест.
	DigestSentences int
}

// Queue — in-process очередь закрытия дней. Поток закрытия один: прогоны
// летописца не конкурируют за одну кампанию и не множат платные вызовы.
type Queue struct {
	store     *store.Store
	newWorker ArchivistFactory
	timeout   time.Duration
	log       *slog.Logger
	sentences int

	mu      sync.Mutex
	pending map[string]job
	order   []string
	closed  bool
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	wake    chan struct{}
}

type job struct {
	campaignID string
	day        int
	highlights string
}

// New создаёт очередь. Start запускает обработчик и досмотр пропусков.
func New(cfg Config) (*Queue, error) {
	if cfg.Store == nil {
		return nil, errors.New("dayclose: store is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	sentences := cfg.DigestSentences
	if sentences <= 0 {
		sentences = 5
	}
	return &Queue{
		store: cfg.Store, newWorker: cfg.NewArchivist, timeout: timeout,
		log: logger, sentences: sentences,
		pending: map[string]job{}, wake: make(chan struct{}, 1),
	}, nil
}

// Start досматривает пропуски прошлых дней и запускает фоновый обработчик.
// Повторный вызов — no-op.
func (q *Queue) Start(parent context.Context) {
	q.mu.Lock()
	if q.started || q.closed {
		q.mu.Unlock()
		return
	}
	q.started = true
	ctx, cancel := context.WithCancel(parent)
	q.cancel = cancel
	q.mu.Unlock()

	q.ScanGaps()
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		q.loop(ctx)
	}()
}

// Close останавливает обработчик. Невыполненные задачи не теряются: их
// подхватит ScanGaps при следующем старте.
func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	cancel := q.cancel
	q.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	q.wg.Wait()
}

// CloseDay реализует tools.DayCloser: ставит закрытие в очередь и сразу
// возвращает управление — день уже сдвинут, модель не должна повторять
// advance_day и ждать летописца.
func (q *Queue) CloseDay(_ context.Context, campaign *store.Campaign, closedDay int) error {
	if campaign == nil {
		return errors.New("dayclose: campaign is required")
	}
	return q.Enqueue(campaign.ID, closedDay, "")
}

// Enqueue добавляет задачу; повторная постановка того же дня схлопывается.
func (q *Queue) Enqueue(campaignID string, day int, highlights string) error {
	if campaignID == "" || day <= 0 {
		return errors.New("dayclose: campaign and day are required")
	}
	key := fmt.Sprintf("%s:%d", campaignID, day)
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return errors.New("dayclose: queue is closed")
	}
	if _, exists := q.pending[key]; !exists {
		q.pending[key] = job{campaignID: campaignID, day: day, highlights: highlights}
		q.order = append(q.order, key)
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

// RunPending синхронно обрабатывает очередь (тесты и ручной дренаж).
func (q *Queue) RunPending(ctx context.Context) error {
	var firstErr error
	for {
		task, ok := q.pop()
		if !ok {
			return firstErr
		}
		if err := q.run(ctx, task); err != nil && firstErr == nil {
			firstErr = err
		}
	}
}

// ScanGaps ставит в очередь прошлые дни без саммари. Это и есть durable
// восстановление: состояние дня уже лежит в SQLite, отдельная таблица не
// нужна. Дни без записей закрывать нечем — они пропускаются.
func (q *Queue) ScanGaps() {
	campaigns, err := q.store.ListCampaigns()
	if err != nil {
		q.log.Error("dayclose: list campaigns failed", "error", err)
		return
	}
	for _, campaign := range campaigns {
		if campaign.Status != store.StatusActive {
			continue
		}
		current := 1
		if campaign.CurrentDay != nil {
			current = *campaign.CurrentDay
		}
		for day := 1; day < current; day++ {
			record, err := q.store.ReadDay(campaign.Slug, day)
			if err != nil {
				q.log.Error("dayclose: read day failed", "campaign", campaign.ID, "day", day, "error", err)
				continue
			}
			if !hasContent(record) || dayClosed(record) {
				continue
			}
			if err := q.Enqueue(campaign.ID, day, ""); err != nil {
				q.log.Error("dayclose: enqueue gap failed", "campaign", campaign.ID, "day", day, "error", err)
				return
			}
		}
	}
}

// dayClosed: саммари уже есть (включая авто-дайджест) или летописец записал
// шапку дня.
func dayClosed(record *store.DayRecord) bool {
	if record == nil {
		return true
	}
	return nonEmpty(record.Summary) || nonEmpty(record.Headline)
}

func hasContent(record *store.DayRecord) bool {
	return record != nil && len(record.Entries) > 0
}

func nonEmpty(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

func (q *Queue) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		}
		for {
			task, ok := q.pop()
			if !ok {
				break
			}
			if err := q.run(ctx, task); err != nil {
				q.log.Error("dayclose: close day failed", "campaign", task.campaignID, "day", task.day, "error", err)
			}
		}
	}
}

func (q *Queue) pop() (job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.order) == 0 {
		return job{}, false
	}
	key := q.order[0]
	q.order = q.order[1:]
	task, ok := q.pending[key]
	delete(q.pending, key)
	return task, ok
}

func (q *Queue) run(parent context.Context, task job) error {
	campaign, err := q.store.GetCampaign(task.campaignID)
	if err != nil {
		return err
	}
	if campaign == nil {
		return fmt.Errorf("dayclose: campaign %s not found", task.campaignID)
	}
	record, err := q.store.ReadDay(campaign.Slug, task.day)
	if err != nil {
		return err
	}
	// Нечего закрывать или день уже закрыт (повторная постановка) — не тратим
	// вызов модели.
	if !hasContent(record) || dayClosed(record) {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, q.timeout)
	defer cancel()

	if q.newWorker == nil {
		q.fallback(campaign, task.day)
		return nil
	}
	worker, err := q.newWorker(campaign)
	if err != nil {
		q.fallback(campaign, task.day)
		return err
	}
	report, err := worker.CloseDay(ctx, campaign, task.day, task.highlights)
	if err != nil {
		q.fallback(campaign, task.day)
		return err
	}
	q.log.Info("dayclose: day closed",
		"campaign", campaign.ID, "day", task.day,
		"summary", report.DaySummaryWritten, "keyEvents", report.KeyEventsWritten,
		"npcs", report.NpcsUpdated, "characters", report.CharactersUpdated,
		"locations", report.LocationsUpdated, "factions", report.FactionsUpdated,
		"world", report.WorldChangesWritten)
	// Летописец мог «отработать» без ошибки, но ничего не записать: день всё
	// равно не должен остаться без саммари.
	after, readErr := q.store.ReadDay(campaign.Slug, task.day)
	if readErr == nil && !dayClosed(after) {
		q.fallback(campaign, task.day)
	}
	return nil
}

// fallback пишет детерминированный дайджест, но никогда не затирает
// человеческое саммари или шапку дня.
func (q *Queue) fallback(campaign *store.Campaign, day int) {
	record, err := q.store.ReadDay(campaign.Slug, day)
	if err != nil {
		q.log.Error("dayclose: fallback read day failed", "campaign", campaign.ID, "day", day, "error", err)
		return
	}
	if !hasContent(record) || dayClosed(record) {
		return
	}
	digest := store.BuildDayDigest(record.Entries, q.sentences)
	if digest == "" {
		return
	}
	if err := q.store.SetDaySummary(campaign.Slug, day, digest); err != nil {
		q.log.Error("dayclose: fallback digest failed", "campaign", campaign.ID, "day", day, "error", err)
		return
	}
	q.log.Warn("dayclose: chronicler failed, wrote fallback digest", "campaign", campaign.ID, "day", day)
}
