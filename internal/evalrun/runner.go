// Package evalrun исполняет eval-набор через настоящего DM-агента: временная
// SQLite, фикстура кампании и партии, реальные тулы, очередь летописца,
// подмена тула ошибкой (fault) и постусловия в БД.
package evalrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	agoconfig "github.com/luckystrker/ago/config"
	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/eval"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/middleware"
	"github.com/luckystrker/ago/stream"

	"github.com/luckystriker/ai-dnd-system/internal/dayclose"
	"github.com/luckystriker/ai-dnd-system/internal/dm"
	"github.com/luckystriker/ai-dnd-system/internal/engine"
	"github.com/luckystriker/ai-dnd-system/internal/evalgame"
	"github.com/luckystriker/ai-dnd-system/internal/store"
	ttrpg "github.com/luckystriker/ai-dnd-system/internal/tools"
)

const (
	// DefaultTurnTimeout — дедлайн одного хода в eval. Он короче боевого:
	// зависший ход не должен останавливать весь прогон.
	DefaultTurnTimeout = 120 * time.Second

	// DefaultRepeatConcurrency — сколько независимых прогонов идёт параллельно.
	DefaultRepeatConcurrency = 2

	// EvalChatID — чат фикстуры. Campaign привязан именно к нему, поэтому
	// тулы и память видят ту же кампанию, что и Telegram-ход.
	EvalChatID = "-1009900000001"
)

// ModelFactory собирает модель под конкретный id разговора: у OpenCode Go
// обязателен заголовок x-opencode-session, а провайдеру нужен стабильный
// session id на кейс. Живой прогон передаёт llm.Settings, офлайн — скрипт.
type ModelFactory func(sessionID string) (agoconfig.ModelConfig, error)

// RunnerConfig — настройки прогона.
type RunnerConfig struct {
	// Repeats — сколько раз прогнать каждый кейс (Pass@k / Pass^k). 0 → 1.
	Repeats int
	// Concurrency — параллельные прогоны. 0 → DefaultRepeatConcurrency.
	Concurrency int
	// Timeout одного хода. 0 → DefaultTurnTimeout.
	Timeout time.Duration
	// Random подменяет источник случайности во всех тулах. nil —
	// детерминированный источник набора.
	Random engine.RandomSource
	// NewArchivist собирает летописца под store конкретного прогона. nil —
	// очередь закрывает день детерминированным fallback-дайджестом (без LLM).
	NewArchivist func(st *store.Store) dayclose.ArchivistFactory
	// TempRoot — каталог для временных SQLite. Пусто — os.TempDir().
	TempRoot string
	Logger   *slog.Logger
}

// Runner исполняет Suite через настоящего DM-агента и настоящие тулы.
type Runner struct {
	suite evalgame.Suite
	model ModelFactory
	cfg   RunnerConfig
}

// NewRunner собирает раннер.
func NewRunner(suite evalgame.Suite, model ModelFactory, cfg RunnerConfig) (*Runner, error) {
	if model == nil {
		return nil, errors.New("evalrun: нужна фабрика модели")
	}
	if cfg.Repeats <= 0 {
		cfg.Repeats = 1
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = DefaultRepeatConcurrency
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTurnTimeout
	}
	if cfg.Random == nil {
		cfg.Random = evalgame.DeterministicRandom()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Runner{suite: suite, model: model, cfg: cfg}, nil
}

// RunOptions — что и сколько гонять.
type RunOptions struct {
	// Cases — имена кейсов; пусто — все активные на Slice.
	Cases []string
	// Slice — слайс набора. 0 → evalgame.DefaultSlice.
	Slice int
}

// Run исполняет набор.
func (r *Runner) Run(ctx context.Context, opts RunOptions) (*evalgame.Report, error) {
	slice := opts.Slice
	if slice == 0 {
		slice = evalgame.DefaultSlice
	}
	plans, err := r.selectCases(slice, opts.Cases)
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("evalrun: не выбрано ни одного кейса")
	}

	type task struct {
		index int
		plan  evalgame.Plan
		rep   int
	}
	var tasks []task
	for _, plan := range plans {
		for repeat := 0; repeat < r.cfg.Repeats; repeat++ {
			tasks = append(tasks, task{index: len(tasks), plan: plan, rep: repeat})
		}
	}
	results := make([]evalgame.RepeatReport, len(tasks))
	sem := make(chan struct{}, r.cfg.Concurrency)
	var wg sync.WaitGroup
	for _, item := range tasks {
		wg.Add(1)
		go func(item task) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[item.index] = r.runRepeat(ctx, item.plan, item.rep)
		}(item)
	}
	wg.Wait()

	return evalgame.BuildReport(plans, results, slice, r.cfg.Repeats, r.suite.MetaFor), nil
}

