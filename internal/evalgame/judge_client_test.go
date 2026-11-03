package evalgame

import (
	"strings"
	"testing"

	agoconfig "github.com/luckystrker/ago/config"
	"github.com/luckystrker/ago/config/openai"

	"github.com/luckystriker/ai-dnd-system/internal/llm"
)

func envMap(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestJudgeProviderAndModel(t *testing.T) {
	t.Parallel()

	if got := JudgeProvider(envMap(nil)); got != llm.ProviderOpenCodeGo {
		t.Fatalf("провайдер судьи по умолчанию = %q, want opencode-go", got)
	}
	if got := JudgeProvider(envMap(map[string]string{JudgeProviderEnv: " openrouter "})); got != llm.ProviderOpenRouter {
		t.Fatalf("EVAL_JUDGE_PROVIDER не применился: %q", got)
	}
	if got := JudgeModel(envMap(nil)); got != DefaultJudgeModel {
		t.Fatalf("модель судьи по умолчанию = %q, want %q", got, DefaultJudgeModel)
	}
	if got := JudgeModel(envMap(map[string]string{JudgeModelEnv: " claude-opus-5 "})); got != "claude-opus-5" {
		t.Fatalf("EVAL_JUDGE_MODEL не применился: %q", got)
	}
}

func TestJudgeConfigOpenCodeGoUsesResponsesAPI(t *testing.T) {
	t.Parallel()

	cfg, err := JudgeConfig(envMap(map[string]string{"LLM_API_KEY": "test-key"}))
	if err != nil {
		t.Fatalf("JudgeConfig: %v", err)
	}
	if cfg.Provider() != agoconfig.ProviderOpenAI {
		t.Errorf("провайдер судьи = %q, want openai-совместимый", cfg.Provider())
	}
	if cfg.Model() != DefaultJudgeModel {
		t.Errorf("модель судьи = %q, want %q", cfg.Model(), DefaultJudgeModel)
	}
	// Muse Spark у OpenCode Go живёт на Responses API, а не на Chat Completions.
	if _, ok := cfg.(*openai.OpenAIResponsesConfig); !ok {
		t.Fatalf("ожидался конфиг Responses API, получен %T", cfg)
	}
}

func TestJudgeConfigOverrideProvider(t *testing.T) {
	t.Parallel()

	cfg, err := JudgeConfig(envMap(map[string]string{
		JudgeProviderEnv:     llm.ProviderOpenRouter,
		"OPENROUTER_API_KEY": "test-key",
		JudgeModelEnv:        "some-model",
	}))
	if err != nil {
		t.Fatalf("JudgeConfig: %v", err)
	}
	if _, ok := cfg.(*openai.OpenAICompletionsConfig); !ok {
		t.Fatalf("для OpenRouter ожидался Chat Completions, получен %T", cfg)
	}
	if cfg.Model() != "some-model" {
		t.Errorf("модель судьи = %q", cfg.Model())
	}
}

func TestNewJudgeClientRequiresKey(t *testing.T) {
	t.Parallel()

	_, err := NewJudgeClient(envMap(nil))
	if err == nil {
		t.Fatal("без ключа клиент судьи должен падать")
	}
	for _, want := range []string{"LLM_API_KEY", "OPENCODE_API_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ошибка %q не подсказывает %s", err, want)
		}
	}

	client, err := NewJudgeClient(envMap(map[string]string{"OPENCODE_API_KEY": "test-key"}))
	if err != nil {
		t.Fatalf("NewJudgeClient: %v", err)
	}
	if client == nil {
		t.Fatal("NewJudgeClient вернул nil без ошибки")
	}
}
