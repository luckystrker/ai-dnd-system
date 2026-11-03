// Package llm выбирает LLM-провайдера и собирает конфиг ago для
// OpenAI-совместимого Chat Completions.
package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	agoconfig "github.com/luckystrker/ago/config"
	"github.com/luckystrker/ago/config/openai"
)

// Имена провайдеров, которые понимает LLM_PROVIDER.
const (
	ProviderOpenRouter = "openrouter"
	ProviderOpenCodeGo = "opencode-go"
	ProviderDeepSeek   = "deepseek"
)

// DefaultProvider используется, когда LLM_PROVIDER не задан: решением
// владельца бот по умолчанию работает через OpenCode Go.
const DefaultProvider = ProviderOpenCodeGo

// OpenCodeSessionHeader — обязательный заголовок OpenCode Go: без него
// эндпоинт отвечает HTTP 400 MissingSessionID («cannot be routed
// efficiently»). Значение — стабильный идентификатор разговора: провайдер
// использует его для маршрутизации и кэша промптов.
const OpenCodeSessionHeader = "x-opencode-session"

// DefaultSessionID — сессия по умолчанию, если вызывающий код не задал свою.
// OpenCode Go не принимает запросы без заголовка, поэтому пустым он не бывает.
const DefaultSessionID = "ttrpg-bot"

// Spec — провайдер по умолчанию: корень эндпоинта, id модели и переменная
// окружения с ключом. ago дописывает к BaseURL путь /chat/completions, поэтому
// BaseURL — это корень вида https://host/v1, а не полный URL запроса.
type Spec struct {
	Provider  string
	BaseURL   string
	Model     string
	APIKeyEnv string
	DocsURL   string
}

// specs — таблица провайдеров. BaseURL и id модели сверены с документацией
// провайдеров на 2026-11-02:
//
//   - OpenRouter: https://openrouter.ai/docs/api-reference/overview —
//     base URL https://openrouter.ai/api/v1, id модели из каталога
//     https://openrouter.ai/api/v1/models (deepseek/deepseek-v4.1-flash).
//   - OpenCode Go: https://opencode.ai/v2/docs/console/go — Chat Completions
//     на https://opencode.ai/zen/go/v1/chat/completions, id модели
//     deepseek-v4.1-flash. Каталог моделей тоже живёт на zen/go/v1: на
//     /inference/go/openai/v1 он отдаёт 404, хотя chat completions там
//     отвечает. Рабочий бот владельца ходит именно на zen/go/v1.
//     Любой запрос обязан нести заголовок x-opencode-session (иначе
//     HTTP 400 MissingSessionID) — его ставит applyHeaders.
//   - DeepSeek: https://api-docs.deepseek.com/ — base_url
//     https://api.deepseek.com, публичный id модели deepseek-flash (его
//     обслуживает DeepSeek-V4.1-Flash-1017; старые id вроде deepseek-v4-flash
//     ещё принимаются, но новых id deepseek-v4.1-flash у провайдера нет).
var specs = map[string]Spec{
	ProviderOpenRouter: {
		Provider:  ProviderOpenRouter,
		BaseURL:   "https://openrouter.ai/api/v1",
		Model:     "deepseek/deepseek-v4.1-flash",
		APIKeyEnv: "OPENROUTER_API_KEY",
		DocsURL:   "https://openrouter.ai/docs/api-reference/overview",
	},
	ProviderOpenCodeGo: {
		Provider:  ProviderOpenCodeGo,
		BaseURL:   "https://opencode.ai/zen/go/v1",
		Model:     "deepseek-v4.1-flash",
		APIKeyEnv: "OPENCODE_API_KEY",
		DocsURL:   "https://opencode.ai/v2/docs/console/go",
	},
	ProviderDeepSeek: {
		Provider:  ProviderDeepSeek,
		BaseURL:   "https://api.deepseek.com",
		Model:     "deepseek-flash",
		APIKeyEnv: "DEEPSEEK_API_KEY",
		DocsURL:   "https://api-docs.deepseek.com/",
	},
}

// Names возвращает известные имена провайдеров в алфавитном порядке — для
// сообщений об ошибке.
func Names() []string {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Lookup возвращает провайдера по имени; пустое имя — провайдер по умолчанию.
func Lookup(name string) (Spec, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		normalized = DefaultProvider
	}
	spec, ok := specs[normalized]
	if !ok {
		return Spec{}, fmt.Errorf("неизвестный LLM_PROVIDER %q, доступны: %s", name, strings.Join(Names(), ", "))
	}
	return spec, nil
}

// Settings — сырые настройки из окружения. BaseURL и Model необязательны:
// пустые значения означают «взять из Spec провайдера».
type Settings struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
	// SessionID — стабильный id разговора для провайдера (в OpenCode Go —
	// заголовок x-opencode-session). Пусто — DefaultSessionID; у OpenCode Go
	// заголовок обязателен, у остальных провайдеров не отправляется.
	SessionID string
}

