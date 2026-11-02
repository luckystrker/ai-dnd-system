// Package dm собирает DM-агента: системный промпт, память кампании в хвосте
// контекста, транскрипт, фиксированное окно и лимиты хода.
package dm

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/luckystrker/ago/agent"
	"github.com/luckystrker/ago/compact"
	agoconfig "github.com/luckystrker/ago/config"
	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/middleware"
	"github.com/luckystrker/ago/observers"
	"github.com/luckystrker/ago/stream"
	"github.com/luckystrker/ago/toolpacks"
	agotools "github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/store"
	"github.com/luckystriker/ai-dnd-system/internal/tools"
)

//go:embed instructions.md
var instructions string

//go:embed skills
var skillsFS embed.FS

const (
	// DefaultContextWindowTokens — фиксированное окно контекста: 96k из 128k
	// окна модели (см. config.DefaultContextWindowTokens).
	DefaultContextWindowTokens = 96000
	// DefaultTurnTimeout — дедлайн одного хода DM.
	DefaultTurnTimeout = 180 * time.Second
	// DefaultTokenBudget — бюджет токенов одной сессии (чата).
	DefaultTokenBudget int64 = 200000
	// DefaultMaxToolRounds — лимит раундов тулов, если конфиг не задал свой
	// (решение владельца: 12).
	DefaultMaxToolRounds = 12
	// NeutralFailureReply — ответ игрокам при терминальном сбое хода: без
	// диагностики и кодов. Записанное тулами при этом не откатывается.
	NeutralFailureReply = "DM задумался, повтори ход."
)

// Config — настройки DM-агента. Store обязателен для памяти и транскрипта;
// пустая сессия допустима для локального REPL без кампании.
type Config struct {
	Model   agoconfig.ModelConfig
	Store   *store.Store
	Session *tools.Session
	// ToolDeps разделяется с летописцем (Random, Dice, CombatLocks).
	ToolDeps tools.Deps
	// DayCloser — очередь закрытия дня. nil — advance_day только сдвигает день.
	DayCloser           tools.DayCloser
	MaxToolRounds       int
	ContextWindowTokens int
	TurnTimeout         time.Duration
	TokenBudget         int64
	// SkillsDir переопределяет каталог скиллов (тесты). Пусто — встроенные
	// skills/ материализуются во временный каталог.
	SkillsDir string
	// ExtraTools добавляются к DM toolkit (eval и узкие тесты).
	ExtraTools []agotools.Tool
	// SkipToolkit собирает агента только из ExtraTools: eval на скриптованных
	// траекториях проверяет промпт, память и лимиты без живой БД-кампании.
	SkipToolkit bool
	Logger      *slog.Logger
}

// DM — собранный агент одного чата: свой stream, свой token monitor.
type DM struct {
	agent   *agent.Agent
	monitor *observers.TokenMonitor
	timeout time.Duration
	log     *slog.Logger
	cleanup func()
	session *tools.Session
	stream  stream.Stream
}

