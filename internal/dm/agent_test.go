package dm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agoconfig "github.com/luckystrker/ago/config"
	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/middleware"
	"github.com/luckystrker/ago/stream"
	agotest "github.com/luckystrker/ago/testing"
	"github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/store"
	ttrpg "github.com/luckystriker/ai-dnd-system/internal/tools"
)

func strPtr(value string) *string { return &value }
func intPtr(value int) *int       { return &value }

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "campaigns.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func testCampaign(t *testing.T, st *store.Store) *store.Campaign {
	t.Helper()
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Три ворона", Length: store.LengthShort,
		Setting: "Туманный север", Theme: "выживание", Tone: strPtr("сумрачный"),
	}, store.NewOwnerInput{UserID: "dm-1", Username: strPtr("dm")})
	if err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}
	if _, err := st.BindAndActivate(campaign.ID, "dm-1", store.BoundChat{ChatID: "-100"}); err != nil {
		t.Fatalf("BindAndActivate: %v", err)
	}
	if _, err := st.AddMember(campaign.ID, "dm-1", store.NewMemberInput{
		UserID: "player-1", Username: strPtr("den"),
	}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if _, err := st.SaveCharacter(campaign.ID, "player-1", store.NewCharacterInput{
		Name: "Дэн", CharacterClass: "воин", Race: "человек",
		HP: intPtr(12), MaxHP: intPtr(12),
	}); err != nil {
		t.Fatalf("SaveCharacter: %v", err)
	}
	return campaign
}

func playerSession(campaign *store.Campaign) *ttrpg.Session {
	return &ttrpg.Session{
		CampaignID: campaign.ID, CampaignSlug: campaign.Slug,
		UserID: "player-1", Username: "den", Name: "Ден",
		ChatID: "-100", ChatType: "supergroup",
	}
}

func plain(text string) *events.ModelResponse {
	return &events.ModelResponse{Message: &events.ModelMessage{Content: text}}
}

func toolCall(id, name, args string) *events.ModelResponse {
	return &events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{
		{ID: id, Name: name, Arguments: args},
	}}}
}

// promptRecorder записывает системный префикс каждого вызова модели.
type promptRecorder struct {
	inner   *agotest.TestConfig
	mu      sync.Mutex
	seen    [][]string
	schemas [][]string
}

func newPromptRecorder(inner *agotest.TestConfig) *promptRecorder {
	return &promptRecorder{inner: inner}
}

func (p *promptRecorder) Provider() agoconfig.Provider { return p.inner.Provider() }
func (p *promptRecorder) Model() string                { return p.inner.Model() }
func (p *promptRecorder) Create() (agoconfig.LLMClient, error) {
	return p, nil
}
func (p *promptRecorder) Clone(overrides map[string]any) (agoconfig.ModelConfig, error) {
	clone, err := p.inner.Clone(overrides)
	if err != nil {
		return nil, err
	}
	return newPromptRecorder(clone.(*agotest.TestConfig)), nil
}
func (p *promptRecorder) Call(ctx context.Context, msgs []events.Event, cx *agocontext.Context, opts agoconfig.CallOptions) (*events.ModelResponse, error) {
	var prompt []string
	if cx != nil {
		prompt = append(prompt, cx.PromptLines()...)
	}
	names := make([]string, 0, len(opts.Tools))
	for _, tool := range opts.Tools {
		if schema, ok := tool.(tools.Schema); ok {
			names = append(names, schema.Name)
		}
	}
	p.mu.Lock()
	p.seen = append(p.seen, prompt)
	p.schemas = append(p.schemas, names)
	p.mu.Unlock()
	return p.inner.Call(ctx, msgs, cx, opts)
}
func (p *promptRecorder) Prompts() [][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]string, len(p.seen))
	copy(out, p.seen)
	return out
}

func (p *promptRecorder) Schemas() [][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]string, len(p.schemas))
	copy(out, p.schemas)
	return out
}

func memoryBlocks(snapshot []events.Event) []string {
	blocks := []string{}
	for _, ev := range snapshot {
		request, ok := events.AsValue(ev).(events.ModelRequest)
		if !ok || request.Metadata["kind"] != "campaign_memory" {
			continue
		}
		for _, part := range request.Parts {
			if text, ok := events.AsValue(part).(events.TextInput); ok {
				blocks = append(blocks, text.Content)
			}
		}
	}
	return blocks
}

func lastRequestIndex(snapshot []events.Event) int {
	for i := len(snapshot) - 1; i >= 0; i-- {
		switch events.AsValue(snapshot[i]).(type) {
		case events.ModelRequest, events.DrainedModelRequest:
			return i
		}
	}
	return -1
}

