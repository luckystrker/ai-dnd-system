package llm_test

import (
	"strings"
	"testing"

	agoconfig "github.com/luckystrker/ago/config"

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
		{llm.ProviderOpenCodeGo, "https://opencode.ai/inference/go/openai/v1", "deepseek-v4.1-flash", "OPENCODE_API_KEY"},
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
