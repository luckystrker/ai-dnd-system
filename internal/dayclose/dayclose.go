// Package dayclose — детерминированное закрытие игрового дня.
//
// Очередь в процессе живёт поверх durable-состояния кампании: сам факт
// «день закрыт» — это настоящее саммари/headline в campaign_days. Новых
// таблиц нет, схема БД остаётся 1:1 с TS. После рестарта ScanGaps дочитывает
// последние BackfillDays дней летописцем, более старые дни без саммари
// закрывает дешёвым дайджестом (без LLM и без записи в состояние мира).
//
// Провал летописца или его «пустой» прогон закрывается детерминированным
// дайджестом (store.BuildDayDigest) — день никогда не остаётся без саммари.
// Авто-дайджест в окне возраста один раз пробует повысить летописец; после
// неудачной попытки в саммари остаётся durable-маркер, и день окончателен
// даже после рестарта. Повторных попыток летописца внутри процесса нет
// намеренно: сбой обычно детерминированный (ключ, контекст), а платный
// повтор удваивает стоимость; падение процесса лечится ScanGaps при
// следующем старте.
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
	// BackfillMax — сколько пропущенных дней на старт максимум закрывать
	// полным летописцем. 0 — DefaultBackfillMax, отрицательное — не
	// догонять прошлые дни вовсе. Без лимита боевая база TS даёт шторм
	// вызовов на первом старте.
	BackfillMax int
	// BackfillDays — окно возраста дня: только последние N закрытых дней
	// закрываются летописцем. Более старые дни без саммари получают только
	// fallback-дайджест (без LLM и без записи в состояние мира).
	// 0 — DefaultBackfillDays.
	BackfillDays int
}

const (
	// DefaultBackfillMax — предохранитель на старт: полный летописец дорогой.
	DefaultBackfillMax = 2
	// DefaultBackfillDays — окно последних закрытых дней для летописца.
	DefaultBackfillDays = 2
	// autoDigestUpgradeFailedNote — durable-маркер: попытка повысить
	// авто-дайджест до настоящего саммари уже была и не удалась. Второй раз
	// день не трогаем даже после рестарта.
	autoDigestUpgradeFailedNote = " (повышение до саммари не удалось)"
)

