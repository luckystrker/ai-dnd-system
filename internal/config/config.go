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

	// DefaultTelegramCoalesceMS — окно склейки сообщений чата. 2.5 с:
	// типичный интервал между двумя репликами одного игрока — 1–3 с,
	// задержка ответа остаётся в пределах восприятия.
	DefaultTelegramCoalesceMS = 2500

	// DefaultTelegramChatPerMinute / DefaultTelegramUserPerMinute — те же
	// лимиты, что TELEGRAM_RATE_LIMITS в TS.
	DefaultTelegramChatPerMinute = 20
	DefaultTelegramUserPerMinute = 10

	// DefaultTelegramMaxUpdateBytes — защита от аномальных payload.
	DefaultTelegramMaxUpdateBytes = 1024 * 1024

	// DefaultTelegramPollTimeoutSeconds — long-poll getUpdates.
	DefaultTelegramPollTimeoutSeconds = 50

	// DefaultTelegramMaxAttachmentBytes — политика вложений TS: 25 МБ.
	DefaultTelegramMaxAttachmentBytes int64 = 25 * 1024 * 1024

	// DefaultTelegramFollowUpMinutes — сколько после ответа DM сообщение
	// игрока считается ответом на реплику DM (гейт follow-up). Калибровка —
	// слайс 7; значение вынесено в env, чтобы менять без пересборки.
	DefaultTelegramFollowUpMinutes = 10

	// DefaultSessionGapHours — пауза в чате, после которой начинается новая
	// игровая сессия (PLAN 39): 20 часов отделяют вечернюю игру от следующей.
	DefaultSessionGapHours = 20
)

// SessionGapHoursMin / Max — допустимые значения SESSION_GAP_HOURS: от часа
// до 30 дней. Верхняя граница ловит опечатку (лишний ноль), а не смысл.
const (
	SessionGapHoursMin = 1
	SessionGapHoursMax = 720
)

// Telegram — настройки канала (слайс 6). Пустой Token — режим repl.
type Telegram struct {
	Token              string
	BotUsername        string
	APIBaseURL         string
	CoalesceMS         int
	ChatPerMinute      int
	UserPerMinute      int
	MaxUpdateBytes     int
	PollTimeoutSeconds int
	MaxAttachmentBytes int64
	FollowUpMinutes    int
}

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
	// SessionGapHours — пауза в чате, после которой начинается новая сессия
	// (маркер в блоке памяти, recap в промпте).
	SessionGapHours int
	Telegram        Telegram
}

// Load читает переменные окружения через getenv (подменяется в тестах).
//
// Переменные:
//
//	LLM_PROVIDER     openrouter | opencode-go | deepseek (по умолчанию opencode-go)
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
//	SESSION_GAP_HOURS    пауза в чате, после которой начинается новая сессия
//	                     (1..720, по умолчанию 20)
//	CAMPAIGN_DB_PATH     путь к SQLite-базе кампаний (по умолчанию data/campaigns.db)
//	TELEGRAM_BOT_TOKEN   токен бота; без него запускается локальный repl
//	TELEGRAM_BOT_USERNAME username бота без @ (для @упоминаний и команд)
//	TELEGRAM_API_BASE_URL корень Bot API (по умолчанию https://api.telegram.org)
//	TELEGRAM_COALESCE_MS окно склейки сообщений чата, 1..60000 (по умолчанию 2500)
//	TELEGRAM_RATE_LIMIT_CHAT_PER_MIN сообщений чата в минуту (по умолчанию 20)
//	TELEGRAM_RATE_LIMIT_USER_PER_MIN сообщений игрока в минуту (по умолчанию 10)
//	TELEGRAM_MAX_UPDATE_BYTES предел размера апдейта (по умолчанию 1 МБ)
//	TELEGRAM_POLL_TIMEOUT_SECONDS long-poll getUpdates, 1..300 (по умолчанию 50)
//	TELEGRAM_MAX_ATTACHMENT_BYTES предел скачиваемого вложения (25 МБ)
//	TELEGRAM_FOLLOW_UP_MINUTES окно follow-up гейта, 1..1440 (по умолчанию 10)
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

	sessionGapHours := DefaultSessionGapHours
	if raw := strings.TrimSpace(getenv("SESSION_GAP_HOURS")); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < SessionGapHoursMin || parsed > SessionGapHoursMax {
			return Config{}, fmt.Errorf("SESSION_GAP_HOURS: ожидается целое %d..%d, получено %q",
				SessionGapHoursMin, SessionGapHoursMax, raw)
		}
		sessionGapHours = parsed
	}

	telegram, err := loadTelegram(getenv)
	if err != nil {
		return Config{}, err
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
		SessionGapHours:      sessionGapHours,
		Telegram:             telegram,
	}, nil
}

