package config_test

import (
	"strings"
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/config"
	"github.com/luckystriker/ai-dnd-system/internal/llm"
)

// envMap превращает карту в getenv, чтобы не трогать процессное окружение.
func envMap(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(envMap(map[string]string{"LLM_API_KEY": "sk-test"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Provider != llm.DefaultProvider {
		t.Errorf("провайдер = %q, want %q", cfg.LLM.Provider, llm.DefaultProvider)
	}
	if cfg.LLM.APIKey != "sk-test" {
		t.Errorf("ключ = %q", cfg.LLM.APIKey)
	}
	if cfg.MaxToolRounds != config.DefaultMaxToolRounds {
		t.Errorf("MaxToolRounds = %d, want %d", cfg.MaxToolRounds, config.DefaultMaxToolRounds)
	}
	if cfg.DBPath != config.DefaultDBPath {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, config.DefaultDBPath)
	}
	if cfg.ContextWindowTokens != config.DefaultContextWindowTokens {
		t.Errorf("ContextWindowTokens = %d, want %d", cfg.ContextWindowTokens, config.DefaultContextWindowTokens)
	}
	if cfg.TurnTimeoutSeconds != config.DefaultTurnTimeoutSeconds {
		t.Errorf("TurnTimeoutSeconds = %d, want %d", cfg.TurnTimeoutSeconds, config.DefaultTurnTimeoutSeconds)
	}
	if cfg.TokenBudget != config.DefaultTokenBudget {
		t.Errorf("TokenBudget = %d, want %d", cfg.TokenBudget, config.DefaultTokenBudget)
	}
}

func TestLoadContextLimits(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(envMap(map[string]string{
		"LLM_API_KEY":           "sk-test",
		"CONTEXT_WINDOW_TOKENS": "64000",
		"TURN_TIMEOUT_SECONDS":  "90",
		"TOKEN_BUDGET":          "150000",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ContextWindowTokens != 64000 {
		t.Errorf("ContextWindowTokens = %d, want 64000", cfg.ContextWindowTokens)
	}
	if cfg.TurnTimeoutSeconds != 90 {
		t.Errorf("TurnTimeoutSeconds = %d, want 90", cfg.TurnTimeoutSeconds)
	}
	if cfg.TokenBudget != 150000 {
		t.Errorf("TokenBudget = %d, want 150000", cfg.TokenBudget)
	}
}

func TestLoadProviderKeyFallback(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(envMap(map[string]string{
		"LLM_PROVIDER":     llm.ProviderOpenCodeGo,
		"OPENCODE_API_KEY": "oc-key",
		"DEEPSEEK_API_KEY": "ds-key",
		"CAMPAIGN_DB_PATH": "/tmp/game.db",
		"MAX_TOOL_ROUNDS":  "5",
		"LLM_MODEL":        "custom",
		"LLM_BASE_URL":     "https://proxy.example/v1",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.APIKey != "oc-key" {
		t.Errorf("ключ = %q, want ключ провайдера из OPENCODE_API_KEY", cfg.LLM.APIKey)
	}
	if cfg.MaxToolRounds != 5 {
		t.Errorf("MaxToolRounds = %d, want 5", cfg.MaxToolRounds)
	}
	if cfg.DBPath != "/tmp/game.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.LLM.Model != "custom" || cfg.LLM.BaseURL != "https://proxy.example/v1" {
		t.Errorf("переопределения не применились: %+v", cfg.LLM)
	}
}

func TestLoadBaseURLValidation(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(envMap(map[string]string{
		"LLM_API_KEY":  "k",
		"LLM_BASE_URL": "https://openrouter.ai/api/v1/chat/completions",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, _, err := cfg.LLM.ModelConfig(); err == nil {
		t.Fatal("полный путь /chat/completions должен отклоняться при сборке конфига")
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		env    map[string]string
		substr string
	}{
		{
			name:   "нет ключа",
			env:    map[string]string{},
			substr: "LLM_API_KEY",
		},
		{
			name:   "неизвестный провайдер",
			env:    map[string]string{"LLM_PROVIDER": "bogus", "LLM_API_KEY": "k"},
			substr: "LLM_PROVIDER",
		},
		{
			name:   "битый лимит раундов",
			env:    map[string]string{"LLM_API_KEY": "k", "MAX_TOOL_ROUNDS": "много"},
			substr: "MAX_TOOL_ROUNDS",
		},
		{
			name:   "нулевой лимит раундов",
			env:    map[string]string{"LLM_API_KEY": "k", "MAX_TOOL_ROUNDS": "0"},
			substr: "MAX_TOOL_ROUNDS",
		},
		{
			name:   "маленькое окно контекста",
			env:    map[string]string{"LLM_API_KEY": "k", "CONTEXT_WINDOW_TOKENS": "100"},
			substr: "CONTEXT_WINDOW_TOKENS",
		},
		{
			name:   "битый дедлайн хода",
			env:    map[string]string{"LLM_API_KEY": "k", "TURN_TIMEOUT_SECONDS": "0"},
			substr: "TURN_TIMEOUT_SECONDS",
		},
		{
			name:   "битый бюджет токенов",
			env:    map[string]string{"LLM_API_KEY": "k", "TOKEN_BUDGET": "-1"},
			substr: "TOKEN_BUDGET",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(envMap(tc.env))
			if err == nil {
				t.Fatal("ожидалась ошибка")
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("ошибка %q не содержит %q", err, tc.substr)
			}
		})
	}
}