// runRepeat — один независимый прогон: временная БД, фикстура, DM, ход,
// очередь летописца, постусловия.
func (r *Runner) runRepeat(ctx context.Context, plan evalgame.Plan, repeat int) evalgame.RepeatReport {
	started := time.Now()
	report := evalgame.RepeatReport{Variant: plan.Variant, Scores: []eval.Score{}}
	dir, err := os.MkdirTemp(r.cfg.TempRoot, "ttrpg-eval-*")
	if err != nil {
		report.Err = err.Error()
		return report
	}
	defer os.RemoveAll(dir)

	st, err := store.Open(filepath.Join(dir, "campaigns.db"))
	if err != nil {
		report.Err = err.Error()
		return report
	}
	defer st.Close()

	meta := r.suite.MetaFor(plan.Base)
	fixture, err := buildFixture(st, plan.Base, meta)
	if err != nil {
		report.Err = err.Error()
		return report
	}

	var archivist dayclose.ArchivistFactory
	if r.cfg.NewArchivist != nil {
		archivist = r.cfg.NewArchivist(st)
	}
	queue, err := dayclose.New(dayclose.Config{
		Store:        st,
		NewArchivist: archivist,
		Logger:       r.cfg.Logger,
	})
	if err != nil {
		report.Err = err.Error()
		return report
	}
	defer queue.Close()

	closer := &evalCloser{queue: queue}
	modelCfg, err := r.model(evalSessionID(plan, repeat))
	if err != nil {
		report.Err = err.Error()
		return report
	}

	conversation := stream.NewMemoryStream()
	random := r.cfg.Random
	if plan.Random != nil {
		random = plan.Random
	}
	agent, err := dm.New(dm.Config{
		Model:               modelCfg,
		Store:               st,
		Stream:              conversation,
		Session:             fixture.session,
		DayCloser:           closer,
		TurnTimeout:         r.cfg.Timeout,
		ContextWindowTokens: dm.DefaultContextWindowTokens,
		MaxToolRounds:       dm.DefaultMaxToolRounds,
		ToolDeps: ttrpg.Deps{
			DB:          st,
			Random:      random,
			Dice:        ttrpg.NewDiceHistory(),
			CombatLocks: ttrpg.NewKeyedMutex(),
			DayCloser:   closer,
		},
		ExtraMiddleware: faultMiddleware(meta.Fault),
		Logger:          r.cfg.Logger,
	})
	if err != nil {
		report.Err = err.Error()
		return report
	}
	defer agent.Close()

	reply, askErr := agent.Ask(ctx, plan.Case.Input)
	// Очередь летописца доигрываем после хода: advance_day только ставит
	// задачу, как в бою.
	if err := closer.Drain(ctx); err != nil && askErr == nil {
		askErr = err
	}

	history, histErr := conversation.History().GetEvents()
	if histErr != nil {
		report.Err = histErr.Error()
		return report
	}
	trace := eval.BuildTrace(history)
	trace.AgentName = "dm"
	if reply != "" {
		trace.FinalResponse = reply
	}
	report.Output = trace.FinalResponse
	if askErr != nil {
		trace.Err = askErr.Error()
		report.Err = trace.Err
	} else if reply == dm.NeutralFailureReply {
		trace.Err = "dm: терминальный сбой хода (нейтральный ответ)"
		report.Err = trace.Err
	}
	report.Tokens = tokenTotal(trace)
	report.ToolCalls = trace.ToolCalls

	for _, scorer := range plan.Case.Scorers {
		report.Scores = append(report.Scores, eval.ScoreWith(ctx, scorer, trace))
	}
	report.Postconditions = checkPostconditions(st, fixture.campaignID, meta.Postconditions)
	report.Passed = passed(report)
	report.DurationMS = time.Since(started).Milliseconds()
	return report
}

