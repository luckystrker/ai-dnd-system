// Package calibrate — офлайн-калибровка двух решений канала Telegram:
// гейта «отвечать ли» и окна склейки сообщений. Значения (2.5 с, 10 минут)
// не меняются: пакет даёт владельцу данные, сколько ходов LLM будит каждая
// последовательность сообщений и сколько подавляет.
package calibrate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/luckystriker/ai-dnd-system/internal/telegram"
)

// Msg — сообщение сценария. At — секунды от начала сценария.
type Msg struct {
	At      time.Duration
	User    string
	Text    string
	Reply   bool // reply на сообщение бота
	Command bool
	Chat    string // "" → group
}

// Scenario — последовательность сообщений одного чата.
type Scenario struct {
	Name        string
	Description string
	Messages    []Msg
}

// Config — окна канала. Ноль — боевые дефолты.
type Config struct {
	Coalesce   time.Duration // 2.5 с
	FollowUp   time.Duration // 10 минут
	TurnLength time.Duration // сколько длится ход LLM; 0 → 30 с
	BotName    string
}

// BatchResult — один склеенный батч.
type BatchResult struct {
	FlushedAt time.Duration
	Messages  int
	Woke      bool
}

// ScenarioResult — результат сценария.
type ScenarioResult struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Messages    int           `json:"messages"`
	Batches     int           `json:"batches"`
	Turns       int           `json:"llm_turns"`
	Suppressed  int           `json:"suppressed_batches"`
	Details     []BatchResult `json:"batches_detail"`
}

// Report — сводка по всем сценариям.
type Report struct {
	Coalesce   string           `json:"coalesce_window"`
	FollowUp   string           `json:"follow_up_window"`
	TurnLength string           `json:"turn_length"`
	Scenarios  []ScenarioResult `json:"scenarios"`
	Messages   int              `json:"messages"`
	Turns      int              `json:"llm_turns"`
	Suppressed int              `json:"suppressed_batches"`
}

// DefaultConfig — боевые значения канала.
func DefaultConfig() Config {
	return Config{
		Coalesce:   telegram.DefaultCoalesceWindow,
		FollowUp:   telegram.DefaultFollowUpWindow,
		TurnLength: 30 * time.Second,
		BotName:    "dnd_bot",
	}
}

// Scenarios — типовые последовательности группового чата.
func Scenarios() []Scenario {
	return []Scenario{
		{
			Name:        "ooc-dialog",
			Description: "Два игрока обсуждают план, бот не адресован",
			Messages: []Msg{
				{At: 0, User: "den", Text: "Лиса, давай сначала осмотрим двор, а потом зайдём внутрь."},
				{At: 1500 * time.Millisecond, User: "lisa", Text: "Согласна, двор выглядит безопаснее."},
				{At: 20 * time.Second, User: "den", Text: "Тогда я проверю колодец."},
				{At: 21 * time.Second, User: "lisa", Text: "А я — повозку."},
			},
		},
		{
			Name:        "burst-then-question",
			Description: "Игрок пишет две реплики подряд (склейка 2.5 с), затем прямо спрашивает ДМ",
			Messages: []Msg{
				{At: 0, User: "den", Text: "Я подхожу к трактирщику."},
				{At: 1200 * time.Millisecond, User: "den", Text: "И спрашиваю, как его зовут."},
				{At: 30 * time.Second, User: "den", Text: "ДМ, а как зовут трактирщика?"},
			},
		},
		{
			Name:        "command",
			Description: "/join в привязанном чате",
			Messages:    []Msg{{At: 0, User: "lisa", Text: "/join", Command: true}},
		},
		{
			Name:        "reply-within-follow-up",
			Description: "Ответ на реплику бота через 9 минут — будит ход",
			Messages: []Msg{
				{At: 0, User: "den", Text: "Я иду к алтарю.", Reply: true},
				{At: 9 * time.Minute, User: "lisa", Text: "Я прикрываю его."},
			},
		},
		{
			Name:        "reply-after-follow-up",
			Description: "Ответ через 11 минут — окно 10 минут истекло, переписка не будит LLM",
			Messages: []Msg{
				{At: 0, User: "den", Text: "Я иду к алтарю.", Reply: true},
				{At: 11 * time.Minute, User: "lisa", Text: "Я прикрываю его."},
			},
		},
		{
			Name:        "mention",
			Description: "@упоминание бота среди разговора игроков",
			Messages: []Msg{
				{At: 0, User: "den", Text: "Мы ждём у ворот."},
				{At: 40 * time.Second, User: "lisa", Text: "@dnd_bot сколько до рассвета?"},
			},
		},
	}
}

