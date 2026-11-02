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

	// DefaultContextWindowTokens — фиксированное окно контекста: 96k из 128k
	// окна модели. Запас в 32k оставлен на ответ, схемы тулов и служебные
	// сообщения; компакция срабатывает на 75% окна (72k). Окно не растёт с
	// длиной кампании — память кампании живёт в SQLite, а не в истории.
	DefaultContextWindowTokens = 96000

	// DefaultTurnTimeoutSeconds — дедлайн одного хода DM. Ход с летописцем
	// внутри (закрытие дня) идёт в фоне и в дедлайн не входит.
	DefaultTurnTimeoutSeconds = 180

	// DefaultTokenBudget — бюджет токенов одного хода: TokenMonitor
	// сбрасывается в начале хода, при превышении модель получает просьбу
	// завершить сцену, игроки — нет.
	DefaultTokenBudget int64 = 200000

	// DefaultDayCloseBackfillMax — сколько пропущенных дней на старте максимум
	// закрывать полным летописцем (дорого и может затирать текущий мир).
	DefaultDayCloseBackfillMax = 2

	// DefaultDayCloseBackfillDays — окно последних закрытых дней, которые
	// закрывает летописец; более старые дни — только fallback-дайджест.
	DefaultDayCloseBackfillDays = 2
)

// Config — вся конфигурация процесса бота.
type Config struct {
	LLM                  llm.Settings
	MaxToolRounds        int
	DBPath               string
	ContextWindowTokens  int
	TurnTimeoutSeconds   int
	TokenBudget          int64
	DayCloseBackfillMax  int
	DayCloseBackfillDays int
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
//	MAX_TOOL_ROUNDS      лимит раундов тулов в одном ходу (по умолчанию 12)
//	CONTEXT_WINDOW_TOKENS фиксированное окно контекста, 16000..512000
//	                     (по умолчанию 96000); компакция на 75%
//	TURN_TIMEOUT_SECONDS дедлайн одного хода DM, 1..3600 (по умолчанию 180)
//	TOKEN_BUDGET         бюджет токенов одного хода (по умолчанию 200000)
//	DAYCLOSE_BACKFILL_MAX сколько пропущенных дней на старте закрывать полным
//	                     летописцем: -1 — не догонять, 0 — по умолчанию 2
//	DAYCLOSE_BACKFILL_DAYS окно последних закрытых дней для летописца
//	                     (1..30, по умолчанию 2); старше — только дайджест
//	CAMPAIGN_DB_PATH     путь к SQLite-базе кампаний (по умолчанию data/campaigns.db)
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

	contextWindow := DefaultContextWindowTokens
	if raw := strings.TrimSpace(getenv("CONTEXT_WINDOW_TOKENS")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 16000 || parsed > 512000 {
			return Config{}, fmt.Errorf("CONTEXT_WINDOW_TOKENS: ожидается целое 16000..512000, получено %q", raw)
		}
		contextWindow = parsed
	}

	turnTimeout := DefaultTurnTimeoutSeconds
	if raw := strings.TrimSpace(getenv("TURN_TIMEOUT_SECONDS")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed <= 0 || parsed > 3600 {
			return Config{}, fmt.Errorf("TURN_TIMEOUT_SECONDS: ожидается целое 1..3600, получено %q", raw)
		}
		turnTimeout = parsed
	}

	tokenBudget := DefaultTokenBudget
	if raw := strings.TrimSpace(getenv("TOKEN_BUDGET")); raw != "" {
		parsed, convErr := strconv.ParseInt(raw, 10, 64)
		if convErr != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("TOKEN_BUDGET: ожидается положительное целое, получено %q", raw)
		}
		tokenBudget = parsed
	}

	backfillMax := DefaultDayCloseBackfillMax
	if raw := strings.TrimSpace(getenv("DAYCLOSE_BACKFILL_MAX")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < -1 || parsed > 1000 {
			return Config{}, fmt.Errorf("DAYCLOSE_BACKFILL_MAX: ожидается -1..1000, получено %q", raw)
		}
		if parsed != 0 {
			backfillMax = parsed
		}
	}
	backfillDays := DefaultDayCloseBackfillDays
	if raw := strings.TrimSpace(getenv("DAYCLOSE_BACKFILL_DAYS")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 1 || parsed > 30 {
			return Config{}, fmt.Errorf("DAYCLOSE_BACKFILL_DAYS: ожидается 1..30, получено %q", raw)
		}
		backfillDays = parsed
	}

	return Config{
		LLM: llm.Settings{
			Provider: spec.Provider,
			APIKey:   apiKey,
			BaseURL:  strings.TrimSpace(getenv("LLM_BASE_URL")),
			Model:    strings.TrimSpace(getenv("LLM_MODEL")),
		},
		MaxToolRounds:        maxRounds,
		DBPath:               dbPath,
		ContextWindowTokens:  contextWindow,
		TurnTimeoutSeconds:   turnTimeout,
		TokenBudget:          tokenBudget,
		DayCloseBackfillMax:  backfillMax,
		DayCloseBackfillDays: backfillDays,
	}, nil
}