// tokenTotal — сумма prompt+completion из Usage-событий трассы.
func tokenTotal(trace *eval.Trace) int64 {
	usage := trace.Tokens()
	var total int64
	if usage.PromptTokens != nil {
		total += *usage.PromptTokens
	}
	if usage.CompletionTokens != nil {
		total += *usage.CompletionTokens
	}
	return total
}

func passed(report evalgame.RepeatReport) bool {
	if report.Err != "" || len(report.Scores) == 0 {
		return false
	}
	for _, score := range report.Scores {
		if !score.Passed {
			return false
		}
	}
	for _, post := range report.Postconditions {
		if !post.Passed {
			return false
		}
	}
	return true
}

// selectCases фильтрует активные кейсы и раскрывает варианты.
func (r *Runner) selectCases(slice int, names []string) ([]evalgame.Plan, error) {
	active := r.suite.ActiveAt(slice)
	wanted := map[string]bool{}
	for _, name := range names {
		if strings.TrimSpace(name) != "" {
			wanted[name] = true
		}
	}
	var plans []evalgame.Plan
	for _, c := range active {
		if len(wanted) > 0 && !wanted[c.Name] {
			continue
		}
		plans = append(plans, evalgame.ExpandVariants(c)...)
	}
	if len(wanted) > 0 {
		for name := range wanted {
			found := false
			for _, plan := range plans {
				if plan.Base == name || plan.Name == name {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("evalrun: кейс %q не активен на слайсе %d", name, slice)
			}
		}
	}
	return plans, nil
}

func evalSessionID(plan evalgame.Plan, repeat int) string {
	name := strings.NewReplacer("/", "-", " ", "-").Replace(plan.Name)
	return fmt.Sprintf("ttrpg-eval-%s-%d", name, repeat)
}

// evalCloser ставит закрытие дня в настоящую очередь и синхронно доигрывает
// её после хода: в бою очередь живёт в фоне, а eval ждёт результата, чтобы
// проверить саммари.
type evalCloser struct {
	queue *dayclose.Queue
}

func (c *evalCloser) CloseDay(ctx context.Context, campaign *store.Campaign, closedDay int) error {
	if campaign == nil {
		return errors.New("evalrun: campaign is required")
	}
	return c.queue.Enqueue(campaign.ID, closedDay, "")
}

// Drain синхронно обрабатывает очередь летописца.
func (c *evalCloser) Drain(ctx context.Context) error { return c.queue.RunPending(ctx) }

// faultMiddleware подменяет результат тула ошибкой на N-м вызове. Счётчик
// живёт один ход (фабрика создаётся на ход), поэтому «call» — номер вызова в
// этом ходе.
func faultMiddleware(fault *evalgame.Fault) []middleware.Factory {
	if fault == nil {
		return nil
	}
	return []middleware.Factory{
		func() middleware.Middleware {
			return &faultMW{tool: fault.Tool, call: fault.Call, message: fault.Error}
		},
	}
}

type faultMW struct {
	middleware.Base
	mu      sync.Mutex
	tool    string
	call    int
	message string
	count   int
}

func (f *faultMW) OnToolExecution(next middleware.ToolExecFunc, ev *events.ToolCallEvent, cx *agocontext.Context) (events.Event, error) {
	if ev.Name != f.tool {
		return next(ev, cx)
	}
	f.mu.Lock()
	f.count++
	current := f.count
	f.mu.Unlock()
	if current != f.call {
		return next(ev, cx)
	}
	return events.FromErrorCall(*ev, errors.New(f.message)), nil
}

// fixture — собранная кампания и сессия говорящего.
type fixture struct {
	campaignID string
	session    *ttrpg.Session
}

// Пользователи фикстуры: DM и двое игроков с персонажами. Имена совпадают с
// speaker в кейсах, чтобы авторизация тулов и память были настоящими.
var (
	fixtureDM   = fixtureUser{ID: "eval-dm", Username: "dm_master", Name: "Мастер"}
	fixtureDen  = fixtureUser{ID: "eval-den", Username: "den", Name: "Дэн"}
	fixtureLiza = fixtureUser{ID: "eval-lisa", Username: "lisa", Name: "Лиса"}
)

type fixtureUser struct {
	ID       string
	Username string
	Name     string
}

func buildFixture(st *store.Store, caseName string, meta evalgame.CaseMeta) (fixture, error) {
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Eval " + caseName, Length: store.LengthShort,
		Setting: "приграничье с гоблинами", Theme: "оборона деревни",
	}, store.NewOwnerInput{UserID: fixtureDM.ID, Name: &fixtureDM.Name, Username: &fixtureDM.Username})
	if err != nil {
		return fixture{}, fmt.Errorf("evalrun: кампания: %w", err)
	}
	if _, err := st.BindAndActivate(campaign.ID, fixtureDM.ID, store.BoundChat{ChatID: EvalChatID}); err != nil {
		return fixture{}, fmt.Errorf("evalrun: привязка: %w", err)
	}
	for _, player := range []fixtureUser{fixtureDen, fixtureLiza} {
		name, username := player.Name, player.Username
		if _, err := st.AddMember(campaign.ID, fixtureDM.ID, store.NewMemberInput{
			UserID: player.ID, Name: &name, Username: &username,
		}); err != nil {
			return fixture{}, fmt.Errorf("evalrun: участник %s: %w", player.Name, err)
		}
	}
	hp, maxHP := 12, 12
	stats := map[string]float64{
		"strength": 14, "dexterity": 12, "constitution": 12,
		"intelligence": 10, "wisdom": 12, "charisma": 10,
	}
	for _, player := range []fixtureUser{fixtureDen, fixtureLiza} {
		class := "воин"
		if player.ID == fixtureLiza.ID {
			class = "плут"
		}
		if _, err := st.SaveCharacter(campaign.ID, player.ID, store.NewCharacterInput{
			Name: player.Name, CharacterClass: class, Race: "человек",
			HP: &hp, MaxHP: &maxHP, Stats: stats,
			Equipment: []string{"кинжал", "верёвка"},
		}); err != nil {
			return fixture{}, fmt.Errorf("evalrun: персонаж %s: %w", player.Name, err)
		}
	}

	speaker := fixtureSpeaker(meta)
	session := &ttrpg.Session{
		CampaignID: campaign.ID, CampaignSlug: campaign.Slug,
		UserID: speaker.ID, Username: speaker.Username, Name: speaker.Name,
		ChatID: EvalChatID, ChatType: "supergroup",
	}
	if needsCombat(meta.Category, caseName) {
		if err := seedCombat(st, campaign.Slug, combatCurrent(caseName)); err != nil {
			return fixture{}, err
		}
		// Бой виден модели только через транскрипт: в настоящей сессии
		// инициатива и начало боя уже записаны прошлыми ходами.
		if err := seedCombatTranscript(st, campaign.Slug); err != nil {
			return fixture{}, err
		}
	}
	return fixture{campaignID: campaign.ID, session: session}, nil
}

