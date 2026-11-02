// Команда bot — точка входа Telegram-бота (миграция с eve на ago).
//
// В слайсе 0 это каркас: конфиг из env, выбор LLM-провайдера и сборка
// DM-агента. Telegram-канал появится в следующих слайсах; пока для ручной
// проверки модель можно погонять локальным REPL-ом.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/luckystrker/ago/tui"

	"github.com/luckystriker/ai-dnd-system/internal/config"
	"github.com/luckystriker/ai-dnd-system/internal/dm"
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

	// Путь к базе проверяем уже сейчас: слайсы хранилища и тулов откроют его.
	if cfg.DBPath == "" {
		return fmt.Errorf("CAMPAIGN_DB_PATH не должен быть пустым")
	}

	fmt.Fprintf(os.Stderr, "бот: провайдер %s (%s), модель %s, лимит раундов тулов %d, база %s\n",
		spec.Provider, spec.BaseURL, spec.Model, cfg.MaxToolRounds, cfg.DBPath)
	if *check {
		return nil
	}

	a := dm.New(modelCfg, dm.Options{MaxToolRounds: cfg.MaxToolRounds})
	return tui.REPL(a, os.Stdin, os.Stdout)
}