// Системный префикс не меняется между ходами и не содержит память; блок
// памяти стоит в хвосте — прямо перед последним сообщением игрока.
func TestSystemPromptStableAndMemoryInTail(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	cfg := agotest.NewTestConfig(plain("Первый ответ"), plain("Второй ответ"))
	recorder := newPromptRecorder(cfg)
	d, err := New(Config{Model: recorder, Store: st, Session: playerSession(campaign)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()

	if _, err := d.Ask(context.Background(), "Иду к двери"); err != nil {
		t.Fatalf("Ask 1: %v", err)
	}
	if _, err := d.Ask(context.Background(), "Открываю дверь"); err != nil {
		t.Fatalf("Ask 2: %v", err)
	}

	prompts := recorder.Prompts()
	if len(prompts) != 2 {
		t.Fatalf("вызовов модели = %d, want 2", len(prompts))
	}
	if strings.Join(prompts[0], "\n") != strings.Join(prompts[1], "\n") {
		t.Fatalf("системный префикс изменился между ходами:\n1: %v\n2: %v", prompts[0], prompts[1])
	}
	for _, prompt := range prompts {
		if strings.Contains(strings.Join(prompt, "\n"), "Память кампании") {
			t.Fatal("блок памяти попал в системный промпт")
		}
	}
	schemas := recorder.Schemas()
	if len(schemas) != 2 || strings.Join(schemas[0], ",") != strings.Join(schemas[1], ",") {
		t.Fatalf("схемы тулов меняются между ходами: %v", schemas)
	}
	if len(schemas[0]) == 0 {
		t.Fatal("в вызове модели нет схем тулов")
	}

	calls := cfg.Calls()
	if len(calls) != 2 {
		t.Fatalf("снапшотов = %d, want 2", len(calls))
	}
	for index, snapshot := range calls {
		blocks := memoryBlocks(snapshot)
		if len(blocks) != 1 {
			t.Fatalf("вызов %d: блоков памяти = %d, want 1", index+1, len(blocks))
		}
		if !strings.Contains(blocks[0], "Память кампании «Три ворона»") {
			t.Fatalf("вызов %d: нет заголовка памяти", index+1)
		}
		if len(memoryBlocks(snapshot)) != 1 {
			t.Fatalf("вызов %d: блоков памяти = %d, want 1", index+1, len(memoryBlocks(snapshot)))
		}
		last := lastRequestIndex(snapshot)
		if last < 1 {
			t.Fatalf("вызов %d: в истории нет запроса игрока", index+1)
		}
		before, ok := events.AsValue(snapshot[last-1]).(events.ModelRequest)
		if !ok || before.Metadata["kind"] != "campaign_memory" {
			t.Fatalf("вызов %d: перед последним запросом нет блока памяти", index+1)
		}
		if got := memoryBlocks(snapshot[last+1:]); len(got) != 0 {
			t.Fatalf("вызов %d: блок памяти после запроса игрока", index+1)
		}
	}

	// Строки транскрипта, уже видимые в окне истории, в блоке не повторяются.
	second := memoryBlocks(calls[1])[0]
	if strings.Contains(second, "Иду к двери") || strings.Contains(second, "Первый ответ") {
		t.Fatalf("блок памяти дублирует историю: %s", second)
	}
}

// Строка транскрипта, которой нет в окне истории, в блоке остаётся.
func TestMemoryBlockKeepsTailMissingFromHistory(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	if err := st.AppendTranscriptEntry(campaign.Slug, 1, store.TranscriptEntry{
		Kind: "dm", Text: "Старое событие дня: партия нашла карту.",
	}); err != nil {
		t.Fatalf("AppendTranscriptEntry: %v", err)
	}
	cfg := agotest.NewTestConfig(plain("Ответ"))
	d, err := New(Config{Model: cfg, Store: st, Session: playerSession(campaign)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "Что на карте?"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	blocks := memoryBlocks(cfg.Calls()[0])
	if len(blocks) != 1 || !strings.Contains(blocks[0], "партия нашла карту") {
		t.Fatalf("старая строка дня потерялась: %v", blocks)
	}
}

// ErrMaxToolRounds превращается в нейтральный ответ без диагностики, а
// записанное тулами не откатывается.
func TestMaxToolRoundsReturnsNeutralReply(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	var calls int32
	var mu sync.Mutex
	tick, err := tools.NewFunctionTool("tick", "Ticks", func(args struct{}) (string, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		_ = st.AppendKeyEvent(campaign.Slug, 1, fmt.Sprintf("Тик %d", n), nil, false)
		return "tock", nil
	})
	if err != nil {
		t.Fatalf("tool: %v", err)
	}
	cfg := agotest.NewTestConfig(
		toolCall("c1", "tick", `{}`),
		toolCall("c2", "tick", `{}`),
		toolCall("c3", "tick", `{}`),
		plain("после сбоя"),
	)
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		SkipToolkit: true, ExtraTools: []tools.Tool{tick}, MaxToolRounds: 2,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()

	reply, err := d.Ask(context.Background(), "играем")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != NeutralFailureReply {
		t.Fatalf("ответ игроку = %q, want %q", reply, NeutralFailureReply)
	}
	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 2 {
		t.Fatalf("вызовов тула = %d, want 2", got)
	}
	permanent, regular, err := st.SplitKeyEvents(campaign.Slug)
	if err != nil {
		t.Fatalf("SplitKeyEvents: %v", err)
	}
	events := strings.Join(append(permanent, regular...), "\n")
	if len(permanent)+len(regular) != 2 || !strings.Contains(events, "Тик 1") || !strings.Contains(events, "Тик 2") {
		t.Fatalf("записи тулов откатились: %v / %v", permanent, regular)
	}

	// Следующий ход после терминального сбоя должен снова работать:
	// ago чинит историю (CloseUnanswered), а middleware не ломается.
	second, err := d.Ask(context.Background(), "продолжаем")
	if err != nil {
		t.Fatalf("Ask после сбоя: %v", err)
	}
	if second != "после сбоя" {
		t.Fatalf("ответ после сбоя = %q", second)
	}
	if got := len(cfg.Calls()); got != 4 {
		t.Fatalf("вызовов модели = %d, want 4", got)
	}
}

// Ошибка модели (скрипт исчерпан) — тоже нейтральный ответ.
func TestLLMFailureReturnsNeutralReply(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	d, err := New(Config{
		Model: agotest.NewTestConfig(), Store: st, Session: playerSession(campaign),
		SkipToolkit: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	reply, err := d.Ask(context.Background(), "играем")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != NeutralFailureReply {
		t.Fatalf("ответ = %q", reply)
	}
}

// deadlineConfig — клиент, который висит до отмены контекста.
type deadlineConfig struct{}

func (deadlineConfig) Provider() agoconfig.Provider { return agotest.ProviderTest }
func (deadlineConfig) Model() string                { return "deadline" }
func (deadlineConfig) Create() (agoconfig.LLMClient, error) {
	return deadlineConfig{}, nil
}
func (deadlineConfig) Clone(map[string]any) (agoconfig.ModelConfig, error) {
	return deadlineConfig{}, nil
}
func (deadlineConfig) Call(ctx context.Context, _ []events.Event, _ *agocontext.Context, _ agoconfig.CallOptions) (*events.ModelResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// Дедлайн хода обрывает вызов модели и даёт игроку нейтральный ответ.
func TestTurnDeadlineReturnsNeutralReply(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	d, err := New(Config{
		Model: deadlineConfig{}, Store: st, Session: playerSession(campaign),
		SkipToolkit: true, TurnTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	reply, err := d.Ask(context.Background(), "играем")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != NeutralFailureReply {
		t.Fatalf("ответ = %q", reply)
	}
}

// Транскрипт пишет сообщение игрока, ответ DM и значимый бросок.
func TestTranscriptRecordsPlayerDMAndRolls(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	roll, err := tools.NewFunctionTool("roll_dice", "Rolls", func(args struct {
		Notation string `json:"notation"`
	}) (string, error) {
		return "🎲 Бросок 1d20: [14] = 14. Итого: 14", nil
	})
	if err != nil {
		t.Fatalf("roll tool: %v", err)
	}
	cfg := agotest.NewTestConfig(
		toolCall("roll-1", "roll_dice", `{"notation":"1d20"}`),
		plain("Дэн замечает следы."),
	)
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		SkipToolkit: true, ExtraTools: []tools.Tool{roll},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "Осматриваю пол"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	joined := strings.Join(day.Entries, "\n")
	for _, want := range []string{"Осматриваю пол", "Дэн замечает следы", "Бросок костей:", "Итого: 14"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("в транскрипте нет %q:\n%s", want, joined)
		}
	}
}

func TestTranscriptFallsBackToNameWhenUsernameMissing(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	session := playerSession(campaign)
	session.Username = ""
	session.Name = "Ден"
	cx := agocontext.NewWithDeps(stream.NewMemoryStream(), map[string]any{sessionDepKey: session}, nil)
	mw := &transcriptMW{store: st}
	request := &events.ModelRequest{
		Base:  events.Base{CreatedAt: time.Unix(1_700_000_000, 0)},
		Parts: []events.Input{events.TextInput{Content: "Открываю дверь"}},
	}
	next := func(*events.ModelRequest, *agocontext.Context) (*events.ModelResponse, error) {
		return plain("Дверь со скрипом открывается."), nil
	}
	if _, err := mw.OnTurn(next, request, cx); err != nil {
		t.Fatalf("OnTurn: %v", err)
	}
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if joined := strings.Join(day.Entries, "\n"); !strings.Contains(joined, "Игрок @Ден") {
		t.Fatalf("в транскрипте нет автора из имени: %v", day.Entries)
	}
}

// Повторная обработка того же хода не дублирует строки транскрипта.
func TestTranscriptDeduplicatesRepeatedTurn(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	session := playerSession(campaign)
	cx := agocontext.NewWithDeps(stream.NewMemoryStream(), map[string]any{sessionDepKey: session}, nil)
	mw := &transcriptMW{store: st}
	request := &events.ModelRequest{
		Base:  events.Base{CreatedAt: time.Unix(1_700_000_000, 0)},
		Parts: []events.Input{events.TextInput{Content: "Открываю дверь"}},
	}
	next := func(*events.ModelRequest, *agocontext.Context) (*events.ModelResponse, error) {
		return plain("Дверь со скрипом открывается."), nil
	}
	for i := 0; i < 2; i++ {
		if _, err := mw.OnTurn(next, request, cx); err != nil {
			t.Fatalf("OnTurn %d: %v", i+1, err)
		}
	}
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if len(day.Entries) != 2 {
		t.Fatalf("строк транскрипта = %d, want 2: %v", len(day.Entries), day.Entries)
	}
}

// Бюджет токенов: при превышении модель один раз получает просьбу свернуть
// сцену, ход не обрывается.
// Бюджет токенов — на ход: сброс в начале хода, поэтому несколько ходов
// подряд под бюджетом не оставляют вечного предупреждения.
func TestTokenBudgetResetsEveryTurn(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	usage := events.Usage{TotalTokens: events.I64(500)}
	cfg := agotest.NewTestConfig(
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Первый"}, Usage: usage},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Второй"}, Usage: usage},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Третий"}, Usage: usage},
	)
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		TokenBudget: 100,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	for i := 0; i < 3; i++ {
		if _, err := d.Ask(context.Background(), "ход"); err != nil {
			t.Fatalf("Ask %d: %v", i+1, err)
		}
	}
	for index, snapshot := range cfg.Calls() {
		blocks := memoryBlocks(snapshot)
		if len(blocks) != 1 {
			t.Fatalf("вызов %d: блоков памяти = %d, want 1", index+1, len(blocks))
		}
		if strings.Contains(blocks[0], "бюджет токенов") {
			t.Fatalf("вызов %d: предупреждение пережило сброс хода: %s", index+1, blocks[0])
		}
	}
}

// Внутри одного хода бюджет уже израсходован первым вызовом — следующий
// раунд получает просьбу свернуть сцену.
func TestTokenBudgetNoticeWithinTurn(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	usage := events.Usage{TotalTokens: events.I64(500)}
	tick, err := tools.NewFunctionTool("tick", "Ticks", func(args struct{}) (string, error) {
		return "tock", nil
	})
	if err != nil {
		t.Fatalf("tool: %v", err)
	}
	cfg := agotest.NewTestConfig(
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
			ID: "c1", Name: "tick", Arguments: `{}`,
		}}}, Usage: usage},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "Готово"}, Usage: usage},
	)
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		TokenBudget: 100, SkipToolkit: true, ExtraTools: []tools.Tool{tick},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "играем"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	calls := cfg.Calls()
	if len(calls) != 2 {
		t.Fatalf("вызовов модели = %d, want 2", len(calls))
	}
	first := memoryBlocks(calls[0])
	if len(first) == 1 && strings.Contains(first[0], "бюджет токенов") {
		t.Fatalf("предупреждение появилось до расхода бюджета: %s", first[0])
	}
	second := memoryBlocks(calls[1])
	if len(second) != 1 || !strings.Contains(second[0], "бюджет токенов") {
		t.Fatalf("во втором раунде нет предупреждения: %v", second)
	}
}

