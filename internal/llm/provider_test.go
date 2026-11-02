package llm_test

import (
	"strings"
	"testing"

	agoconfig "github.com/luckystrker/ago/config"
	"github.com/luckystrker/ago/config/openai"

	"github.com/luckystriker/ai-dnd-system/internal/llm"
)

// Таблица провайдеров: BaseURL — корень эндпоинта (ago дописывает
// /chat/completions), id модели у каждого провайдера свой.
func TestSpecs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		provider string
		baseURL  string
		model    string
		keyEnv   string
	}{
		{llm.ProviderOpenRouter, "https://openrouter.ai/api/v1", "deepseek/deepseek-v4.1-flash", "OPENROUTER_API_KEY"},
		{llm.ProviderOpenCodeGo, "https://opencode.ai/zen/go/v1", "deepseek-v4.1-flash", "OPENCODE_API_KEY"},
		{llm.ProviderDeepSeek, "https://api.deepseek.com", "deepseek-flash", "DEEPSEEK_API_KEY"},
	}

	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			spec, err := llm.Lookup(tc.provider)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", tc.provider, err)
			}
			if spec.BaseURL != tc.baseURL {
				t.Errorf("BaseURL = %q, want %q", spec.BaseURL, tc.baseURL)
			}
			if spec.Model != tc.model {
				t.Errorf("Model = %q, want %q", spec.Model, tc.model)
			}
			if spec.APIKeyEnv != tc.keyEnv {
				t.Errorf("APIKeyEnv = %q, want %q", spec.APIKeyEnv, tc.keyEnv)
			}
			if !strings.HasPrefix(spec.DocsURL, "https://") {
				t.Errorf("DocsURL = %q, want https-ссылку на документацию", spec.DocsURL)
			}
		})
	}
}

func TestLookupDefaultsAndUnknown(t *testing.T) {
	t.Parallel()

	spec, err := llm.Lookup("")
	if err != nil {
		t.Fatalf("Lookup(\"\") : %v", err)
	}
	if spec.Provider != llm.DefaultProvider {
		t.Fatalf("провайдер по умолчанию = %q, want %q", spec.Provider, llm.DefaultProvider)
	}

	if _, err := llm.Lookup("nope"); err == nil {
		t.Fatal("неизвестный провайдер должен давать ошибку")
	} else if !strings.Contains(err.Error(), llm.ProviderOpenRouter) {
		t.Fatalf("ошибка должна перечислять доступных провайдеров, получено: %v", err)
	}
}

func TestResolveOverrides(t *testing.T) {
	t.Parallel()

	spec, err := llm.Settings{
		Provider: llm.ProviderDeepSeek,
		BaseURL:  "https://proxy.example/v1",
		Model:    "custom-model",
	}.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if spec.BaseURL != "https://proxy.example/v1" {
		t.Errorf("BaseURL = %q, want переопределённый", spec.BaseURL)
	}
	if spec.Model != "custom-model" {
		t.Errorf("Model = %q, want переопределённый", spec.Model)
	}
}

func TestResolveBaseURL(t *testing.T) {
	t.Parallel()

	spec, err := llm.Settings{Provider: llm.ProviderOpenRouter, BaseURL: "https://proxy.example/v1/"}.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if spec.BaseURL != "https://proxy.example/v1" {
		t.Errorf("BaseURL = %q, want без хвостового слэша", spec.BaseURL)
	}

	// Полный путь из чужой документации: ago дописал бы /chat/completions ещё
	// раз, поэтому ошибка должна быть явной и до первого запроса.
	for _, base := range []string{
		"https://openrouter.ai/api/v1/chat/completions",
		"https://api.deepseek.com/chat/completions/",
	} {
		_, err := llm.Settings{Provider: llm.ProviderOpenRouter, BaseURL: base}.Resolve()
		if err == nil {
			t.Fatalf("BaseURL %q должен отклоняться", base)
		}
		if !strings.Contains(err.Error(), "/chat/completions") {
			t.Fatalf("ошибка %q не объясняет про /chat/completions", err)
		}
	}
}

