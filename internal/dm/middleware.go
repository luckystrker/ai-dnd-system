package dm

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"

	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/middleware"
	"github.com/luckystrker/ago/policies"

	"github.com/luckystriker/ai-dnd-system/internal/store"
	"github.com/luckystriker/ai-dnd-system/internal/tools"
)

const sessionDepKey = "ttrpg.session"

// memoryPolicy собирает блок памяти кампании и вставляет его в хвост
// контекста — прямо перед последним сообщением пользователя. Системный
// префикс (PromptLines) при этом не меняется, поэтому кэш промпта между
// ходами остаётся валидным: меняется только хвост истории.
//
// Заодно применяется жёсткий лимит окна к истории (страховка поверх compact)
// и один раз за сессию добавляется предупреждение о превышении бюджета.
type memoryPolicy struct {
	middleware.Base
	store      *store.Store
	budget     int64
	total      func() int64
	budgetWarn atomic.Bool
	log        *slog.Logger
	window     policies.AssemblyPolicy
}

func newMemoryPolicy(st *store.Store, window int, budget int64, total func() int64, log *slog.Logger) *memoryPolicy {
	policy := &memoryPolicy{store: st, budget: budget, total: total, log: log}
	if window > 0 {
		policy.window = policies.NewTokenBudgetPolicy(window)
	}
	return policy
}

func (p *memoryPolicy) OnLLMCall(next middleware.LLMCallFunc, evs []events.Event, cx *agocontext.Context) (*events.ModelResponse, error) {
	if p.window != nil {
		// PromptLines не переписываем: системный префикс должен быть стабилен.
		_, reduced, err := p.window.Apply(cx.PromptLines(), evs, cx)
		if err != nil {
			return nil, err
		}
		evs = reduced
	}
	block := BuildMemoryBlock(p.store, sessionFrom(cx), historyText(evs))
	block = p.withBudgetNotice(block)
	if block != "" {
		evs = insertBeforeLastRequest(evs, block)
	}
	return next(evs, cx)
}

// withBudgetNotice — понятное поведение при превышении бюджета: модель один
// раз получает просьбу свернуть сцену. Игроков это не касается, ход не
// обрывается, записанное тулами остаётся.
func (p *memoryPolicy) withBudgetNotice(block string) string {
	if p.budget <= 0 || p.total == nil || p.total() <= p.budget {
		return block
	}
	if p.budgetWarn.CompareAndSwap(false, true) {
		p.log.Warn("dm: session token budget exceeded",
			"budget", p.budget, "used", p.total())
	}
	notice := "Внимание: бюджет токенов этой сессии превышен. Заверши текущую сцену " +
		"коротко, предложи игрокам паузу и не начинай новых больших сцен без явной просьбы."
	if block == "" {
		return notice
	}
	return block + "\n\n" + notice
}

// insertBeforeLastRequest кладёт служебный блок перед последним запросом
// пользователя. Если запроса в истории нет (Resume с другого события) —
// контекст не трогаем.
func insertBeforeLastRequest(evs []events.Event, block string) []events.Event {
	index := -1
	for i := len(evs) - 1; i >= 0; i-- {
		switch events.AsValue(evs[i]).(type) {
		case events.DrainedModelRequest, events.ModelRequest:
			index = i
		}
		if index >= 0 {
			break
		}
	}
	if index < 0 {
		return evs
	}
	memory := events.ModelRequest{
		Parts: []events.Input{events.TextInput{
			Content: "Служебный блок памяти кампании (обновляется каждый ход):\n\n" + block,
		}},
		Metadata: map[string]any{"kind": "campaign_memory"},
	}
	out := make([]events.Event, 0, len(evs)+1)
	out = append(out, evs[:index]...)
	out = append(out, memory)
	out = append(out, evs[index:]...)
	return out
}

// historyText собирает уже видимый модели текст: сообщения, ответы и
// результаты тулов. По нему dayTailSection отсеивает строки транскрипта,
// которые и так есть в окне истории.
func historyText(evs []events.Event) string {
	var builder strings.Builder
	for _, ev := range evs {
		switch item := events.AsValue(ev).(type) {
		case events.ModelRequest:
			writePartText(&builder, item.Parts)
		case events.DrainedModelRequest:
			writePartText(&builder, item.Parts)
		case events.ModelResponse:
			if content := item.Content(); content != "" {
				builder.WriteString(content)
				builder.WriteString("\n")
			}
		case events.ToolResultEvent:
			writePartText(&builder, item.Result.Parts)
		case events.ToolResultsEvent:
			for _, result := range item.Results {
				writePartText(&builder, result.Result.Parts)
			}
		}
	}
	return builder.String()
}

