// Команда bot — точка входа Telegram-бота (миграция с eve на ago).
//
// Режимы:
//
//	telegram — long-poll Bot API (по умолчанию, если задан TELEGRAM_BOT_TOKEN);
//	repl     — локальный чат для отладки без сети;
//	auto     — telegram при заданном токене, иначе repl.
//
// Ход идёт через dm.Ask/AskInputs: терминальные сбои (ErrMaxToolRounds,
// дедлайн, ошибка модели) дают игроку нейтральный ответ, а диагностика уходит
// в лог. По SIGINT/SIGTERM канал перестаёт опрашивать Telegram, дожидается
// идущих ходов (с таймаутом), закрывает очередь закрытия дня и базу.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	agoconfig "github.com/luckystrker/ago/config"
	"github.com/luckystrker/ago/stream"

	"github.com/luckystriker/ai-dnd-system/internal/chronicler"
	"github.com/luckystriker/ai-dnd-system/internal/config"
	"github.com/luckystriker/ai-dnd-system/internal/dayclose"
	"github.com/luckystriker/ai-dnd-system/internal/dm"
	"github.com/luckystriker/ai-dnd-system/internal/store"
	"github.com/luckystriker/ai-dnd-system/internal/telegram"
	ttrpg "github.com/luckystriker/ai-dnd-system/internal/tools"
)

const (
	modeAuto     = "auto"
	modeTelegram = "telegram"
	modeRepl     = "repl"

	// shutdownTimeout — сколько ждём идущие ходы при остановке.
	shutdownTimeout = 15 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("bot", flag.ContinueOnError)
	check := fs.Bool("check", false, "проверить конфигурацию и выйти, не обращаясь к модели")
	mode := fs.String("mode", modeAuto, "режим: auto | telegram | repl")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	modelCfg, spec, err := cfg.LLM.ModelConfig()
	if err != nil {
		return err
	}
	if cfg.DBPath == "" {
		return fmt.Errorf("CAMPAIGN_DB_PATH не должен быть пустым")
	}
	selected, err := resolveMode(*mode, cfg.Telegram.Token)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "бот: режим %s, провайдер %s (%s), модель %s, лимит раундов тулов %d, окно %d, база %s\n",
		selected, spec.Provider, spec.BaseURL, spec.Model, cfg.MaxToolRounds, cfg.ContextWindowTokens, cfg.DBPath)
	if *check {
		return nil
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	// Скиллы лежат рядом с БД и перезаписываются при старте: временный
	// каталог может вычистить система, и load_skill начнёт падать.
	skillsDir := filepath.Join(filepath.Dir(cfg.DBPath), ".skills")
	if err := dm.MaterializeSkills(skillsDir); err != nil {
		return err
	}

	// Закрытие дня ставится в очередь из advance_day. После рестарта очередь
	// дочитывает дни без саммари (ScanGaps), а сбой летописца закрывается
	// fallback-дайджестом.
	queue, err := dayclose.New(dayclose.Config{
		Store:        st,
		BackfillMax:  cfg.DayCloseBackfillMax,
		BackfillDays: cfg.DayCloseBackfillDays,
		NewArchivist: func(campaign *store.Campaign) (dayclose.Archivist, error) {
			return chronicler.New(chronicler.Config{
				Model:         modelCfg,
				Store:         st,
				CampaignID:    campaign.ID,
				MaxToolRounds: cfg.MaxToolRounds,
			})
		},
	})
	if err != nil {
		return err
	}
	queue.Start(ctx)
	defer queue.Close()

	if selected == modeTelegram {
		return runTelegram(ctx, cfg, modelCfg, st, queue, skillsDir)
	}
	bot, err := newDM(cfg, modelCfg, st, queue, skillsDir, nil, nil, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = bot.Close() }()
	return repl(bot)
}

// resolveMode выбирает режим: auto — telegram при заданном токене.
func resolveMode(mode, token string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", modeAuto:
		if strings.TrimSpace(token) != "" {
			return modeTelegram, nil
		}
		return modeRepl, nil
	case modeTelegram:
		if strings.TrimSpace(token) == "" {
			return "", errors.New("режим telegram требует TELEGRAM_BOT_TOKEN")
		}
		return modeTelegram, nil
	case modeRepl:
		return modeRepl, nil
	default:
		return "", fmt.Errorf("неизвестный режим %q, доступны: auto, telegram, repl", mode)
	}
}

