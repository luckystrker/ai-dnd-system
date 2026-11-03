package evalgame

import (
	"fmt"
	"strings"

	agoconfig "github.com/luckystrker/ago/config"

	"github.com/luckystriker/ai-dnd-system/internal/llm"
)

const (
	// DefaultJudgeProvider — провайдер судьи eval: тот же, что у бота.
	DefaultJudgeProvider = llm.ProviderOpenCodeGo

	// DefaultJudgeModel — модель LLM-судьи для живого прогона eval. Судья
	// должен быть сильнее и независимее модели бота: иначе он оценивает
	// собственные слепые пятна.
	//
	// muse-spark-1.3-contributor у OpenCode Go обслуживается Responses API,
	// а не Chat Completions: в каталоге https://opencode.ai/zen/go/v1/models
	// у модели указано api.responses.base_url =
	// https://opencode.ai/inference/go/openai/v1 и SDK @ai-sdk/openai
	// (таблица моделей: https://opencode.ai/v2/docs/console/go). На zen/go/v1
	// /responses отвечает так же (проверено живым запросом), поэтому судья
	// идёт через тот же корень, что и бот: openai.NewOpenAIResponses.
	DefaultJudgeModel = "muse-spark-1.3-contributor"

	// JudgeProviderEnv и JudgeModelEnv переопределяют провайдера и модель
	// судьи, не трогая модель бота.
	JudgeProviderEnv = "EVAL_JUDGE_PROVIDER"
	JudgeModelEnv    = "EVAL_JUDGE_MODEL"

	// JudgeSessionID — id разговора для провайдера: у OpenCode Go вызов без
	// заголовка x-opencode-session отвечает HTTP 400 MissingSessionID.
	JudgeSessionID = "ttrpg-eval-judge"
)

// JudgeProvider возвращает провайдера судьи: EVAL_JUDGE_PROVIDER или дефолт.
func JudgeProvider(getenv func(string) string) string {
	if provider := strings.TrimSpace(getenv(JudgeProviderEnv)); provider != "" {
		return provider
	}
	return DefaultJudgeProvider
}

// JudgeModel возвращает id модели судьи: EVAL_JUDGE_MODEL или дефолт.
func JudgeModel(getenv func(string) string) string {
	if model := strings.TrimSpace(getenv(JudgeModelEnv)); model != "" {
		return model
	}
	return DefaultJudgeModel
}

// JudgeConfig собирает конфиг судьи. Ключ — тот же, что у бота
// (LLM_API_KEY, затем ключ провайдера). OpenCode Go отдаёт свою дефолтную
// модель судьи только через Responses API; остальные провайдеры — через
// Chat Completions.
func JudgeConfig(getenv func(string) string) (agoconfig.ModelConfig, error) {
	spec, err := llm.Lookup(JudgeProvider(getenv))
	if err != nil {
		return nil, fmt.Errorf("судья eval: %w", err)
	}
	key := llm.APIKeyFromEnv(spec, getenv)
	if key == "" {
		return nil, fmt.Errorf(
			"не задан ключ судьи eval: заполните LLM_API_KEY или %s (провайдер %q, модель %q; переопределяются %s и %s)",
			spec.APIKeyEnv, spec.Provider, JudgeModel(getenv), JudgeProviderEnv, JudgeModelEnv,
		)
	}

	settings := llm.Settings{
		Provider: spec.Provider, APIKey: key, Model: JudgeModel(getenv),
		SessionID: JudgeSessionID,
	}
	var cfg agoconfig.ModelConfig
	if spec.Provider == llm.ProviderOpenCodeGo {
		cfg, _, err = settings.ResponsesModelConfig()
	} else {
		cfg, _, err = settings.ModelConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("судья eval: %w", err)
	}
	return cfg, nil
}

// NewJudgeClient создаёт клиента судьи. Без ключа живой прогон падает сразу
// и с понятной ошибкой, а не на первом вызове судьи; офлайн-тесты клиента не
// создают и ключа не требуют.
func NewJudgeClient(getenv func(string) string) (agoconfig.LLMClient, error) {
	cfg, err := JudgeConfig(getenv)
	if err != nil {
		return nil, err
	}
	return cfg.Create()
}