// New собирает DM-агента.
func New(cfg Config) (*DM, error) {
	if cfg.Model == nil {
		return nil, errors.New("dm: model config is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	window := cfg.ContextWindowTokens
	if window <= 0 {
		window = DefaultContextWindowTokens
	}
	timeout := cfg.TurnTimeout
	if timeout <= 0 {
		timeout = DefaultTurnTimeout
	}
	budget := cfg.TokenBudget
	if budget <= 0 {
		budget = DefaultTokenBudget
	}
	maxToolRounds := cfg.MaxToolRounds
	if maxToolRounds <= 0 {
		maxToolRounds = DefaultMaxToolRounds
	}
	session := cfg.Session
	if session == nil {
		session = &tools.Session{}
	}

	agentTools := make([]agotools.Tool, 0, 2+len(cfg.ExtraTools))
	var cleanup func()
	if !cfg.SkipToolkit {
		deps := cfg.ToolDeps
		deps.DB = cfg.Store
		deps.DayCloser = cfg.DayCloser
		dmTools, err := tools.DMToolkit(deps)
		if err != nil {
			return nil, err
		}
		skillsDir, skillsCleanup, err := resolveSkills(cfg.SkillsDir)
		if err != nil {
			return nil, err
		}
		cleanup = skillsCleanup
		skills, err := toolpacks.NewSkillsToolkit(skillsDir)
		if err != nil {
			if cleanup != nil {
				cleanup()
			}
			return nil, fmt.Errorf("dm: skills: %w", err)
		}
		agentTools = append(agentTools, dmTools, skills)
	}
	agentTools = append(agentTools, cfg.ExtraTools...)

	client, err := cfg.Model.Create()
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("dm: model client: %w", err)
	}

	conversation := stream.NewMemoryStream()
	cx := agocontext.New(conversation)
	monitor := observers.NewTokenMonitor(budget)
	if err := monitor.Register(cx); err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, err
	}

	memory := newMemoryPolicy(cfg.Store, window, budget, monitor.Total, logger)

	agentOpts := []agent.Option{
		agent.WithConfig(cfg.Model),
		agent.WithTools(agentTools...),
		agent.WithDependencies(map[string]any{sessionDepKey: session}),
		agent.WithStreamBase(conversation),
		agent.WithMiddleware(
			// Компакция — самая внешняя: её OnTurn выполняется после хода и
			// после записи ответа в транскрипт. Ошибку компакции глотаем,
			// чтобы готовый ответ не превратился в нейтральный.
			safeCompactionFactory(compactionFactory(client, window), logger),
			func() middleware.Middleware {
				return &transcriptMW{store: cfg.Store, log: logger}
			},
			func() middleware.Middleware { return memory },
		),
	}
	agentOpts = append(agentOpts, agent.WithMaxToolRounds(maxToolRounds))
	a := agent.New("dm", instructions, agentOpts...)
	return &DM{
		agent: a, monitor: monitor, timeout: timeout, log: logger, cleanup: cleanup,
		session: session, stream: conversation,
	}, nil
}

// compactionFactory включает компакцию истории по токенному триггеру: 75%
// фиксированного окна. Окно не растёт с длиной кампании.
func compactionFactory(client agoconfig.LLMClient, window int) middleware.Factory {
	return compact.New(
		contentTrigger{budget: window * 3 / 4},
		compact.LLMStrategy{Client: client, Timeout: 120 * time.Second},
		compact.WithAgent("dm"),
	)
}

// safeCompactionFactory оборачивает компакцию: ход уже завершён и ответ
// записан, поэтому сбой Replace/Send при компакции логируется, но не
// превращает готовый ответ в нейтральный.
func safeCompactionFactory(inner middleware.Factory, log *slog.Logger) middleware.Factory {
	return func() middleware.Middleware {
		return &safeCompaction{inner: inner(), log: log}
	}
}

type safeCompaction struct {
	middleware.Base
	inner middleware.Middleware
	log   *slog.Logger
}

func (s *safeCompaction) OnTurn(next middleware.TurnFunc, ev *events.ModelRequest, cx *agocontext.Context) (*events.ModelResponse, error) {
	var (
		result  *events.ModelResponse
		turnErr error
	)
	compacted, err := s.inner.OnTurn(func(e *events.ModelRequest, c *agocontext.Context) (*events.ModelResponse, error) {
		result, turnErr = next(e, c)
		return result, turnErr
	}, ev, cx)
	if turnErr != nil {
		return result, turnErr
	}
	if err != nil {
		s.log.Error("dm: compaction failed; keeping the turn result", "error", err)
		return result, nil
	}
	return compacted, nil
}

// Agent возвращает нижележащего агента (TUI, eval).
func (d *DM) Agent() *agent.Agent { return d.agent }

// Monitor возвращает монитор токенов сессии.
func (d *DM) Monitor() *observers.TokenMonitor { return d.monitor }