// newDM собирает DM-агента. session nil — фиксированная сессия; иначе сессия
// задаётся на каждый ход (Telegram-чат), а conversation — durable-поток.
func newDM(
	cfg config.Config,
	modelCfg agoconfig.ModelConfig,
	st *store.Store,
	queue *dayclose.Queue,
	skillsDir string,
	conversation stream.Stream,
	session func() *ttrpg.Session,
	poster ttrpg.ImagePoster,
	activity func(),
) (*dm.DM, error) {
	args := dm.Config{
		Model:               modelCfg,
		Store:               st,
		DayCloser:           queue,
		MaxToolRounds:       cfg.MaxToolRounds,
		ContextWindowTokens: cfg.ContextWindowTokens,
		TurnTimeout:         time.Duration(cfg.TurnTimeoutSeconds) * time.Second,
		TokenBudget:         cfg.TokenBudget,
		SkillsDir:           skillsDir,
		Stream:              conversation,
		SessionFunc:         session,
		ToolDeps:            ttrpg.Deps{Poster: poster},
		OnToolActivity:      activity,
	}
	if session == nil {
		args.Session = &ttrpg.Session{UserID: "local", ChatID: "local", ChatType: "private"}
	}
	return dm.New(args)
}

// runTelegram поднимает канал и ждёт остановки.
func runTelegram(
	ctx context.Context,
	cfg config.Config,
	modelCfg agoconfig.ModelConfig,
	st *store.Store,
	queue *dayclose.Queue,
	skillsDir string,
) error {
	options := []telegram.Option{
		telegram.WithRetryBackoff(telegram.DefaultRetryBackoff),
		telegram.WithMaxRetries(telegram.DefaultMaxRetries),
	}
	if cfg.Telegram.APIBaseURL != "" {
		options = append(options, telegram.WithBaseURL(cfg.Telegram.APIBaseURL))
	}
	api := telegram.NewClient(cfg.Telegram.Token, options...)
	sender := telegram.NewSender(api)
	typing := telegram.NewTypingNotifier(api)

	factory := func(_ string, conversation stream.Stream, session func() *ttrpg.Session) (*dm.DM, error) {
		agent, err := newDM(cfg, modelCfg, st, queue, skillsDir, conversation, session, sender, typing.Activity(session))
		if err != nil {
			return nil, err
		}
		return agent, nil
	}
	channel, err := telegram.NewChannel(telegram.Config{
		API:                api,
		Store:              st,
		BotUsername:        cfg.Telegram.BotUsername,
		CoalesceWindow:     time.Duration(cfg.Telegram.CoalesceMS) * time.Millisecond,
		Limits:             telegram.Limits{ChatPerMinute: cfg.Telegram.ChatPerMinute, UserPerMinute: cfg.Telegram.UserPerMinute, Window: time.Minute},
		MaxUpdateBytes:     cfg.Telegram.MaxUpdateBytes,
		MaxAttachmentBytes: cfg.Telegram.MaxAttachmentBytes,
		PollTimeout:        time.Duration(cfg.Telegram.PollTimeoutSeconds) * time.Second,
		FollowUpWindow:     time.Duration(cfg.Telegram.FollowUpMinutes) * time.Minute,
		NewDM:              factory,
	})
	if err != nil {
		return err
	}
	runErr := channel.Run(ctx)

	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := channel.Stop(stopCtx); err != nil {
		fmt.Fprintf(os.Stderr, "бот: остановка канала: %v\n", err)
	}
	return runErr
}

// repl — минимальный локальный чат без сети (отладка модели и тулов).
func repl(bot *dm.DM) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	fmt.Fprintln(os.Stderr, "локальный REPL: /exit — выход")
	for {
		fmt.Fprint(os.Stdout, "> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("repl: %w", err)
			}
			return nil
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if strings.EqualFold(text, "/exit") {
			return nil
		}
		reply, err := bot.Ask(context.Background(), text)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, reply)
	}
}
