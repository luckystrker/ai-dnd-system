package store

import (
	"strings"
	"testing"
)

func searchFixture(t *testing.T) (*Store, string) {
	t.Helper()
	store := newTestStore(t)
	campaign := newCampaign(t, store, "test-campaign")
	slug := campaign.Slug

	_ = store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "player", Author: ptr("hero"), Text: "Я говорю с Каэлем у часовни."})
	_ = store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "dm", Text: "Каэль обещает помочь, если принести тело Яна."})
	_ = store.AppendTranscriptEntry(slug, 3, TranscriptEntry{Kind: "dm", Text: "Каэль снимает проклятие в часовне."})
	_ = store.AppendKeyEvent(slug, 1, "Каэль дал клятву.", nil, false)
	_, _ = store.UpsertNpc(slug, NpcUpsertInput{
		Name: "Каэль", Role: ptr("Отшельник"),
		MemoryAppend: ptr("Договорился с партией."), MemoryAppendDay: ptr(1),
	})
	_, _ = store.UpsertLocation(slug, UpsertLocationInput{Name: "Часовня", Description: ptr("Разрушенная часовня на холме")})
	_, _ = store.UpsertFaction(slug, UpsertFactionInput{Name: "Гильдия купцов", Description: ptr("Торговцы из Морага")})
	return store, slug
}

func hitSources(hits []MemoryHit) []string {
	sources := make([]string, 0, len(hits))
	for _, hit := range hits {
		sources = append(sources, hit.Source)
	}
	return sources
}

func containsSource(sources []string, want string) bool {
	for _, source := range sources {
		if source == want {
			return true
		}
	}
	return false
}

func TestQueryTerms(t *testing.T) {
	terms := QueryTerms("Каэль, ЧАСОВНЯ! часовня")
	if len(terms) != 2 || terms[0] != "каэль" || terms[1] != "часовня" {
		t.Fatalf("terms = %v", terms)
	}
	if terms := QueryTerms("а у K?"); len(terms) != 0 {
		t.Fatalf("короткие слова = %v", terms)
	}
	if terms := QueryTerms("   "); len(terms) != 0 {
		t.Fatalf("пустой запрос = %v", terms)
	}
}

