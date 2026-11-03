package chronicler_test

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	agoagent "github.com/luckystrker/ago/agent"
	"github.com/luckystrker/ago/events"
	agotest "github.com/luckystrker/ago/testing"

	"github.com/luckystriker/ai-dnd-system/internal/chronicler"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func call(id, name, args string) *events.ModelResponse {
	return &events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{
		{ID: id, Name: name, Arguments: args},
	}}}
}

func reply(text string) *events.ModelResponse {
	return &events.ModelResponse{Message: &events.ModelMessage{Content: text}}
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "campaigns.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func newCampaign(t *testing.T, st *store.Store, title string) *store.Campaign {
	t.Helper()
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: title, Length: store.LengthShort, Setting: "s", Theme: "t",
	}, store.NewOwnerInput{UserID: "dm-1"})
	if err != nil {
		t.Fatalf("campaign: %v", err)
	}
	return campaign
}

// readDayRounds собирает скрипт из n раундов read_day и финального ответа:
// так проверяется именно лимит раундов, без побочных записей в память.
func readDayRounds(n int) []*events.ModelResponse {
	script := make([]*events.ModelResponse, 0, n+1)
	for index := 0; index < n; index++ {
		script = append(script, call("c"+strconv.Itoa(index), "read_day", `{"day":1}`))
	}
	return append(script, reply("Готово."))
}

// У летописца свой лимит раундов: значение из конфига, а не MAX_TOOL_ROUNDS DM
// (PLAN 44).
func TestChroniclerUsesConfiguredToolRoundLimit(t *testing.T) {
	t.Parallel()

	st := newStore(t)
	campaign := newCampaign(t, st, "Хроника")
	cfg := agotest.NewTestConfig(readDayRounds(3)...)
	agent, err := chronicler.New(chronicler.Config{
		Model: cfg, Store: st, CampaignID: campaign.ID, MaxToolRounds: 2,
	})
	if err != nil {
		t.Fatalf("chronicler: %v", err)
	}
	_, err = agent.CloseDay(context.Background(), campaign, 1, "")
	if !errors.Is(err, agoagent.ErrMaxToolRounds) {
		t.Fatalf("ошибка = %v, want ErrMaxToolRounds", err)
	}
}

// По умолчанию летописец получает 24 раунда: 24 проходят, 25-й упирается в
// лимит.
func TestChroniclerDefaultToolRoundLimit(t *testing.T) {
	t.Parallel()

	const wantDefault = 24
	st := newStore(t)
	campaign := newCampaign(t, st, "Хроника")
	okAgent, err := chronicler.New(chronicler.Config{
		Model: agotest.NewTestConfig(readDayRounds(wantDefault)...),
		Store: st, CampaignID: campaign.ID,
	})
	if err != nil {
		t.Fatalf("chronicler: %v", err)
	}
	if _, err := okAgent.CloseDay(context.Background(), campaign, 1, ""); err != nil {
		t.Fatalf("24 раунда должны проходить: %v", err)
	}

	overAgent, err := chronicler.New(chronicler.Config{
		Model: agotest.NewTestConfig(readDayRounds(wantDefault + 1)...),
		Store: st, CampaignID: campaign.ID,
	})
	if err != nil {
		t.Fatalf("chronicler: %v", err)
	}
	if _, err := overAgent.CloseDay(context.Background(), campaign, 1, ""); !errors.Is(err, agoagent.ErrMaxToolRounds) {
		t.Fatalf("25-й раунд: ошибка = %v, want ErrMaxToolRounds", err)
	}
}

// Счётчики отчёта считаются по изменениям в БД, а не по словам модели.
func TestCloseDayCountsDatabaseChanges(t *testing.T) {
	t.Parallel()

	st := newStore(t)
	campaign := newCampaign(t, st, "Хроника")
	if err := st.AppendTranscriptEntry(campaign.Slug, 1, store.TranscriptEntry{
		Kind: "dm", Text: "Партия вошла в трактир.",
	}); err != nil {
		t.Fatalf("transcript: %v", err)
	}

	cfg := agotest.NewTestConfig(
		call("c1", "update_day_summary", `{"day":1,"summary":"Партия вошла в трактир и познакомилась с Мартой.","headline":"Трактир «Три ворона»"}`),
		call("c2", "append_key_event", `{"event":"Марта согласилась помочь партии.","day":1}`),
		call("c3", "append_campaign_summary", `{"day":1,"text":"День 1: знакомство в трактире."}`),
		call("c4", "upsert_npc", `{"name":"Марта","role":"трактирщица","memoryAppend":"Партия заплатила за ночлег.","memoryAppendDay":1}`),
		reply("Готово."),
	)
	agent, err := chronicler.New(chronicler.Config{Model: cfg, Store: st, CampaignID: campaign.ID})
	if err != nil {
		t.Fatalf("chronicler: %v", err)
	}
	report, err := agent.CloseDay(context.Background(), campaign, 1, "")
	if err != nil {
		t.Fatalf("CloseDay: %v", err)
	}
	if !report.DaySummaryWritten {
		t.Error("DaySummaryWritten = false, want true")
	}
	if report.KeyEventsWritten != 1 {
		t.Errorf("KeyEventsWritten = %d, want 1", report.KeyEventsWritten)
	}
	if report.NpcsUpdated != 1 {
		t.Errorf("NpcsUpdated = %d, want 1", report.NpcsUpdated)
	}
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if day.Summary == nil || day.Headline == nil {
		t.Fatalf("саммари/шапка не записаны: %+v", day)
	}
}