// loadTelegram читает настройки канала; отсутствие токена — не ошибка
// (локальный repl без сети).
func loadTelegram(getenv func(string) string) (Telegram, error) {
	settings := Telegram{
		Token:              strings.TrimSpace(getenv("TELEGRAM_BOT_TOKEN")),
		BotUsername:        strings.TrimLeft(strings.TrimSpace(getenv("TELEGRAM_BOT_USERNAME")), "@"),
		APIBaseURL:         strings.TrimRight(strings.TrimSpace(getenv("TELEGRAM_API_BASE_URL")), "/"),
		CoalesceMS:         DefaultTelegramCoalesceMS,
		ChatPerMinute:      DefaultTelegramChatPerMinute,
		UserPerMinute:      DefaultTelegramUserPerMinute,
		MaxUpdateBytes:     DefaultTelegramMaxUpdateBytes,
		PollTimeoutSeconds: DefaultTelegramPollTimeoutSeconds,
		MaxAttachmentBytes: DefaultTelegramMaxAttachmentBytes,
		FollowUpMinutes:    DefaultTelegramFollowUpMinutes,
	}
	readInt := func(name string, fallback, min, max int) (int, error) {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return fallback, nil
		}
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < min || parsed > max {
			return 0, fmt.Errorf("%s: ожидается целое %d..%d, получено %q", name, min, max, raw)
		}
		return parsed, nil
	}
	var err error
	if settings.CoalesceMS, err = readInt("TELEGRAM_COALESCE_MS", settings.CoalesceMS, 1, 60000); err != nil {
		return Telegram{}, err
	}
	if settings.ChatPerMinute, err = readInt("TELEGRAM_RATE_LIMIT_CHAT_PER_MIN", settings.ChatPerMinute, 1, 100000); err != nil {
		return Telegram{}, err
	}
	if settings.UserPerMinute, err = readInt("TELEGRAM_RATE_LIMIT_USER_PER_MIN", settings.UserPerMinute, 1, 100000); err != nil {
		return Telegram{}, err
	}
	if settings.MaxUpdateBytes, err = readInt("TELEGRAM_MAX_UPDATE_BYTES", settings.MaxUpdateBytes, 1024, 64*1024*1024); err != nil {
		return Telegram{}, err
	}
	if settings.PollTimeoutSeconds, err = readInt("TELEGRAM_POLL_TIMEOUT_SECONDS", settings.PollTimeoutSeconds, 1, 300); err != nil {
		return Telegram{}, err
	}
	if settings.FollowUpMinutes, err = readInt("TELEGRAM_FOLLOW_UP_MINUTES", settings.FollowUpMinutes, 1, 1440); err != nil {
		return Telegram{}, err
	}
	attachmentDefault := int(DefaultTelegramMaxAttachmentBytes)
	attachmentBytes, err := readInt("TELEGRAM_MAX_ATTACHMENT_BYTES", attachmentDefault, 1024, 2*1024*1024*1024)
	if err != nil {
		return Telegram{}, err
	}
	settings.MaxAttachmentBytes = int64(attachmentBytes)
	return settings, nil
}
