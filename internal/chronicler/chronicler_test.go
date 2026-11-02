package chronicler_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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