func TestModelConfig(t *testing.T) {
	t.Parallel()

	cfg, spec, err := llm.Settings{Provider: llm.ProviderOpenCodeGo, APIKey: "test-key"}.ModelConfig()
	if err != nil {
		t.Fatalf("ModelConfig: %v", err)
	}
	if spec.Provider != llm.ProviderOpenCodeGo {
		t.Fatalf("Provider = %q", spec.Provider)
	}
	if cfg.Provider() != agoconfig.Provider("openai") {
		t.Errorf("провайдер ago = %q, want openai", cfg.Provider())
	}
	if cfg.Model() != "deepseek-v4.1-flash" {
		t.Errorf("модель = %q", cfg.Model())
	}

	// Create() должен принять собранный конфиг офлайн: без ключа — ошибка.
	if _, _, err := (llm.Settings{Provider: llm.ProviderDeepSeek}).ModelConfig(); err == nil {
		t.Fatal("без ключа ModelConfig должен падать")
	}

	bad := llm.Settings{Provider: "bogus", APIKey: "k"}
	if _, _, err := bad.ModelConfig(); err == nil {
		t.Fatal("неизвестный провайдер должен падать")
	}
}

// OpenCode Go без x-opencode-session отвечает HTTP 400 MissingSessionID
// (проверено живым запросом и рабочей настройкой бота владельца), поэтому
// заголовок обязателен и у Chat Completions, и у Responses API.
func TestOpenCodeGoSessionHeader(t *testing.T) {
	t.Parallel()

	chat, _, err := llm.Settings{Provider: llm.ProviderOpenCodeGo, APIKey: "k"}.ModelConfig()
	if err != nil {
		t.Fatalf("ModelConfig: %v", err)
	}
	typed, ok := chat.(*openai.OpenAICompletionsConfig)
	if !ok {
		t.Fatalf("ожидался *openai.OpenAICompletionsConfig, получено %T", chat)
	}
	if got := typed.DefaultHeaders[llm.OpenCodeSessionHeader]; got != llm.DefaultSessionID {
		t.Fatalf("заголовок %s = %q, want %q", llm.OpenCodeSessionHeader, got, llm.DefaultSessionID)
	}

	responses, _, err := llm.Settings{
		Provider: llm.ProviderOpenCodeGo, APIKey: "k", Model: "muse-spark-1.3-contributor",
		SessionID: "judge-session",
	}.ResponsesModelConfig()
	if err != nil {
		t.Fatalf("ResponsesModelConfig: %v", err)
	}
	typedResponses, ok := responses.(*openai.OpenAIResponsesConfig)
	if !ok {
		t.Fatalf("ожидался *openai.OpenAIResponsesConfig, получено %T", responses)
	}
	if got := typedResponses.DefaultHeaders[llm.OpenCodeSessionHeader]; got != "judge-session" {
		t.Fatalf("заголовок судьи = %q, want judge-session", got)
	}
}

// Остальные провайдеры заголовок OpenCode не получают: он им не нужен и
// может удивить прокси.
func TestOtherProvidersHaveNoSessionHeader(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{llm.ProviderOpenRouter, llm.ProviderDeepSeek} {
		cfg, _, err := llm.Settings{Provider: provider, APIKey: "k"}.ModelConfig()
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		typed, ok := cfg.(*openai.OpenAICompletionsConfig)
		if !ok {
			t.Fatalf("%s: ожидался *openai.OpenAICompletionsConfig", provider)
		}
		if _, exists := typed.DefaultHeaders[llm.OpenCodeSessionHeader]; exists {
			t.Fatalf("%s не должен слать %s", provider, llm.OpenCodeSessionHeader)
		}
	}
}

func TestChatSessionIDIsStableAndHashed(t *testing.T) {
	t.Parallel()

	first := llm.ChatSessionID("-1001234567890:42")
	second := llm.ChatSessionID("-1001234567890:42")
	if first != second {
		t.Fatalf("ChatSessionID нестабилен: %q != %q", first, second)
	}
	if first == llm.ChatSessionID("-1001234567890:43") {
		t.Fatal("разные чаты/топики должны давать разные сессии")
	}
	if strings.Contains(first, "1001234567890") {
		t.Fatalf("chat id утёк в id сессии: %q", first)
	}
	if llm.ChatSessionID("") != llm.DefaultSessionID {
		t.Fatalf("пустой ключ должен давать %q", llm.DefaultSessionID)
	}
}