// Пустой прогон не выдумывает счётчики, даже если модель утверждает обратное.
func TestCloseDayNoChangesReportsZero(t *testing.T) {
	t.Parallel()

	st := newStore(t)
	campaign := newCampaign(t, st, "Пусто")
	cfg := agotest.NewTestConfig(reply("Записал 5 событий, 3 NPC и саммари."))
	agent, err := chronicler.New(chronicler.Config{Model: cfg, Store: st, CampaignID: campaign.ID})
	if err != nil {
		t.Fatalf("chronicler: %v", err)
	}
	report, err := agent.CloseDay(context.Background(), campaign, 1, "")
	if err != nil {
		t.Fatalf("CloseDay: %v", err)
	}
	if report.DaySummaryWritten || report.KeyEventsWritten != 0 || report.NpcsUpdated != 0 {
		t.Fatalf("счётчики выдуманы: %+v", report)
	}
}

// Кампания задана кодом: Trusted-сессия летописца не может писать в другую
// кампанию, даже если модель просит.
func TestCloseDayWritesOnlyItsCampaign(t *testing.T) {
	t.Parallel()

	st := newStore(t)
	own := newCampaign(t, st, "Своя")
	other := newCampaign(t, st, "Чужая")
	cfg := agotest.NewTestConfig(
		call("c1", "update_day_summary", `{"day":1,"summary":"Саммари своей кампании","headline":"Своя"}`),
		reply("Готово."),
	)
	agent, err := chronicler.New(chronicler.Config{Model: cfg, Store: st, CampaignID: own.ID})
	if err != nil {
		t.Fatalf("chronicler: %v", err)
	}
	if _, err := agent.CloseDay(context.Background(), own, 1, ""); err != nil {
		t.Fatalf("CloseDay: %v", err)
	}
	ownDay, err := st.ReadDay(own.Slug, 1)
	if err != nil || ownDay == nil || ownDay.Summary == nil {
		t.Fatalf("своя кампания не закрыта: %+v, %v", ownDay, err)
	}
	otherDay, err := st.ReadDay(other.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay other: %v", err)
	}
	if otherDay != nil && otherDay.Summary != nil {
		t.Fatalf("летописец записал в чужую кампанию: %+v", otherDay)
	}
	if agent.Session().Trusted != true {
		t.Fatal("сессия летописца должна быть Trusted")
	}
}

// Повторное закрытие дня не выдумывает ключевые события из ниоткуда.
func TestCloseDaySecondRunDoesNotDoubleCount(t *testing.T) {
	t.Parallel()

	st := newStore(t)
	campaign := newCampaign(t, st, "Повтор")
	if err := st.SetDaySummary(campaign.Slug, 1, "уже закрыт"); err != nil {
		t.Fatalf("SetDaySummary: %v", err)
	}
	cfg := agotest.NewTestConfig(reply("Нечего делать."))
	agent, err := chronicler.New(chronicler.Config{Model: cfg, Store: st, CampaignID: campaign.ID})
	if err != nil {
		t.Fatalf("chronicler: %v", err)
	}
	report, err := agent.CloseDay(context.Background(), campaign, 1, "")
	if err != nil {
		t.Fatalf("CloseDay: %v", err)
	}
	if report.DaySummaryWritten {
		t.Fatal("саммари уже было — DaySummaryWritten должен быть false")
	}
	if strings.TrimSpace(report.Note) == "" {
		t.Fatal("note не заполнен из ответа модели")
	}
}

// Летописец не закрывает чужую кампанию, даже если её передали напрямую.
func TestCloseDayRejectsForeignCampaign(t *testing.T) {
	t.Parallel()

	st := newStore(t)
	own := newCampaign(t, st, "Своя")
	other := newCampaign(t, st, "Чужая")
	cfg := agotest.NewTestConfig(reply("не должно дойти"))
	agent, err := chronicler.New(chronicler.Config{Model: cfg, Store: st, CampaignID: own.ID})
	if err != nil {
		t.Fatalf("chronicler: %v", err)
	}
	if _, err := agent.CloseDay(context.Background(), other, 1, ""); err == nil {
		t.Fatal("ожидался отказ для чужой кампании")
	}
}

// Ключевые события чужого дня (параллельный ход DM в N+1) не засчитываются
// летописцу закрываемого дня.
func TestCloseDayCountsKeyEventsOnlyForClosedDay(t *testing.T) {
	t.Parallel()

	st := newStore(t)
	campaign := newCampaign(t, st, "Дни")
	if err := st.AppendKeyEvent(campaign.Slug, 2, "Событие следующего дня.", nil, false); err != nil {
		t.Fatalf("AppendKeyEvent: %v", err)
	}
	cfg := agotest.NewTestConfig(
		call("c1", "append_key_event", `{"event":"Событие закрываемого дня.","day":2}`),
		reply("Готово."),
	)
	agent, err := chronicler.New(chronicler.Config{Model: cfg, Store: st, CampaignID: campaign.ID})
	if err != nil {
		t.Fatalf("chronicler: %v", err)
	}
	report, err := agent.CloseDay(context.Background(), campaign, 1, "")
	if err != nil {
		t.Fatalf("CloseDay: %v", err)
	}
	if report.KeyEventsWritten != 0 {
		t.Fatalf("чужой день засчитан: %+v", report)
	}
}