func writePartText(builder *strings.Builder, parts []events.Input) {
	for _, part := range parts {
		if text, ok := events.AsValue(part).(events.TextInput); ok {
			builder.WriteString(text.Content)
			builder.WriteString("\n")
		}
	}
}

func sessionFrom(cx *agocontext.Context) *tools.Session {
	if cx == nil {
		return nil
	}
	value, ok := cx.Dependency(sessionDepKey)
	if !ok {
		return nil
	}
	session, _ := value.(*tools.Session)
	return session
}

// transcriptMW пишет транскрипт кампании: сообщение игрока, ответ DM и
// значимые броски/бой. Автосейв боя отдельным хуком не нужен — боевые тулы
// сами пишут HP и снимок в SQLite.
//
// Дедуп: id записи детерминирован от хода (времени запроса) и id вызова тула,
// поэтому повторная обработка того же хода не плодит строки в БД.
type transcriptMW struct {
	middleware.Base
	store *store.Store
	log   *slog.Logger
	turn  string
}

var actionLabels = map[string]string{
	"roll_dice":   "Бросок костей",
	"skill_check": "Проверка навыка",
	"combat":      "Бой",
	"initiative":  "Инициатива",
}

func (m *transcriptMW) OnTurn(next middleware.TurnFunc, ev *events.ModelRequest, cx *agocontext.Context) (*events.ModelResponse, error) {
	m.turn = turnKey(ev)
	session := sessionFrom(cx)
	if text := requestText(ev); text != "" {
		author := ""
		if session != nil {
			author = session.Username
			if author == "" {
				author = session.UserID
			}
		}
		m.appendEntry(session, "player", author, text, m.turn+":player")
	}
	response, err := next(ev, cx)
	if err == nil && response != nil {
		if text := strings.TrimSpace(response.Content()); text != "" {
			m.appendEntry(session, "dm", "", text, m.turn+":dm")
		}
	}
	return response, err
}

func (m *transcriptMW) OnToolExecution(next middleware.ToolExecFunc, ev *events.ToolCallEvent, cx *agocontext.Context) (events.Event, error) {
	result, err := next(ev, cx)
	if err != nil {
		return result, err
	}
	label, ok := actionLabels[ev.Name]
	if !ok {
		return result, nil
	}
	if _, failed := events.AsValue(result).(events.ToolErrorEvent); failed {
		return result, nil
	}
	text := toolResultText(result)
	if text == "" {
		return result, nil
	}
	if ev.Name == "initiative" {
		text = strings.ReplaceAll(text, "\n", "; ")
	}
	callID := ev.ID
	if callID == "" {
		callID = shortHash(ev.Name + "\x00" + ev.Arguments + "\x00" + text)
	}
	m.appendEntry(sessionFrom(cx), "action", "", label+": "+text, m.turn+":tool:"+callID)
	return result, nil
}

// turnKey — стабильный ключ хода. Base у ModelRequest не имеет id, но несёт
// время создания: при повторной обработке того же запроса оно совпадает.
func turnKey(ev *events.ModelRequest) string {
	if ev != nil && !ev.At().IsZero() {
		return "turn:" + strconv.FormatInt(ev.At().UnixNano(), 10)
	}
	return "turn:hash:" + shortHash(requestText(ev))
}

func (m *transcriptMW) appendEntry(session *tools.Session, kind, author, text, eventID string) {
	if m.store == nil || session == nil || strings.TrimSpace(text) == "" {
		return
	}
	key := session.CampaignID
	if key == "" {
		key = session.CampaignSlug
	}
	if key == "" {
		return
	}
	campaign, err := m.store.GetCampaign(key)
	if err != nil || campaign == nil {
		return
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	entry := store.TranscriptEntry{Kind: kind, Text: text}
	if author != "" {
		entry.Author = &author
	}
	entry.EventID = &eventID
	if err := m.store.AppendTranscriptEntry(campaign.Slug, day, entry); err != nil {
		m.log.Warn("dm: transcript append failed", "error", err)
	}
}

func requestText(ev *events.ModelRequest) string {
	if ev == nil {
		return ""
	}
	var builder strings.Builder
	for _, part := range ev.Parts {
		if text, ok := events.AsValue(part).(events.TextInput); ok {
			builder.WriteString(text.Content)
			builder.WriteString("\n")
		}
	}
	return strings.TrimSpace(builder.String())
}

func toolResultText(result events.Event) string {
	var parts []events.Input
	switch item := events.AsValue(result).(type) {
	case events.ToolResultEvent:
		parts = item.Result.Parts
	default:
		return ""
	}
	var builder strings.Builder
	for _, part := range parts {
		if text, ok := events.AsValue(part).(events.TextInput); ok {
			builder.WriteString(text.Content)
			builder.WriteString("\n")
		}
	}
	return strings.TrimSpace(builder.String())
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:20]
}