func TestBuildMatchQuery(t *testing.T) {
	if got := BuildMatchQuery([]string{"тело", "ян"}); got != `"тело"* AND "ян"*` {
		t.Fatalf("query = %q", got)
	}
	if got := BuildMatchQuery(nil); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestSearchFindsSources(t *testing.T) {
	store, slug := searchFixture(t)

	hits, err := store.SearchCampaignMemory(slug, "Каэль", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 4 {
		t.Fatalf("hits = %+v", hits)
	}
	sources := hitSources(hits)
	for _, want := range []string{"транскрипт, день 1", "транскрипт, день 3", "ключевое событие, день 1", "NPC Каэль"} {
		if !containsSource(sources, want) {
			t.Fatalf("нет источника %q: %v", want, sources)
		}
	}
	for _, hit := range hits {
		if hit.Snippet == "" || !strings.Contains(strings.ToLower(hit.Snippet), "каэль") {
			t.Fatalf("сниппет = %q", hit.Snippet)
		}
	}
}

func TestSearchSortsByFreshDay(t *testing.T) {
	store, slug := searchFixture(t)

	hits, err := store.SearchCampaignMemory(slug, "Каэль", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var days []int
	for _, hit := range hits {
		if hit.Day != nil {
			days = append(days, *hit.Day)
		}
	}
	if len(days) == 0 || days[0] != 3 {
		t.Fatalf("days = %v", days)
	}
	for index := 1; index < len(days); index++ {
		if days[index] > days[index-1] {
			t.Fatalf("порядок дней = %v", days)
		}
	}
}

func TestSearchANDLogic(t *testing.T) {
	store, slug := searchFixture(t)

	hits, err := store.SearchCampaignMemory(slug, "тело ян", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("нет совпадений")
	}
	found := false
	for _, hit := range hits {
		if strings.Contains(hit.Snippet, "тело") {
			found = true
		}
		if !strings.Contains(strings.ToLower(hit.Snippet), "ян") {
			t.Fatalf("сниппет без второго терма: %q", hit.Snippet)
		}
	}
	if !found {
		t.Fatalf("сниппет без «тело»: %+v", hits)
	}
}

func TestSearchDescriptions(t *testing.T) {
	store, slug := searchFixture(t)

	chapel, err := store.SearchCampaignMemory(slug, "часовня", SearchOptions{})
	if err != nil || !containsSource(hitSources(chapel), "локация Часовня") {
		t.Fatalf("chapel = %+v, %v", chapel, err)
	}
	morag, err := store.SearchCampaignMemory(slug, "мораг", SearchOptions{})
	if err != nil || !containsSource(hitSources(morag), "фракция Гильдия купцов") {
		t.Fatalf("morag = %+v, %v", morag, err)
	}
}

func TestSearchEmptyAndLimits(t *testing.T) {
	store, slug := searchFixture(t)

	if hits, err := store.SearchCampaignMemory(slug, "несуществующееслово", SearchOptions{}); err != nil || len(hits) != 0 {
		t.Fatalf("empty = %v, %v", hits, err)
	}
	if hits, err := store.SearchCampaignMemory("no-such-campaign", "Каэль", SearchOptions{}); err != nil || len(hits) != 0 {
		t.Fatalf("unknown = %v, %v", hits, err)
	}
	if hits, _ := store.SearchCampaignMemory(slug, "Каэль", SearchOptions{Limit: 2}); len(hits) != 2 {
		t.Fatalf("limit = %+v", hits)
	}
	perSourceHits, _ := store.SearchCampaignMemory(slug, "Каэль", SearchOptions{PerSource: 1})
	transcripts := 0
	for _, hit := range perSourceHits {
		if strings.HasPrefix(hit.Source, "транскрипт") {
			transcripts++
		}
	}
	if transcripts != 1 {
		t.Fatalf("perSource = %+v", perSourceHits)
	}
	if hits, _ := store.SearchCampaignMemory(slug, "а у", SearchOptions{}); len(hits) != 0 {
		t.Fatalf("короткие термы = %+v", hits)
	}
}

func TestSearchFTSPrefixAndFallback(t *testing.T) {
	store, slug := searchFixture(t)

	hits, err := store.SearchCampaignMemory(slug, "прокляти", SearchOptions{})
	if err != nil || len(hits) == 0 {
		t.Fatalf("prefix = %+v, %v", hits, err)
	}
	found := false
	for _, hit := range hits {
		if hit.Source == "транскрипт, день 3" && strings.Contains(hit.Snippet, "проклятие") {
			found = true
		}
	}
	if !found {
		t.Fatalf("prefix hit = %+v", hits)
	}

	substring, err := store.SearchCampaignMemory(slug, "эль", SearchOptions{})
	if err != nil || len(substring) == 0 {
		t.Fatalf("fallback = %+v, %v", substring, err)
	}
	found = false
	for _, hit := range substring {
		if strings.Contains(hit.Snippet, "Каэль") {
			found = true
		}
	}
	if !found {
		t.Fatalf("substring hit = %+v", substring)
	}
}

func TestSearchScopesByCampaign(t *testing.T) {
	store, slug := searchFixture(t)
	other := newCampaign(t, store, "other-campaign")
	_ = store.AppendTranscriptEntry(other.Slug, 5, TranscriptEntry{Kind: "dm", Text: "Каэль — злой близнец в другом мире."})

	hits, err := store.SearchCampaignMemory(slug, "близнец", SearchOptions{})
	if err != nil || len(hits) != 0 {
		t.Fatalf("чужой хит = %+v, %v", hits, err)
	}
	own, _ := store.SearchCampaignMemory(slug, "Каэль", SearchOptions{})
	for _, hit := range own {
		if strings.Contains(hit.Snippet, "близнец") {
			t.Fatalf("чужой сниппет = %q", hit.Snippet)
		}
	}
}

func TestSearchCatchUp(t *testing.T) {
	store, slug := searchFixture(t)

	_ = store.AppendTranscriptEntry(slug, 4, TranscriptEntry{Kind: "dm", Text: "Артефакт спрятан под алтарём."})
	hits, err := store.SearchCampaignMemory(slug, "артефакт", SearchOptions{})
	if err != nil || len(hits) == 0 || !containsSource(hitSources(hits), "транскрипт, день 4") {
		t.Fatalf("catch-up = %+v, %v", hits, err)
	}
	before := len(hits)
	_ = store.AppendTranscriptEntry(slug, 4, TranscriptEntry{Kind: "dm", Text: "Каэль вспоминает про артефакт."})
	after, _ := store.SearchCampaignMemory(slug, "артефакт", SearchOptions{})
	if len(after) <= before {
		t.Fatalf("новая запись не найдена: %d -> %d", before, len(after))
	}
	again, _ := store.SearchCampaignMemory(slug, "артефакт", SearchOptions{})
	if len(again) != len(after) {
		t.Fatalf("повторный поиск задублировал: %d -> %d", len(after), len(again))
	}

	var last int
	if err := store.db.QueryRow(
		"SELECT last_source_id FROM fts_meta WHERE source = 'transcript_entries'",
	).Scan(&last); err != nil {
		t.Fatal(err)
	}
	var maxID int
	_ = store.db.QueryRow("SELECT MAX(id) FROM transcript_entries").Scan(&maxID)
	if last < maxID {
		t.Fatalf("индекс отстал: %d < %d", last, maxID)
	}
}

func TestSearchFTSLedgerAndDeleteTrigger(t *testing.T) {
	store, slug := searchFixture(t)

	_ = store.AppendLedgerRow(slug, LedgerRow{Day: 2, Type: LedgerFound, ItemOrGold: "Серебряный медальон"}, nil)
	hits, err := store.SearchCampaignMemory(slug, "медальон", SearchOptions{})
	if err != nil || !containsSource(hitSources(hits), "журнал лута, день 2") {
		t.Fatalf("ledger = %+v, %v", hits, err)
	}

	_ = store.AppendKeyEvent(slug, 2, "Древний клинок блестит во тьме.", nil, false)
	before, _ := store.SearchCampaignMemory(slug, "клинок", SearchOptions{})
	if !containsSource(hitSources(before), "ключевое событие, день 2") {
		t.Fatalf("before = %+v", before)
	}
	if _, err := store.db.Exec("DELETE FROM key_events WHERE line LIKE '%клинок%'"); err != nil {
		t.Fatal(err)
	}
	after, _ := store.SearchCampaignMemory(slug, "клинок", SearchOptions{})
	if containsSource(hitSources(after), "ключевое событие, день 2") {
		t.Fatalf("удалённая строка осталась в индексе: %+v", after)
	}
}

func TestSearchPerSourceFreshness(t *testing.T) {
	store, slug := searchFixture(t)

	for _, item := range []struct {
		day  int
		text string
	}{
		{4, "Каэль идёт в лес."},
		{5, "Каэль находит следы."},
		{6, "Каэль встречает волков."},
		{7, "Каэль возвращается к часовне."},
	} {
		_ = store.AppendTranscriptEntry(slug, item.day, TranscriptEntry{Kind: "dm", Text: item.text})
	}
	hits, err := store.SearchCampaignMemory(slug, "Каэль", SearchOptions{PerSource: 3})
	if err != nil {
		t.Fatal(err)
	}
	var days []int
	for _, hit := range hits {
		if strings.HasPrefix(hit.Source, "транскрипт") && hit.Day != nil {
			days = append(days, *hit.Day)
		}
	}
	if len(days) != 3 || days[0] != 7 || days[1] != 6 || days[2] != 5 {
		t.Fatalf("days = %v", days)
	}
}