// Run прогоняет сценарии. Гейт — настоящий telegram.Gate с подменёнными
// часами; склейка повторяет очередь чата: батч закрывается через окно,
// сообщения во время хода копятся в следующий батч.
func Run(scenarios []Scenario, cfg Config) Report {
	cfg = withDefaults(cfg)
	report := Report{
		Coalesce:   cfg.Coalesce.String(),
		FollowUp:   cfg.FollowUp.String(),
		TurnLength: cfg.TurnLength.String(),
	}
	for _, scenario := range scenarios {
		result := runScenario(scenario, cfg)
		report.Scenarios = append(report.Scenarios, result)
		report.Messages += result.Messages
		report.Turns += result.Turns
		report.Suppressed += result.Suppressed
	}
	return report
}

func withDefaults(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.Coalesce <= 0 {
		cfg.Coalesce = defaults.Coalesce
	}
	if cfg.FollowUp <= 0 {
		cfg.FollowUp = defaults.FollowUp
	}
	if cfg.TurnLength <= 0 {
		cfg.TurnLength = defaults.TurnLength
	}
	if cfg.BotName == "" {
		cfg.BotName = defaults.BotName
	}
	return cfg
}

func runScenario(scenario Scenario, cfg Config) ScenarioResult {
	result := ScenarioResult{Name: scenario.Name, Description: scenario.Description, Messages: len(scenario.Messages)}
	if len(scenario.Messages) == 0 {
		return result
	}
	messages := make([]Msg, len(scenario.Messages))
	copy(messages, scenario.Messages)
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].At < messages[j].At })

	now := time.Unix(0, 0)
	gate := telegram.NewGate(nil, cfg.BotName, func() time.Time { return now })
	gate.SetFollowUpWindow(cfg.FollowUp)
	chatKey := "calibrate:" + scenario.Name

	var (
		pending    []telegram.Incoming
		batchStart time.Duration
		turnEnd    time.Duration
	)
	flush := func(at time.Duration) {
		if len(pending) == 0 {
			return
		}
		now = time.Unix(0, 0).Add(at)
		woke := gate.ShouldReply(chatKey, pending)
		result.Batches++
		result.Details = append(result.Details, BatchResult{FlushedAt: at, Messages: len(pending), Woke: woke})
		if woke {
			result.Turns++
			// Ход занимает TurnLength; ответ бота открывает follow-up окно.
			turnEnd = at + cfg.TurnLength
			now = time.Unix(0, 0).Add(turnEnd)
			gate.MarkBotReply(chatKey)
		} else {
			result.Suppressed++
		}
		pending = nil
	}

	for _, msg := range messages {
		if len(pending) == 0 {
			pending = append(pending, incoming(msg))
			batchStart = msg.At
			continue
		}
		// Идёт ход: сообщения копятся на следующий батч без дополнительного окна.
		if msg.At < turnEnd {
			pending = append(pending, incoming(msg))
			continue
		}
		// Окно склейки ещё не вышло — сообщение вливается в текущий батч.
		if msg.At-batchStart < cfg.Coalesce {
			pending = append(pending, incoming(msg))
			continue
		}
		flushAt := batchStart + cfg.Coalesce
		if flushAt < turnEnd {
			flushAt = turnEnd
		}
		flush(flushAt)
		pending = append(pending, incoming(msg))
		batchStart = msg.At
	}
	flushAt := batchStart + cfg.Coalesce
	if flushAt < turnEnd {
		flushAt = turnEnd
	}
	flush(flushAt)
	return result
}

func incoming(msg Msg) telegram.Incoming {
	chatType := msg.Chat
	if chatType == "" {
		chatType = "group"
	}
	text := msg.Text
	if msg.Command && !strings.HasPrefix(text, "/") {
		text = "/" + text
	}
	message := &telegram.Message{
		MessageID: int64(len(text)),
		From:      &telegram.User{ID: 1, FirstName: msg.User, Username: msg.User},
		Chat:      telegram.Chat{ID: -1001, Type: chatType},
		Text:      text,
	}
	if msg.Reply {
		message.ReplyToMessage = &telegram.Message{
			From: &telegram.User{ID: 2, IsBot: true, FirstName: "bot", Username: "dnd_bot"},
		}
	}
	return telegram.Incoming{Message: message}
}

// Summary — короткая печать отчёта.
func (r Report) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Telegram: склейка %s, follow-up %s, ход %s\n", r.Coalesce, r.FollowUp, r.TurnLength)
	fmt.Fprintf(&b, "Всего: сообщений %d, ходов LLM %d, подавлено батчей %d\n", r.Messages, r.Turns, r.Suppressed)
	for _, scenario := range r.Scenarios {
		fmt.Fprintf(&b, "  %-24s сообщений %d, батчей %d, ходов %d, подавлено %d\n",
			scenario.Name, scenario.Messages, scenario.Batches, scenario.Turns, scenario.Suppressed)
	}
	return b.String()
}
