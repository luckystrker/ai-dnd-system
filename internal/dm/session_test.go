package dm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"
	"github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// newSessionDM собирает DM на общем store и сессии: тесты границы сессии
// проверяют именно durable-время, а не состояние процесса.
func newSessionDM(t *testing.T, st *store.Store, campaign *store.Campaign, replies ...string) (*DM, *agotest.TestConfig) {
	t.Helper()
	responses := make([]*events.ModelResponse, 0, len(replies))
	for _, reply := range replies {
		responses = append(responses, plain(reply))
	}
	cfg := agotest.NewTestConfig(responses...)
	d, err := New(Config{Model: cfg, Store: st, Session: playerSession(campaign)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, cfg
}

// sessionMarkerBlock возвращает блок памяти первого вызова модели.
func sessionMarkerBlock(t *testing.T, cfg *agotest.TestConfig) string {
	t.Helper()
	calls := cfg.Calls()
	if len(calls) == 0 {
		t.Fatal("модель не вызывалась")
	}
	blocks := memoryBlocks(calls[0])
	if len(blocks) != 1 {
		t.Fatalf("блоков памяти = %d, want 1", len(blocks))
	}
	return blocks[0]
}

// Пауза ≥ порога — маркер начала сессии с длительностью паузы.
func TestSessionMarkerOnLongPause(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	if err := st.SetChatActivity(campaign.ID, time.Now().Add(-21*time.Hour)); err != nil {
		t.Fatalf("SetChatActivity: %v", err)
	}
	d, cfg := newSessionDM(t, st, campaign, "С возвращением.")
	if _, err := d.Ask(context.Background(), "Мы вернулись."); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	block := sessionMarkerBlock(t, cfg)
	if !strings.Contains(block, "Начало новой сессии") {
		t.Fatalf("нет маркера начала сессии:\n%s", block)
	}
	if !strings.Contains(block, "пауза 21 ч") {
		t.Fatalf("маркер не называет паузу:\n%s", block)
	}
}

// Пауза меньше порога — маркера нет: обычное продолжение сцены.
func TestNoSessionMarkerOnShortPause(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	if err := st.SetChatActivity(campaign.ID, time.Now().Add(-19*time.Hour)); err != nil {
		t.Fatalf("SetChatActivity: %v", err)
	}
	d, cfg := newSessionDM(t, st, campaign, "Продолжаем.")
	if _, err := d.Ask(context.Background(), "Иду дальше."); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if block := sessionMarkerBlock(t, cfg); strings.Contains(block, "Начало новой сессии") {
		t.Fatalf("маркер при короткой паузе:\n%s", block)
	}
}

// Рестарт процесса без паузы не даёт маркера: граница считается по SQLite,
// а не по жизненному циклу агента.
func TestNoSessionMarkerAfterRestartWithoutPause(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	if err := st.SetChatActivity(campaign.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("SetChatActivity: %v", err)
	}
	first, firstCfg := newSessionDM(t, st, campaign, "Первый ответ")
	if _, err := first.Ask(context.Background(), "Иду к двери."); err != nil {
		t.Fatalf("Ask 1: %v", err)
	}
	if block := sessionMarkerBlock(t, firstCfg); strings.Contains(block, "Начало новой сессии") {
		t.Fatalf("маркер на первом ходу без паузы:\n%s", block)
	}

	second, secondCfg := newSessionDM(t, st, campaign, "Второй ответ")
	if _, err := second.Ask(context.Background(), "Открываю дверь."); err != nil {
		t.Fatalf("Ask 2: %v", err)
	}
	if block := sessionMarkerBlock(t, secondCfg); strings.Contains(block, "Начало новой сессии") {
		t.Fatalf("маркер после рестарта без паузы:\n%s", block)
	}
}

// Первый ход в истории чата — тоже начало сессии: пересказывать нечего, но
// get_game_context и открытие сцены остаются теми же.
func TestFirstTurnInChatGetsSessionMarker(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	d, cfg := newSessionDM(t, st, campaign, "Начинаем.")
	if _, err := d.Ask(context.Background(), "Привет!"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	block := sessionMarkerBlock(t, cfg)
	if !strings.Contains(block, "Начало новой сессии") {
		t.Fatalf("нет маркера на первом ходу чата:\n%s", block)
	}
	if !strings.Contains(block, "первый ход") {
		t.Fatalf("маркер не объясняет, что ход первый:\n%s", block)
	}
	// Время активности записано: следующий ход уже не первый.
	last, ok, err := st.ChatActivity(campaign.ID)
	if err != nil || !ok || time.Since(last) > time.Minute {
		t.Fatalf("активность не записана: %v (ok=%v, err=%v)", last, ok, err)
	}
}

// Маркер держится на всех раундах тулов внутри хода.
func TestSessionMarkerHoldsAcrossToolRounds(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	if err := st.SetChatActivity(campaign.ID, time.Now().Add(-21*time.Hour)); err != nil {
		t.Fatalf("SetChatActivity: %v", err)
	}
	tick, err := tools.NewFunctionTool("tick", "Ticks", func(args struct{}) (string, error) {
		return "tock", nil
	})
	if err != nil {
		t.Fatalf("tool: %v", err)
	}
	cfg := agotest.NewTestConfig(toolCall("c1", "tick", `{}`), plain("Готово"))
	d, err := New(Config{
		Model: cfg, Store: st, Session: playerSession(campaign),
		SkipToolkit: true, ExtraTools: []tools.Tool{tick},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Ask(context.Background(), "Осматриваюсь."); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	calls := cfg.Calls()
	if len(calls) != 2 {
		t.Fatalf("вызовов модели = %d, want 2", len(calls))
	}
	for index, snapshot := range calls {
		blocks := memoryBlocks(snapshot)
		if len(blocks) != 1 || !strings.Contains(blocks[0], "Начало новой сессии") {
			t.Fatalf("раунд %d без маркера: %v", index+1, blocks)
		}
	}
}

// На следующем ходу маркер не повторяется, даже если пауза была.
func TestSessionMarkerNotRepeatedOnNextTurn(t *testing.T) {
	t.Parallel()

	st := testStore(t)
	campaign := testCampaign(t, st)
	if err := st.SetChatActivity(campaign.ID, time.Now().Add(-21*time.Hour)); err != nil {
		t.Fatalf("SetChatActivity: %v", err)
	}
	d, cfg := newSessionDM(t, st, campaign, "Первый", "Второй")
	if _, err := d.Ask(context.Background(), "Мы вернулись."); err != nil {
		t.Fatalf("Ask 1: %v", err)
	}
	if _, err := d.Ask(context.Background(), "Идём дальше."); err != nil {
		t.Fatalf("Ask 2: %v", err)
	}
	calls := cfg.Calls()
	if len(calls) != 2 {
		t.Fatalf("вызовов модели = %d, want 2", len(calls))
	}
	first := memoryBlocks(calls[0])
	if len(first) != 1 || !strings.Contains(first[0], "Начало новой сессии") {
		t.Fatalf("первый ход без маркера: %v", first)
	}
	second := memoryBlocks(calls[1])
	if len(second) != 1 || strings.Contains(second[0], "Начало новой сессии") {
		t.Fatalf("маркер повторился на втором ходу: %v", second)
	}
}

// Пауза в маркере округляется: 20 ч 59 мин — «пауза 21 ч».
func TestSessionMarkerRoundsPause(t *testing.T) {
	t.Parallel()

	marker := sessionStartMarker(20*time.Hour + 59*time.Minute)
	if !strings.Contains(marker, "пауза 21 ч") {
		t.Fatalf("пауза не округлена:\n%s", marker)
	}
}

// Промпт опирается на маркер, а не на текст сообщения игрока: правило recap
// не должно срабатывать по фразе «прошло время».
func TestPromptRecapOnlyByMarker(t *testing.T) {
	t.Parallel()

	if !strings.Contains(instructions, "Начало новой сессии") {
		t.Fatal("промпт не ссылается на маркер начала сессии")
	}
	for _, forbidden := range []string{"a real-time pause", "long time has passed"} {
		if strings.Contains(instructions, forbidden) {
			t.Fatalf("промпт по-прежнему угадывает паузу по тексту: %q", forbidden)
		}
	}
}