// Скиллы из embed загружаются моделью через load_skill.
func TestSkillsAreLoadable(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	cfg := agotest.NewTestConfig(
		toolCall("s1", "load_skill", `{"name":"create-campaign"}`),
		plain("Начинаем интервью."),
	)
	d, err := New(Config{Model: cfg, Store: st, Session: &ttrpg.Session{UserID: "local", ChatID: "local", ChatType: "private"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	reply, err := d.Agent().Ask(context.Background(), "/newcampaign")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	history, err := reply.History().GetEvents()
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	found := false
	for _, ev := range history {
		if result, ok := events.AsValue(ev).(events.ToolResultEvent); ok {
			for _, part := range result.Result.Parts {
				if text, ok := events.AsValue(part).(events.TextInput); ok &&
					strings.Contains(text.Content, "Campaign creation interview") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("скилл create-campaign не загрузился через load_skill")
	}
}

// Промпт соответствует чек-листу ревью слайса 4.
func TestPromptFollowsReviewChecklist(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"campaignSlug",
		"persist the sheet with `update_character`",
		"call `chronicler`",
		"session game state is authoritative",
	}
	for _, fragment := range forbidden {
		if strings.Contains(instructions, fragment) {
			t.Errorf("в промпте остался запрещённый фрагмент %q", fragment)
		}
	}
	required := []string{
		"advance_day",
		"call a chronicler yourself",
		"`code`, `retryable` and `remediation`",
		"load_skill",
		"Campaign memory",
		"`access_denied`: explain",
		"`advantage: true`",
		"`advantage: \"disadvantage\"`",
		"Take a player's word",
		"Plan-discussion replies are the exception",
		"`ask_player`",
	}
	for _, fragment := range required {
		if !strings.Contains(instructions, fragment) {
			t.Errorf("в промпте нет фрагмента %q", fragment)
		}
	}
}

// Sanity: агент вообще собирается с полным toolkit-ом.
func TestNewBuildsFullAgent(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	d, err := New(Config{Model: agotest.NewTestConfig(plain("ok")), Store: st, Session: playerSession(campaign)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if d.Agent() == nil || d.Monitor() == nil {
		t.Fatal("агент или монитор не собраны")
	}
}

// Два одинаковых сообщения игрока в разных ходах — две записи транскрипта
// (ключ хода не должен схлопываться по тексту).
func TestTranscriptKeepsIdenticalMessagesAcrossTurns(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	cfg := agotest.NewTestConfig(plain("Первый"), plain("Второй"))
	d, err := New(Config{Model: cfg, Store: st, Session: playerSession(campaign)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	for i := 0; i < 2; i++ {
		if _, err := d.Ask(context.Background(), "Жду."); err != nil {
			t.Fatalf("Ask %d: %v", i+1, err)
		}
	}
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	playerLines := 0
	for _, line := range day.Entries {
		if strings.Contains(line, "Игрок @den") && strings.Contains(line, "Жду.") {
			playerLines++
		}
	}
	if playerLines != 2 {
		t.Fatalf("строк игрока = %d, want 2:\n%s", playerLines, strings.Join(day.Entries, "\n"))
	}
}

type recordingCloser struct {
	campaignID string
	day        int
}

func (c *recordingCloser) CloseDay(_ context.Context, campaign *store.Campaign, day int) error {
	c.campaignID = campaign.ID
	c.day = day
	return nil
}

// Настоящий toolkit DM получает DayCloser из конфига: advance_day ставит
// закрытие прошлого дня в очередь, а не просит модель звать летописца.
func TestAdvanceDayThroughDMToolkitEnqueuesCloser(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	closer := &recordingCloser{}
	cfg := agotest.NewTestConfig(
		toolCall("a1", "advance_day", `{}`),
		plain("Наступило утро."),
	)
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign), DayCloser: closer,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "Ночуем в трактире."); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if closer.campaignID != campaign.ID || closer.day != 1 {
		t.Fatalf("closer = %+v, want campaign %s day 1", closer, campaign.ID)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil || updated.CurrentDay == nil || *updated.CurrentDay != 2 {
		t.Fatalf("день не сдвинут: %+v, %v", updated, err)
	}
}

// Отмена хода не подменяется нейтральным ответом: вызывающий код должен знать,
// что ответ устарел.
type cancelConfig struct{ started chan struct{} }

func (cancelConfig) Provider() agoconfig.Provider { return agotest.ProviderTest }
func (cancelConfig) Model() string                { return "cancel" }
func (c cancelConfig) Create() (agoconfig.LLMClient, error) {
	return c, nil
}
func (c cancelConfig) Clone(map[string]any) (agoconfig.ModelConfig, error) {
	return c, nil
}
func (c cancelConfig) Call(ctx context.Context, _ []events.Event, _ *agocontext.Context, _ agoconfig.CallOptions) (*events.ModelResponse, error) {
	close(c.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestTurnCancellationPropagates(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	cfg := cancelConfig{started: make(chan struct{})}
	d, err := New(Config{Model: cfg, Store: st, Session: playerSession(campaign), SkipToolkit: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		reply string
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		reply, err := d.Ask(ctx, "играем")
		done <- outcome{reply: reply, err: err}
	}()
	<-cfg.started
	cancel()
	got := <-done
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("ошибка = %v, want context.Canceled", got.err)
	}
	if got.reply != "" {
		t.Fatalf("ответ = %q, want пусто", got.reply)
	}
}

func plainWithUsage(text string, total int64) *events.ModelResponse {
	return &events.ModelResponse{
		Message: &events.ModelMessage{Content: text},
		Usage:   events.Usage{TotalTokens: events.I64(total)},
	}
}

// Размер истории считается по содержимому: Usage ответа (весь промпт вызова)
// не должен раздувать окно и запускать компакцию каждый ход.
func TestCompactionIgnoresReportedUsage(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	cfg := agotest.NewTestConfig(
		plainWithUsage("Первый", 40000),
		plainWithUsage("Второй", 40000),
		plainWithUsage("Третий", 40000),
	)
	d, err := New(Config{Model: cfg, Store: st, Session: playerSession(campaign)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	for i := 0; i < 3; i++ {
		if _, err := d.Ask(context.Background(), "ход"); err != nil {
			t.Fatalf("Ask %d: %v", i+1, err)
		}
	}
	if calls := len(cfg.Calls()); calls != 3 {
		t.Fatalf("вызовов модели = %d, want 3: компакция сработала на Usage", calls)
	}
}

func TestContentTriggerUsesContentNotUsage(t *testing.T) {
	t.Parallel()

	small := []events.Event{
		&events.ModelResponse{Message: &events.ModelMessage{Content: "ответ"}, Usage: events.Usage{TotalTokens: events.I64(40000)}},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "ответ"}, Usage: events.Usage{TotalTokens: events.I64(40000)}},
	}
	if (contentTrigger{budget: 1000}).ShouldTrigger(small, events.Usage{}) {
		t.Fatal("маленькая история с большим Usage запустила компакцию")
	}
	big := []events.Event{&events.ModelResponse{
		Message: &events.ModelMessage{Content: strings.Repeat("x", 8000)},
	}}
	if !(contentTrigger{budget: 1000}).ShouldTrigger(big, events.Usage{}) {
		t.Fatal("большое содержимое не запустило компакцию")
	}
}

func TestContentBudgetPolicyIgnoresUsage(t *testing.T) {
	t.Parallel()

	evs := make([]events.Event, 0, 12)
	for i := 0; i < 10; i++ {
		evs = append(evs, &events.ModelResponse{
			Message: &events.ModelMessage{Content: "ответ"},
			Usage:   events.Usage{TotalTokens: events.I64(40000)},
		})
	}
	_, kept, err := newContentBudgetPolicy(1000).Apply(nil, evs, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(kept) != len(evs) {
		t.Fatalf("окно выкинуло %d событий из-за Usage", len(evs)-len(kept))
	}
	evs = append(evs, &events.ModelResponse{
		Message: &events.ModelMessage{Content: strings.Repeat("y", 8000)},
	})
	_, kept, err = newContentBudgetPolicy(1000).Apply(nil, evs, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(kept) >= len(evs) {
		t.Fatal("большое содержимое не ограничено окном")
	}
}

// Короткая реплика не должна выкидываться из хвоста только потому, что она
// встречается подстрокой в другом слове истории («ок» внутри «окно»).
func TestMemoryTailKeepsShortLineWhenHistoryHasLongerWord(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	author := "hero"
	eventID := "turn:old:player"
	if err := st.AppendTranscriptEntry(campaign.Slug, 1, store.TranscriptEntry{
		Kind: "player", Author: &author, Text: "ок", EventID: &eventID,
	}); err != nil {
		t.Fatalf("AppendTranscriptEntry: %v", err)
	}
	cfg := agotest.NewTestConfig(plain("Ты видишь окно."))
	d, err := New(Config{Model: cfg, Store: st, Session: playerSession(campaign)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "Вижу окно."); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	block := memoryBlocks(cfg.Calls()[0])[0]
	if !strings.Contains(block, "**Игрок @hero**: ок") {
		t.Fatalf("короткая реплика потерялась из хвоста: %s", block)
	}
}

// Встроенные скиллы можно разложить в постоянный каталог рядом с БД.
func TestMaterializeSkills(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := MaterializeSkills(dir); err != nil {
		t.Fatalf("MaterializeSkills: %v", err)
	}
	for _, name := range []string{"create-campaign", "create-character"} {
		path := filepath.Join(dir, name, "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			t.Fatalf("скилл %s не разложен: %v", name, err)
		}
	}
}

// Настоящий toolkit: combat damage пишет HP в лист SQLite.
func TestDMToolkitCombatDamageWritesSheet(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	cfg := agotest.NewTestConfig(
		toolCall("i1", "initiative", `{"combatants":[`+
			`{"name":"Дэн","side":"party","bonus":0},`+
			`{"name":"Гоблин","side":"enemy","bonus":0,"hp":7,"ac":13}]}`),
		toolCall("c1", "combat", `{"action":"damage","target":"Дэн","amount":4}`),
		plain("Дэн получает 4 урона."),
	)
	d, err := New(Config{Model: cfg, Store: st, Session: playerSession(campaign)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "Гоблин бьёт Дэна."); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	sheets, err := st.ListCharacters(campaign.ID)
	if err != nil || len(sheets) != 1 {
		t.Fatalf("ListCharacters: %v, %v", sheets, err)
	}
	if sheets[0].HP == nil || *sheets[0].HP != 8 {
		t.Fatalf("HP = %v, want 8 (combat damage пишет лист)", sheets[0].HP)
	}
}

// Настоящий toolkit: чужой игрок получает access_denied, лист не меняется,
// а модель видит типизированную ошибку.
func TestDMToolkitDeniesOutsiderMemoryWrite(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	outsider := &ttrpg.Session{
		CampaignID: campaign.ID, CampaignSlug: campaign.Slug,
		UserID: "outsider", ChatID: "-100", ChatType: "supergroup",
	}
	cfg := agotest.NewTestConfig(
		toolCall("u1", "update_character", `{"character":"Дэн","hp":5}`),
		plain("Этот персонаж не ваш."),
	)
	d, err := New(Config{Model: cfg, Store: st, Session: outsider})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "Меняю HP Дэна."); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	sheets, err := st.ListCharacters(campaign.ID)
	if err != nil || len(sheets) != 1 {
		t.Fatalf("ListCharacters: %v, %v", sheets, err)
	}
	if sheets[0].HP == nil || *sheets[0].HP != 12 {
		t.Fatalf("чужой игрок изменил HP: %v", sheets[0].HP)
	}
	evs, err := d.stream.History().GetEvents()
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if !historyHasToolErrorCode(evs, "access_denied") {
		t.Fatalf("в истории нет access_denied: %v", evs)
	}
}

func historyHasToolErrorCode(evs []events.Event, code string) bool {
	for _, ev := range evs {
		switch item := events.AsValue(ev).(type) {
		case events.ToolErrorEvent:
			if strings.Contains(item.Error, code) {
				return true
			}
		case events.ToolResultEvent:
			for _, part := range item.Result.Parts {
				if text, ok := events.AsValue(part).(events.TextInput); ok && strings.Contains(text.Content, code) {
					return true
				}
			}
		case events.ToolResultsEvent:
			for _, result := range item.Results {
				for _, part := range result.Result.Parts {
					if text, ok := events.AsValue(part).(events.TextInput); ok && strings.Contains(text.Content, code) {
						return true
					}
				}
			}
		}
	}
	return false
}

// Паника тула не уходит игроку: ago перехватывает её, ход продолжается.
func TestToolPanicDoesNotReachPlayer(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	boom, err := tools.NewFunctionTool("boom", "Panics", func(args struct{}) (string, error) {
		panic("boom")
	})
	if err != nil {
		t.Fatalf("tool: %v", err)
	}
	cfg := agotest.NewTestConfig(
		toolCall("b1", "boom", `{}`),
		plain("Сцена продолжается."),
	)
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		SkipToolkit: true, ExtraTools: []tools.Tool{boom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	reply, err := d.Ask(context.Background(), "Пробую.")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "Сцена продолжается." {
		t.Fatalf("ответ = %q", reply)
	}
	if strings.Contains(reply, "panic") || strings.Contains(reply, "boom") {
		t.Fatalf("диагностика паники ушла игроку: %q", reply)
	}
}

type failingCompaction struct{ middleware.Base }

func (failingCompaction) OnTurn(next middleware.TurnFunc, ev *events.ModelRequest, cx *agocontext.Context) (*events.ModelResponse, error) {
	if _, err := next(ev, cx); err != nil {
		return nil, err
	}
	return nil, errors.New("compaction boom")
}

// Сбой компакции после успешного хода не отбрасывает готовый ответ.
func TestSafeCompactionKeepsTurnResult(t *testing.T) {
	t.Parallel()

	safe := &safeCompaction{inner: failingCompaction{}, log: slog.Default()}
	resp, err := safe.OnTurn(func(*events.ModelRequest, *agocontext.Context) (*events.ModelResponse, error) {
		return plain("готовый ответ"), nil
	}, &events.ModelRequest{}, nil)
	if err != nil {
		t.Fatalf("OnTurn: %v", err)
	}
	if resp == nil || resp.Content() != "готовый ответ" {
		t.Fatalf("ответ потерян: %+v", resp)
	}
}

// Сессия обновляется в начале каждого хода: один DM обслуживает чат, а
// говорящий меняется. Транскрипт должен записать обоих авторов.
func TestSessionFuncSwitchesSpeakerPerTurn(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	var current = playerSession(campaign)
	cfg := agotest.NewTestConfig(plain("Первый"), plain("Второй"))
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		SessionFunc: func() *ttrpg.Session { return current },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "ход Дэна"); err != nil {
		t.Fatalf("Ask 1: %v", err)
	}
	current = &ttrpg.Session{
		CampaignID: campaign.ID, CampaignSlug: campaign.Slug,
		UserID: "dm-1", Username: "dm", ChatID: "-100", ChatType: "supergroup",
	}
	if _, err := d.Ask(context.Background(), "ход ведущего"); err != nil {
		t.Fatalf("Ask 2: %v", err)
	}
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	joined := strings.Join(day.Entries, "\n")
	if !strings.Contains(joined, "Игрок @den") || !strings.Contains(joined, "Игрок @dm") {
		t.Fatalf("смена говорящего не попала в транскрипт:\n%s", joined)
	}
}

// История ходов на stream.Storage переживает пересборку агента: новый DM на
// том же потоке видит предыдущий разговор.
func TestDurableStreamKeepsHistoryAcrossRebuild(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	storage := store.NewEventStreamStorage(st)
	cfg := agotest.NewTestConfig(plain("Первый ответ"), plain("Второй ответ"))

	open := func() *DM {
		t.Helper()
		conversation := stream.NewMemoryStream(
			stream.WithStreamID("tg:-100:"), stream.WithStorage(storage),
		)
		d, err := New(Config{
			Model: cfg, Store: st, Session: playerSession(campaign), Stream: conversation,
			SkipToolkit: true,
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return d
	}
	first := open()
	if _, err := first.Ask(context.Background(), "Иду к двери"); err != nil {
		t.Fatalf("Ask 1: %v", err)
	}
	_ = first.Close()

	second := open()
	defer func() { _ = second.Close() }()
	history, err := second.stream.History().GetEvents()
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("durable-история не загрузилась в новый агент")
	}
	if _, err := second.Ask(context.Background(), "Открываю дверь"); err != nil {
		t.Fatalf("Ask 2: %v", err)
	}
	calls := cfg.Calls()
	if len(calls) != 2 {
		t.Fatalf("вызовов модели = %d", len(calls))
	}
	rendered := ""
	for _, snapshot := range calls[1] {
		rendered += events.RenderForPrompt(snapshot) + "\n"
	}
	if !strings.Contains(rendered, "Иду к двери") || !strings.Contains(rendered, "Первый ответ") {
		t.Fatalf("второй ход не видит предыдущий разговор:\n%s", rendered)
	}
}

// Индикатор набора вызывается на старте хода и перед вызовом тула.
func TestActivityCallbackOnTurnAndTool(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	tick, err := tools.NewFunctionTool("tick", "Ticks", func(args struct{}) (string, error) {
		return "tock", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := agotest.NewTestConfig(toolCall("c1", "tick", `{}`), plain("Готово"))
	var count int32
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		SkipToolkit: true, ExtraTools: []tools.Tool{tick},
		OnToolActivity: func() { atomic.AddInt32(&count, 1) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "играем"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got := atomic.LoadInt32(&count); got < 2 {
		t.Fatalf("индикатор вызван %d раз, want ≥2", got)
	}
}

type turnCtxKey struct{}

type turnCtxGenerator struct {
	seen context.Context
	url  string
}

func (g *turnCtxGenerator) Generate(ctx context.Context, _ string) (string, error) {
	g.seen = ctx
	return g.url, nil
}

type turnCtxPoster struct{ seen context.Context }

func (p *turnCtxPoster) PostImage(ctx context.Context, _ string, _ *int, _, _ string) error {
	p.seen = ctx
	return nil
}

// illustrate_scene получает контекст хода (дедлайн/отмену), а не
// context.Background(): иначе картинка может уйти после нейтрального ответа.
func TestIllustrateSceneGetsTurnContext(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	generator := &turnCtxGenerator{url: "https://img.example/x.png"}
	poster := &turnCtxPoster{}
	cfg := agotest.NewTestConfig(
		toolCall("i1", "illustrate_scene", `{"sceneDescription":"A dark tavern"}`),
		plain("Готово"),
	)
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		ToolDeps: ttrpg.Deps{Images: generator, Poster: poster},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	ctx := context.WithValue(context.Background(), turnCtxKey{}, "token")
	if _, err := d.Ask(ctx, "рисуй"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if generator.seen == nil || generator.seen.Value(turnCtxKey{}) != "token" {
		t.Fatalf("генератор не получил контекст хода: %v", generator.seen)
	}
	if poster.seen == nil || poster.seen.Value(turnCtxKey{}) != "token" {
		t.Fatalf("постер не получил контекст хода: %v", poster.seen)
	}
}