// fixtureSpeaker сопоставляет speaker кейса с пользователем фикстуры. Роль dm
// отправляет ход от владельца кампании: так проверяются административные
// права, а не только «игрок может писать память».
func fixtureSpeaker(meta evalgame.CaseMeta) fixtureUser {
	if meta.Role == "dm" {
		return fixtureDM
	}
	switch strings.ToLower(strings.TrimSpace(meta.Speaker)) {
	case "лиса":
		return fixtureLiza
	default:
		return fixtureDen
	}
}

// needsCombat — нужен ли кейсу уже идущий бой. Глобальный бой нельзя
// ставить всем: он попадает в блок памяти, и модель начинает каждую сцену с
// гоблинами (таверна, проверки, закрытие дня — всё превращается в бой).
// Бой нужен только кейсам, чей вход продолжает уже начавшийся бой.
func needsCombat(category, caseName string) bool {
	switch caseName {
	case "combat-02-target-by-id", "combat-03-one-action-per-turn",
		"combat-04-dodge-disadvantage", "combat-05-damage-and-persist",
		"combat-06-end-of-fight", "memory-01-damage-on-player-turn",
		"rolls-02-enemy-attack-notation", "rolls-03-damage-attack-combat":
		return true
	}
	return category == "combat" && caseName != "combat-01-initiative-all-participants"
}

// combatCurrent — чей ход в фикстуре. rolls-02 проверяет атаку врага, и
// вход прямо говорит «начинается его ход»; если в снимке ход Дэна, модель
// справедливо откладывает атаку гоблина. Остальным кейсам нужен ход игрока.
func combatCurrent(caseName string) int {
	if caseName == "rolls-02-enemy-attack-notation" {
		return 2 // гоблин-1
	}
	return 0
}