// Queue — in-process очередь закрытия дней. Поток закрытия один: прогоны
// летописца не конкурируют за одну кампанию и не множат платные вызовы.
type Queue struct {
	store        *store.Store
	newWorker    ArchivistFactory
	timeout      time.Duration
	log          *slog.Logger
	sentences    int
	backfillMax  int
	backfillDays int

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
	backfillMax := cfg.BackfillMax
	if backfillMax == 0 {
		backfillMax = DefaultBackfillMax
	}
	backfillDays := cfg.BackfillDays
	if backfillDays <= 0 {
		backfillDays = DefaultBackfillDays
	}
	return &Queue{
		store: cfg.Store, newWorker: cfg.NewArchivist, timeout: timeout,
		log: logger, sentences: sentences, backfillMax: backfillMax,
		backfillDays: backfillDays,
		pending:      map[string]job{}, wake: make(chan struct{}, 1),
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

// ScanGaps досматривает прошлые дни при старте.
//
// Последние BackfillDays закрытых дней закрывает летописец (BackfillMax штук
// на старт, от новых к старым). Более старые дни без саммари получают только
// детерминированный дайджест: полный летописец дорог, а запись событий
// двадцатидневной давности поверх текущего мира вредна. День с авто-дайджестом
// в окне возраста один раз пробует повысить летописец; маркер неудачи делает
// попытку окончательной и переживает рестарт.
func (q *Queue) ScanGaps() {
	if q.backfillMax < 0 {
		return
	}
	campaigns, err := q.store.ListCampaigns()
	if err != nil {
		q.log.Error("dayclose: list campaigns failed", "error", err)
		return
	}
	type recentDay struct {
		campaign *store.Campaign
		day      int
	}
	recent := make([]recentDay, 0)
	for index := range campaigns {
		campaign := &campaigns[index]
		if campaign.Status != store.StatusActive {
			continue
		}
		current := campaignDay(campaign)
		threshold := current - q.backfillDays
		for day := current - 1; day >= 1; day-- {
			record, err := q.store.ReadDay(campaign.Slug, day)
			if err != nil {
				q.log.Error("dayclose: read day failed", "campaign", campaign.ID, "day", day, "error", err)
				continue
			}
			if !hasContent(record) || realSummary(record) || hasHeadline(record) {
				continue
			}
			if hasAnySummary(record) && !needsUpgrade(record) {
				// Авто-дайджест уже пробовали повысить — окончателен.
				continue
			}
			if day >= threshold {
				recent = append(recent, recentDay{campaign: campaign, day: day})
				continue
			}
			if !hasAnySummary(record) {
				// Старый день без саммари: только дешёвый дайджест, без LLM.
				q.fallback(campaign, day, false)
			}
		}
	}
	enqueued := 0
	for _, candidate := range recent {
		if enqueued >= q.backfillMax {
			// Лимит на старт исчерпан: день всё равно не должен остаться без
			// саммари. Дайджест можно будет повысить на следующем старте.
			q.fallback(candidate.campaign, candidate.day, false)
			continue
		}
		if err := q.Enqueue(candidate.campaign.ID, candidate.day, ""); err != nil {
			q.log.Error("dayclose: enqueue gap failed", "campaign", candidate.campaign.ID, "day", candidate.day, "error", err)
			return
		}
		enqueued++
	}
}

// realSummary — настоящее саммари, а не авто-дайджест.
func realSummary(record *store.DayRecord) bool {
	if record == nil || record.Summary == nil {
		return false
	}
	return strings.TrimSpace(*record.Summary) != "" && !strings.HasPrefix(*record.Summary, store.AutoDigestMark)
}

func hasHeadline(record *store.DayRecord) bool {
	return record != nil && nonEmpty(record.Headline)
}

func hasAnySummary(record *store.DayRecord) bool {
	return record != nil && nonEmpty(record.Summary)
}

// needsUpgrade: день закрыт авто-дайджестом, попытки повышения ещё не было.
func needsUpgrade(record *store.DayRecord) bool {
	if record == nil || record.Summary == nil {
		return false
	}
	summary := strings.TrimSpace(*record.Summary)
	if !strings.HasPrefix(summary, store.AutoDigestMark) {
		return false
	}
	return !strings.Contains(summary, autoDigestUpgradeFailedNote)
}

func hasContent(record *store.DayRecord) bool {
	return record != nil && len(record.Entries) > 0
}

func nonEmpty(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

func campaignDay(campaign *store.Campaign) int {
	if campaign == nil || campaign.CurrentDay == nil {
		return 1
	}
	return *campaign.CurrentDay
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
	// Нечего закрывать, день уже закрыт настоящим саммари/шапкой или
	// повышение дайджеста уже провалилось — не тратим вызов модели.
	if !hasContent(record) || realSummary(record) || hasHeadline(record) {
		return nil
	}
	if hasAnySummary(record) && !needsUpgrade(record) {
		return nil
	}
	upgrade := needsUpgrade(record)
	ctx, cancel := context.WithTimeout(parent, q.timeout)
	defer cancel()

	if q.newWorker == nil {
		if parent.Err() != nil {
			return parent.Err()
		}
		q.fallback(campaign, task.day, upgrade)
		return nil
	}
	worker, err := q.newWorker(campaign)
	if err != nil {
		if parent.Err() != nil {
			return err
		}
		q.fallback(campaign, task.day, upgrade)
		return err
	}
	highlights := task.highlights
	if current := campaignDay(campaign); task.day < current-1 {
		note := fmt.Sprintf("День %d закрывается задним числом: сейчас идёт день %d. "+
			"Обновляй только память этого дня; не перезаписывай актуальное состояние "+
			"персонажей, NPC, локаций, квестов и мира более поздними событиями — "+
			"помечай такие факты как прошлые.", task.day, current)
		if strings.TrimSpace(highlights) == "" {
			highlights = note
		} else {
			highlights += "\n" + note
		}
	}
	report, err := worker.CloseDay(ctx, campaign, task.day, highlights)
	if err != nil {
		// Остановка очереди (деплой/рестарт) — не провал летописца: не пишем
		// дайджест, иначе ScanGaps посчитает день закрытым навсегда. День
		// останется без саммари и будет подхвачен при следующем старте.
		if parent.Err() != nil {
			return err
		}
		q.fallback(campaign, task.day, upgrade)
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
	if readErr == nil && !realSummary(after) && !hasHeadline(after) {
		if parent.Err() != nil {
			return parent.Err()
		}
		q.fallback(campaign, task.day, upgrade)
	}
	return nil
}

// fallback пишет детерминированный дайджест, но никогда не затирает
// настоящее саммари или шапку дня. attemptedUpgrade помечает неудачную
// попытку повышения: второй раз день уже не трогаем даже после рестарта.
func (q *Queue) fallback(campaign *store.Campaign, day int, attemptedUpgrade bool) {
	record, err := q.store.ReadDay(campaign.Slug, day)
	if err != nil {
		q.log.Error("dayclose: fallback read day failed", "campaign", campaign.ID, "day", day, "error", err)
		return
	}
	if !hasContent(record) || realSummary(record) || hasHeadline(record) {
		return
	}
	if hasAnySummary(record) && !needsUpgrade(record) {
		return
	}
	digest := store.BuildDayDigest(record.Entries, q.sentences)
	if digest == "" {
		return
	}
	if attemptedUpgrade {
		digest += autoDigestUpgradeFailedNote
	}
	if err := q.store.SetDaySummary(campaign.Slug, day, digest); err != nil {
		q.log.Error("dayclose: fallback digest failed", "campaign", campaign.ID, "day", day, "error", err)
		return
	}
	q.log.Info("dayclose: fallback digest written", "campaign", campaign.ID, "day", day, "upgrade", attemptedUpgrade)
}