// APIKeyFromEnv резолвит ключ: LLM_API_KEY, затем переменная провайдера.
func APIKeyFromEnv(spec Spec, getenv func(string) string) string {
	if key := strings.TrimSpace(getenv("LLM_API_KEY")); key != "" {
		return key
	}
	return strings.TrimSpace(getenv(spec.APIKeyEnv))
}

// Resolve дополняет настройки значениями провайдера по умолчанию.
func (s Settings) Resolve() (Spec, error) {
	spec, err := Lookup(s.Provider)
	if err != nil {
		return Spec{}, err
	}
	if base := strings.TrimSpace(s.BaseURL); base != "" {
		base = strings.TrimRight(base, "/")
		if strings.HasSuffix(strings.ToLower(base), "/chat/completions") {
			return Spec{}, fmt.Errorf(
				"LLM_BASE_URL указывает на полный путь /chat/completions (%q): ago дописывает его сам, укажите корень эндпоинта, например %q",
				s.BaseURL, spec.BaseURL,
			)
		}
		spec.BaseURL = base
	}
	if model := strings.TrimSpace(s.Model); model != "" {
		spec.Model = model
	}
	return spec, nil
}

// ModelConfig собирает конфиг Chat Completions для ago.
func (s Settings) ModelConfig() (agoconfig.ModelConfig, Spec, error) {
	spec, err := s.Resolve()
	if err != nil {
		return nil, Spec{}, err
	}
	if strings.TrimSpace(s.APIKey) == "" {
		return nil, Spec{}, fmt.Errorf("не задан ключ LLM: заполните LLM_API_KEY или %s", spec.APIKeyEnv)
	}
	cfg := openai.NewOpenAICompletions(spec.Model)
	cfg.APIKey = s.APIKey
	cfg.BaseURL = spec.BaseURL
	applyHeaders(cfg, spec, s.SessionID)
	return cfg, spec, nil
}

// ResponsesModelConfig собирает конфиг OpenAI Responses API для моделей,
// которые провайдер отдаёт только через /responses (например, muse-spark-*
// у OpenCode Go). BaseURL тот же корень: ago дописывает /responses.
func (s Settings) ResponsesModelConfig() (agoconfig.ModelConfig, Spec, error) {
	spec, err := s.Resolve()
	if err != nil {
		return nil, Spec{}, err
	}
	if strings.TrimSpace(s.APIKey) == "" {
		return nil, Spec{}, fmt.Errorf("не задан ключ LLM: заполните LLM_API_KEY или %s", spec.APIKeyEnv)
	}
	cfg := openai.NewOpenAIResponses(spec.Model)
	cfg.APIKey = s.APIKey
	cfg.BaseURL = spec.BaseURL
	// OpenCode Go не хранит ответы Responses API: с дефолтным store=true
	// /responses отвечает HTTP 400 «OpenCode doesn't store responses; omit
	// store or set it to false». Store=false — требование провайдера, а не
	// наша настройка.
	if spec.Provider == ProviderOpenCodeGo {
		storeResponses := false
		cfg.Store = &storeResponses
	}
	applyHeaders(cfg, spec, s.SessionID)
	return cfg, spec, nil
}

// applyHeaders выставляет обязательные заголовки провайдера на собранном
// конфиге. OpenCode Go без x-opencode-session отвечает HTTP 400
// MissingSessionID, поэтому пустой SessionID заменяется на DefaultSessionID.
func applyHeaders(cfg agoconfig.ModelConfig, spec Spec, sessionID string) {
	if spec.Provider != ProviderOpenCodeGo {
		return
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = DefaultSessionID
	}
	switch typed := cfg.(type) {
	case *openai.OpenAICompletionsConfig:
		if typed.DefaultHeaders == nil {
			typed.DefaultHeaders = map[string]string{}
		}
		typed.DefaultHeaders[OpenCodeSessionHeader] = sessionID
	case *openai.OpenAIResponsesConfig:
		if typed.DefaultHeaders == nil {
			typed.DefaultHeaders = map[string]string{}
		}
		typed.DefaultHeaders[OpenCodeSessionHeader] = sessionID
	}
}

// SessionConfig возвращает копию настроек с другим id разговора: один процесс
// обслуживает несколько чатов, и каждому нужен свой x-opencode-session.
func (s Settings) SessionConfig(sessionID string) Settings {
	s.SessionID = sessionID
	return s
}

// Служебные id разговоров: заголовок уходит провайдеру, поэтому chat id в
// открытом виде не отправляется — только хеш.
const (
	ChroniclerSessionID = "ttrpg-chronicler"
	ReplSessionID       = "ttrpg-repl"
	EvalSessionPrefix   = "ttrpg-eval-"
)

// ChatSessionID — стабильный id разговора для Telegram-чата/топика. Хеш, а не
// сам chat id: наружу уходит только идентификатор сессии провайдера.
func ChatSessionID(chatKey string) string {
	if strings.TrimSpace(chatKey) == "" {
		return DefaultSessionID
	}
	sum := sha256.Sum256([]byte("ttrpg-chat-" + chatKey))
	return "ttrpg-" + hex.EncodeToString(sum[:8])
}
