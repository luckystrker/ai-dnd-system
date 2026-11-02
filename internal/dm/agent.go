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
	transcript := &transcriptMW{store: cfg.Store, log: logger}

	agentOpts := []agent.Option{
		agent.WithConfig(cfg.Model),
		agent.WithTools(agentTools...),
		agent.WithDependencies(map[string]any{sessionDepKey: session}),
		agent.WithStreamBase(conversation),
		agent.WithMiddleware(
			func() middleware.Middleware { return transcript },
			func() middleware.Middleware { return memory },
			compactionFactory(client, window),
		),
	}
	if cfg.MaxToolRounds > 0 {
		agentOpts = append(agentOpts, agent.WithMaxToolRounds(cfg.MaxToolRounds))
	}
	a := agent.New("dm", instructions, agentOpts...)
	return &DM{agent: a, monitor: monitor, timeout: timeout, log: logger, cleanup: cleanup}, nil
}

// compactionFactory включает компакцию истории по токенному триггеру: 75%
// фиксированного окна. Окно не растёт с длиной кампании.
func compactionFactory(client agoconfig.LLMClient, window int) middleware.Factory {
	return compact.New(
		compact.TokenTrigger(window*3/4),
		compact.LLMStrategy{Client: client, Timeout: 120 * time.Second},
		compact.WithAgent("dm"),
	)
}

// Agent возвращает нижележащего агента (TUI, eval).
func (d *DM) Agent() *agent.Agent { return d.agent }

// Monitor возвращает монитор токенов сессии.
func (d *DM) Monitor() *observers.TokenMonitor { return d.monitor }

// Ask выполняет один ход. Терминальные сбои (ErrMaxToolRounds, дедлайн,
// ошибка модели) превращаются в нейтральный ответ игрокам; диагностика
// уходит в лог, результаты тулов и записи в БД остаются как есть.
func (d *DM) Ask(ctx context.Context, text string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	reply, err := d.agent.Ask(runCtx, text)
	if err != nil {
		switch {
		case errors.Is(err, agent.ErrMaxToolRounds):
			d.log.Warn("dm: max tool rounds exceeded", "error", err)
		case errors.Is(err, context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.DeadlineExceeded):
			d.log.Warn("dm: turn deadline exceeded", "timeout", d.timeout)
		case errors.Is(err, context.Canceled):
			d.log.Info("dm: turn cancelled")
		default:
			d.log.Error("dm: turn failed", "error", err)
		}
		return NeutralFailureReply, nil
	}
	if reply == nil {
		return NeutralFailureReply, nil
	}
	return reply.Body(), nil
}

// Close освобождает временный каталог скиллов.
func (d *DM) Close() error {
	if d.cleanup != nil {
		d.cleanup()
		d.cleanup = nil
	}
	return nil
}

// resolveSkills возвращает каталог скиллов и функцию очистки. Встроенные
// скиллы материализуются во временный каталог: загрузчик ago читает файловую
// систему, а бинарь должен оставаться самодостаточным.
func resolveSkills(dir string) (string, func(), error) {
	if dir != "" {
		return dir, nil, nil
	}
	root, err := os.MkdirTemp("", "ttrpg-skills-*")
	if err != nil {
		return "", nil, fmt.Errorf("dm: skills temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	err = fs.WalkDir(skillsFS, "skills", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel("skills", path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := skillsFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return root, cleanup, nil
}
