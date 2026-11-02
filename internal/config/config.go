// Package config собирает конфигурацию бота из переменных окружения.
package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/luckystriker/ai-dnd-system/internal/llm"
)

const (
	// DefaultMaxToolRounds — явный лимит цикла «модель → тулы» в одном ходу.
	// Без него ago крутит тулы до тех пор, пока модель сама не остановится.
	DefaultMaxToolRounds = 12

	// DefaultDBPath — та же база кампаний, что у TS-версии; Go-бот открывает
	// её без миграции данных.
	DefaultDBPath = "data/campaigns.db"
)

// Config — вся конфигурация процесса бота.
type Config struct {
	LLM           llm.Settings
	MaxToolRounds int
	DBPath        string
}

// Load читает переменные окружения через getenv (подменяется в тестах).
//
// Переменные:
//
//	LLM_PROVIDER     openrouter | opencode-go | deepseek (по умолчанию openrouter)
//	LLM_API_KEY      ключ провайдера; если пуст — берётся из OPENROUTER_API_KEY,
//	                 OPENCODE_API_KEY или DEEPSEEK_API_KEY
//	LLM_BASE_URL     переопределить корень эндпоинта провайдера
//	LLM_MODEL        переопределить id модели
//	MAX_TOOL_ROUNDS  лимит раундов тулов в одном ходу (по умолчанию 12)
//	CAMPAIGN_DB_PATH путь к SQLite-базе кампаний (по умолчанию data/campaigns.db)
func Load(getenv func(string) string) (Config, error) {
	spec, err := llm.Lookup(getenv("LLM_PROVIDER"))
	if err != nil {
		return Config{}, err
	}

	apiKey := llm.APIKeyFromEnv(spec, getenv)
	if apiKey == "" {
		return Config{}, fmt.Errorf("не задан ключ LLM: заполните LLM_API_KEY или %s", spec.APIKeyEnv)
	}

	maxRounds := DefaultMaxToolRounds
	if raw := strings.TrimSpace(getenv("MAX_TOOL_ROUNDS")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("MAX_TOOL_ROUNDS: ожидается положительное целое, получено %q", raw)
		}
		maxRounds = parsed
	}

	dbPath := strings.TrimSpace(getenv("CAMPAIGN_DB_PATH"))
	if dbPath == "" {
		dbPath = DefaultDBPath
	}

	return Config{
		LLM: llm.Settings{
			Provider: spec.Provider,
			APIKey:   apiKey,
			BaseURL:  strings.TrimSpace(getenv("LLM_BASE_URL")),
			Model:    strings.TrimSpace(getenv("LLM_MODEL")),
		},
		MaxToolRounds: maxRounds,
		DBPath:        dbPath,
	}, nil
}
