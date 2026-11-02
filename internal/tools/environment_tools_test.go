package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

type fakeDayCloser struct {
	called     bool
	campaignID string
	closedDay  int
}

func (f *fakeDayCloser) CloseDay(_ context.Context, campaign *store.Campaign, closedDay int) error {
	f.called = true
	f.campaignID = campaign.ID
	f.closedDay = closedDay
	return nil
}

func TestAdvanceDayAndDayCloser(t *testing.T) {
	env := newTestEnv(t)
	closer := &fakeDayCloser{}
	env.deps.DayCloser = closer

	result, err := env.deps.advanceDay(env.player, advanceDayArgs{Note: ptr("ночёвка")})
	if err != nil {
		t.Fatalf("advanceDay: %v", err)
	}
	if !closer.called || closer.closedDay != 1 || closer.campaignID != env.campaign.ID {
		t.Fatalf("closer = %+v", closer)
	}
	payload := result.(map[string]any)
	if payload["previousDay"] != 1 || payload["day"] != 2 {
		t.Fatalf("result = %+v", payload)
	}
	updated, _ := env.store.GetCampaign(env.campaign.ID)
	if updated.CurrentDay == nil || *updated.CurrentDay != 2 || updated.TimeOfDay == nil || *updated.TimeOfDay != store.TimeMorning {
		t.Fatalf("campaign = %+v", updated)
	}
	day, _ := env.store.ReadDay(env.campaign.Slug, 2)
	if day == nil || day.Note == nil || *day.Note != "ночёвка" {
		t.Fatalf("day = %+v", day)
	}

	// Без closer тул подсказывает вызвать летописца.
	env.deps.DayCloser = nil
	result, err = env.deps.advanceDay(env.player, advanceDayArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.(map[string]any)["note"].(string), "chronicler") {
		t.Fatalf("note = %v", result)
	}
}

func TestAdvanceDayForbiddenForOutsider(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.deps.advanceDay(env.outsider, advanceDayArgs{})
	wantToolCode(t, err, CodeAccessDenied)
}

func TestAdvanceTimeAndWeather(t *testing.T) {
	env := newTestEnv(t)

	first, err := env.deps.advanceTime(env.player, advanceTimeArgs{})
	if err != nil {
		t.Fatalf("advanceTime: %v", err)
	}
	if first.(map[string]any)["timeOfDay"] != "day" {
		t.Fatalf("first = %+v", first)
	}
	for i := 0; i < 2; i++ {
		if _, err := env.deps.advanceTime(env.player, advanceTimeArgs{}); err != nil {
			t.Fatal(err)
		}
	}
	wrapped, err := env.deps.advanceTime(env.player, advanceTimeArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if wrapped.(map[string]any)["timeOfDay"] != "morning" || !strings.Contains(wrapped.(map[string]any)["note"].(string), "advance_day") {
		t.Fatalf("wrapped = %+v", wrapped)
	}
	if _, err := env.deps.advanceTime(env.player, advanceTimeArgs{TimeOfDay: ptr("noon")}); err == nil {
		t.Fatal("noon должен отклоняться")
	}

	if _, err := env.deps.setWeather(env.player, setWeatherArgs{Weather: "туман"}); err != nil {
		t.Fatalf("setWeather: %v", err)
	}
	updated, _ := env.store.GetCampaign(env.campaign.ID)
	if updated.Weather == nil || *updated.Weather != "туман" {
		t.Fatalf("weather = %+v", updated.Weather)
	}
}

func TestGetGameContextForMember(t *testing.T) {
	env := newTestEnv(t)
	result, err := env.deps.getGameContext(env.player)
	if err != nil {
		t.Fatalf("getGameContext: %v", err)
	}
	payload := result.(map[string]any)
	campaign := payload["campaign"].(map[string]any)
	if campaign["slug"] != env.campaign.Slug {
		t.Fatalf("campaign = %+v", campaign)
	}
	characters := payload["characters"].([]map[string]any)
	if len(characters) != 1 || characters[0]["name"] != "Ария" {
		t.Fatalf("characters = %+v", characters)
	}
}
