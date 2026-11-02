// Package llm выбирает LLM-провайдера и собирает конфиг ago для
// OpenAI-совместимого Chat Completions.
package llm

import (
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

// DefaultProvider используется, когда LLM_PROVIDER не задан: бот изначально
// работал через OpenRouter, поведение сохраняем.
const DefaultProvider = ProviderOpenRouter

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
//     на https://opencode.ai/inference/go/openai/v1/chat/completions, id модели
//     deepseek-v4.1-flash.
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
		BaseURL:   "https://opencode.ai/inference/go/openai/v1",
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
}

// Resolve дополняет настройки значениями провайдера по умолчанию.
func (s Settings) Resolve() (Spec, error) {
	spec, err := Lookup(s.Provider)
	if err != nil {
		return Spec{}, err
	}
	if base := strings.TrimSpace(s.BaseURL); base != "" {
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
	return cfg, spec, nil
}