// Ask выполняет один ход. Терминальные сбои (ErrMaxToolRounds, дедлайн,
// ошибка модели) превращаются в нейтральный ответ игрокам; диагностика
// уходит в лог, результаты тулов и записи в БД остаются как есть.
func (d *DM) Ask(ctx context.Context, text string) (string, error) {
	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	reply, err := d.agent.Ask(runCtx, text)
	if err != nil {
		switch {
		case errors.Is(err, agent.ErrMaxToolRounds):
			d.log.Warn("dm: max tool rounds exceeded", d.failureFields(err, started)...)
		case errors.Is(err, context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.DeadlineExceeded):
			d.log.Warn("dm: turn deadline exceeded", d.failureFields(err, started)...)
		case errors.Is(err, context.Canceled):
			// Отмена — не сбой DM: отдаём её наверх, чтобы вызывающий код не
			// отправил игрокам устаревший ответ.
			d.log.Info("dm: turn cancelled", d.failureFields(err, started)...)
			return "", err
		default:
			d.log.Error("dm: turn failed", d.failureFields(err, started)...)
		}
		return NeutralFailureReply, nil
	}
	if reply == nil {
		d.log.Error("dm: empty reply", d.failureFields(errors.New("empty reply"), started)...)
		return NeutralFailureReply, nil
	}
	return reply.Body(), nil
}

// failureFields — трасса терминального сбоя для лога: чат, длительность,
// число событий хода и последние вызванные тулы. Игрокам это не показывается.
func (d *DM) failureFields(err error, started time.Time) []any {
	fields := []any{"error", err, "duration", time.Since(started)}
	if d.session != nil && d.session.ChatID != "" {
		fields = append(fields, "chat", d.session.ChatID)
	}
	if d.stream == nil {
		return fields
	}
	evs, readErr := d.stream.History().GetEvents()
	if readErr != nil {
		return append(fields, "history_error", readErr)
	}
	fields = append(fields, "events", len(evs), "tool_rounds", toolRounds(evs))
	if names := recentToolNames(evs, 5); len(names) > 0 {
		fields = append(fields, "tools", names)
	}
	return fields
}

func toolRounds(evs []events.Event) int {
	rounds := 0
	for _, ev := range evs {
		switch item := events.AsValue(ev).(type) {
		case events.ToolCallsEvent:
			if len(item.Calls) > 0 {
				rounds++
			}
		}
	}
	return rounds
}

func recentToolNames(evs []events.Event, limit int) []string {
	names := []string{}
	for _, ev := range evs {
		if item, ok := events.AsValue(ev).(events.ToolCallsEvent); ok {
			for _, call := range item.Calls {
				names = append(names, call.Name)
			}
		}
	}
	if len(names) == 0 {
		for _, ev := range evs {
			if item, ok := events.AsValue(ev).(events.ToolCallEvent); ok {
				names = append(names, item.Name)
			}
		}
	}
	if limit > 0 && len(names) > limit {
		names = names[len(names)-limit:]
	}
	return names
}

// Close освобождает временный каталог скиллов.
func (d *DM) Close() error {
	if d.cleanup != nil {
		d.cleanup()
		d.cleanup = nil
	}
	return nil
}

// MaterializeSkills записывает встроенные скиллы в dir, перезаписывая файлы.
// Боевой запуск кладёт их в постоянный каталог рядом с БД: временный каталог
// может быть вычищен системой, и load_skill начнёт падать без перезапуска.
func MaterializeSkills(dir string) error {
	if dir == "" {
		return errors.New("dm: skills dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("dm: skills dir: %w", err)
	}
	return fs.WalkDir(skillsFS, "skills", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel("skills", path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dir, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := skillsFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// resolveSkills возвращает каталог скиллов и функцию очистки. Явный каталог
// (боевой режим) наполняется встроенными файлами и не чистится; без него
// используется временный каталог на процесс (тесты, REPL).
func resolveSkills(dir string) (string, func(), error) {
	if dir != "" {
		if err := MaterializeSkills(dir); err != nil {
			return "", nil, err
		}
		return dir, nil, nil
	}
	root, err := os.MkdirTemp("", "ttrpg-skills-*")
	if err != nil {
		return "", nil, fmt.Errorf("dm: skills temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	if err := MaterializeSkills(root); err != nil {
		cleanup()
		return "", nil, err
	}
	return root, cleanup, nil
}