// seedCombatTranscript пишет в день 1 начало боя так, как его записал бы
// прошлый ход: без этой строки модель не знает о бое, и кейс memory-01
// («меня снова ранили») превращается в проверку догадки, а не записи памяти.
func seedCombatTranscript(st *store.Store, campaignSlug string) error {
	entries := []string{
		"DM: Из-за деревьев выскакивают два гоблина с ржавыми кинжалами.",
		"Инициатива: Дэн — 17, Лиса — 15, гоблин-1 — 13, гоблин-2 — 11. Бой начат, раунд 1. Первый ход: Дэн.",
	}
	for _, line := range entries {
		if err := st.AppendTranscriptEntry(campaignSlug, 1, store.TranscriptEntry{Kind: "dm", Text: line}); err != nil {
			return fmt.Errorf("evalrun: транскрипт боя: %w", err)
		}
	}
	return nil
}

// seedCombat ставит кампанию в начало боя: кейсы вроде memory-01 («меня
// снова ранили») идут на ходу обычного игрока внутри боя, и combat damage
// должен найти настоящий снимок, а не отвечать «бой не начат». Блок памяти
// бой не показывает, поэтому на сцены вне боя это не влияет.
func seedCombat(st *store.Store, campaignSlug string, current int) error {
	hp, maxHP := 12, 12
	goblinHP, goblinAC := 7, 13
	combat := engine.CombatOrder{
		Started: true, Round: 1, Current: current,
		Order: []engine.CombatantEntry{
			{ID: "dan", Name: "Дэн", Side: engine.SideParty, Bonus: 2, Roll: 15, Total: 17, HP: &hp, MaxHP: &maxHP},
			{ID: "lisa", Name: "Лиса", Side: engine.SideParty, Bonus: 2, Roll: 13, Total: 15, HP: &hp, MaxHP: &maxHP},
			{ID: "goblin-1", Name: "Гоблин", Side: engine.SideEnemy, Bonus: 2, Roll: 11, Total: 13, HP: &goblinHP, AC: &goblinAC},
			{ID: "goblin-2", Name: "Гоблин", Side: engine.SideEnemy, Bonus: 2, Roll: 9, Total: 11, HP: &goblinHP, AC: &goblinAC},
		},
	}
	return st.SaveCombatState(campaignSlug, store.CombatSnapshot{
		Combat: combat,
		Enemies: []store.SerializedEnemy{
			{ID: "goblin-1", Name: "Гоблин", HP: goblinHP, AC: goblinAC},
			{ID: "goblin-2", Name: "Гоблин", HP: goblinHP, AC: goblinAC},
		},
	})
}

func checkPostconditions(st *store.Store, campaignID string, items []evalgame.Postcondition) []evalgame.PostconditionResult {
	if len(items) == 0 {
		return nil
	}
	campaign, err := st.GetCampaign(campaignID)
	out := make([]evalgame.PostconditionResult, 0, len(items))
	for _, item := range items {
		result := evalgame.PostconditionResult{Postcondition: item, Details: map[string]any{}}
		if err != nil || campaign == nil {
			result.Details["error"] = "кампания фикстуры не найдена"
			out = append(out, result)
			continue
		}
		switch item.Type {
		case "day_summary":
			closed := 0
			if campaign.CurrentDay != nil {
				closed = *campaign.CurrentDay - 1
			}
			result.Details["closed_day"] = closed
			if closed < 1 {
				result.Details["reason"] = "день не закрыт: ход не вызвал advance_day"
				out = append(out, result)
				continue
			}
			record, readErr := st.ReadDay(campaign.Slug, closed)
			if readErr != nil {
				result.Details["error"] = readErr.Error()
				out = append(out, result)
				continue
			}
			if record == nil {
				result.Details["reason"] = "нет записи закрытого дня"
				out = append(out, result)
				continue
			}
			result.Passed = true
			for _, field := range item.Fields {
				var value *string
				switch field {
				case "summary":
					value = record.Summary
				case "headline":
					value = record.Headline
				}
				if value == nil || strings.TrimSpace(*value) == "" {
					result.Passed = false
					result.Details[field] = "пусто"
				} else {
					result.Details[field] = truncate(*value, 200)
				}
			}
		default:
			result.Details["reason"] = "неизвестный тип postcondition " + item.Type
		}
		out = append(out, result)
	}
	return out
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}
