package store

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// Отказ FTS больше не глотается: catch-up по источнику продолжает работать
// сканом, но в лог попадает предупреждение (находка слайса 3).
func TestFTSFailureIsLogged(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	var log bytes.Buffer
	st.SetLogger(slog.New(slog.NewTextHandler(&log, nil)))

	campaign, err := st.CreateCampaign(NewCampaignInput{
		Title: "Log", Length: LengthShort, Setting: "s", Theme: "t",
	}, NewOwnerInput{UserID: "u1"})
	if err != nil {
		t.Fatalf("campaign: %v", err)
	}
	text := "следы борьбы у колодца"
	if err := st.AppendTranscriptEntry(campaign.Slug, 1, TranscriptEntry{Kind: "dm", Text: text}); err != nil {
		t.Fatalf("transcript: %v", err)
	}
	// Первый поиск создаёт индекс и catch-up для всех источников.
	if _, err := st.SearchCampaignMemory(campaign.Slug, "следы", SearchOptions{Limit: 5}); err != nil {
		t.Fatalf("search: %v", err)
	}
	// Ломаем один FTS-источник: catch-up обязан пожаловаться в лог, а поиск —
	// продолжить работу (остальные источники и JS-скан).
	if _, err := st.db.Exec("DROP TABLE key_events"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := st.SearchCampaignMemory(campaign.Slug, "следы", SearchOptions{Limit: 5}); err != nil {
		t.Fatalf("search после сбоя: %v", err)
	}
	if !strings.Contains(log.String(), "FTS catch-up failed") {
		t.Fatalf("в логе нет предупреждения о сбое FTS: %q", log.String())
	}
	if !strings.Contains(log.String(), "key_events") {
		t.Fatalf("в логе не назван источник: %q", log.String())
	}
}
