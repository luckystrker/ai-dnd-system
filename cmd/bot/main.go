// Команда bot — точка входа Telegram-бота (миграция с eve на ago).
//
// Telegram-канал появится в слайсе 6; пока для ручной проверки модель можно
// погонять локальным REPL-ом. Ход идёт через dm.Ask: терминальные сбои
// (ErrMaxToolRounds, дедлайн, ошибка модели) дают игроку нейтральный ответ, а
// диагностика уходит в лог.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luckystriker/ai-dnd-system/internal/chronicler"
	"github.com/luckystriker/ai-dnd-system/internal/config"
	"github.com/luckystriker/ai-dnd-system/internal/dayclose"
	"github.com/luckystriker/ai-dnd-system/internal/dm"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("bot", flag.ContinueOnError)
	check := fs.Bool("check", false, "проверить конфигурацию и выйти, не обращаясь к модели")
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

	fmt.Fprintf(os.Stderr, "бот: провайдер %s (%s), модель %s, лимит раундов тулов %d, окно %d, база %s\n",
		spec.Provider, spec.BaseURL, spec.Model, cfg.MaxToolRounds, cfg.ContextWindowTokens, cfg.DBPath)
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
	queue.Start(context.Background())
	defer queue.Close()

	bot, err := dm.New(dm.Config{
		Model:               modelCfg,
		Store:               st,
		DayCloser:           queue,
		MaxToolRounds:       cfg.MaxToolRounds,
		ContextWindowTokens: cfg.ContextWindowTokens,
		TurnTimeout:         time.Duration(cfg.TurnTimeoutSeconds) * time.Second,
		TokenBudget:         cfg.TokenBudget,
		SkillsDir:           skillsDir,
	})
	if err != nil {
		return err
	}
	defer func() { _ = bot.Close() }()

	return repl(bot)
}

// repl — минимальный локальный чат до появления Telegram-канала (слайс 6).
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
