package store

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func journalFixture(t *testing.T) (*Store, string) {
	t.Helper()
	store := newTestStore(t)
	campaign := newCampaign(t, store, "test-campaign")
	return store, campaign.Slug
}

func TestEnsureDayAndListDays(t *testing.T) {
	store, slug := journalFixture(t)

	if err := store.EnsureDay(slug, 1, ptr("Старт"), nil); err != nil {
		t.Fatal(err)
	}
	day, err := store.ReadDay(slug, 1)
	if err != nil || day == nil {
		t.Fatalf("ReadDay = %+v, %v", day, err)
	}
	if day.Date == nil || !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(*day.Date) {
		t.Fatalf("date = %+v", day.Date)
	}
	if day.Note == nil || *day.Note != "Старт" {
		t.Fatalf("note = %+v", day.Note)
	}

	if err := store.EnsureDay(slug, 2, ptr("Первая версия"), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDay(slug, 2, ptr("Не перезапишется"), nil); err != nil {
		t.Fatal(err)
	}
	second, _ := store.ReadDay(slug, 2)
	if second.Note == nil || *second.Note != "Первая версия" {
		t.Fatalf("ensureDay не идемпотентен: %+v", second.Note)
	}

	for _, dayNumber := range []int{2, 10, 1} {
		if err := store.EnsureDay(slug, dayNumber, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	days, err := store.ListDays(slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 || days[0] != 1 || days[1] != 2 || days[2] != 10 {
		t.Fatalf("days = %v", days)
	}
}

func TestAppendTranscriptEntryFormats(t *testing.T) {
	store, slug := journalFixture(t)

	if err := store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "player", Author: ptr("gandalf"), Text: "Я осматриваю дверь."}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "dm", Text: "Дверь заперта."}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "action", Text: "Ария прячется в тенях."}); err != nil {
		t.Fatal(err)
	}
	day, err := store.ReadDay(slug, 1)
	if err != nil || day == nil {
		t.Fatalf("ReadDay = %v, %v", day, err)
	}
	if len(day.Entries) != 3 {
		t.Fatalf("entries = %v", day.Entries)
	}
	checks := []struct {
		entry string
		want  string
	}{
		{day.Entries[0], `^-\s+\[\d{2}:\d{2}\] \*\*Игрок @gandalf\*\*: Я осматриваю дверь\.$`},
		{day.Entries[1], `^-\s+\[\d{2}:\d{2}\] \*\*DM\*\*: Дверь заперта\.$`},
		{day.Entries[2], `^-\s+\[\d{2}:\d{2}\] \*Ария прячется в тенях\.\*$`},
	}
	for _, check := range checks {
		if !regexp.MustCompile(check.want).MatchString(check.entry) {
			t.Errorf("entry %q не подходит под %q", check.entry, check.want)
		}
	}
}

func TestAppendTranscriptDedupAndWhitespace(t *testing.T) {
	store, slug := journalFixture(t)

	for i := 0; i < 2; i++ {
		if err := store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "action", Text: "Событие", EventID: ptr("evt-1")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "dm", Text: "Первая\n\nстрока\n длинная"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "dm", Text: "   "}); err != nil {
		t.Fatal(err)
	}
	day, _ := store.ReadDay(slug, 1)
	if day == nil || len(day.Entries) != 2 {
		t.Fatalf("entries = %v", day)
	}
	if !strings.HasSuffix(day.Entries[1], "**DM**: Первая строка длинная") {
		t.Errorf("склейка строк = %q", day.Entries[1])
	}
}

func TestReadDayMissingAndTail(t *testing.T) {
	store, slug := journalFixture(t)

	missing, err := store.ReadDay(slug, 99)
	if err != nil || missing != nil {
		t.Fatalf("ReadDay(99) = %+v, %v", missing, err)
	}
	for i := 1; i <= 5; i++ {
		if err := store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "action", Text: "Событие " + strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}
	tail, err := store.ReadDayTail(slug, 1, 2)
	if err != nil || tail == nil || len(tail.Entries) != 2 {
		t.Fatalf("tail = %+v, %v", tail, err)
	}
	if !strings.Contains(tail.Entries[0], "Событие 4") || !strings.Contains(tail.Entries[1], "Событие 5") {
		t.Fatalf("tail entries = %v", tail.Entries)
	}
}

func TestSetDaySummaryAndHeadline(t *testing.T) {
	store, slug := journalFixture(t)

	if err := store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "action", Text: "Бой"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDaySummary(slug, 1, "Герои победили гоблинов."); err != nil {
		t.Fatal(err)
	}
	day, _ := store.ReadDay(slug, 1)
	if day.Summary == nil || *day.Summary != "Герои победили гоблинов." || len(day.Entries) != 1 {
		t.Fatalf("day = %+v", day)
	}
	if err := store.SetDaySummary(slug, 1, "Обновлённое саммари."); err != nil {
		t.Fatal(err)
	}
	day, _ = store.ReadDay(slug, 1)
	if day.Summary == nil || *day.Summary != "Обновлённое саммари." {
		t.Fatalf("summary = %+v", day.Summary)
	}

	if err := store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "action", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDayHeadline(slug, 1, strings.Repeat("А", 200)); err != nil {
		t.Fatal(err)
	}
	record, _ := store.ReadDayTail(slug, 1, 1)
	if record.Headline == nil || len([]rune(*record.Headline)) != 140 {
		t.Fatalf("headline length = %d", len([]rune(*record.Headline)))
	}
}

func TestCampaignSummary(t *testing.T) {
	store, slug := journalFixture(t)

	if err := store.UpsertCampaignSummaryDay(slug, 1, "День первый: знакомство."); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertCampaignSummaryDay(slug, 2, "День второй: бой."); err != nil {
		t.Fatal(err)
	}
	summary, err := store.ReadCampaignSummary(slug)
	if err != nil {
		t.Fatal(err)
	}
	want := "## День 1\n\nДень первый: знакомство.\n\n## День 2\n\nДень второй: бой."
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	if err := store.UpsertCampaignSummaryDay(slug, 1, "День первый: переписано."); err != nil {
		t.Fatal(err)
	}
	summary, _ = store.ReadCampaignSummary(slug)
	if !strings.Contains(summary, "## День 1\n\nДень первый: переписано.") || !strings.Contains(summary, "## День 2\n\nДень второй: бой.") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestAppendKeyEvents(t *testing.T) {
	store, slug := journalFixture(t)

	if err := store.AppendKeyEvent(slug, 1, "Герои нашли артефакт.", ptr("k1"), false); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendKeyEvent(slug, 2, "Артефакт украден.", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendKeyEvent(slug, 1, "Герои нашли артефакт.", ptr("k1"), false); err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadKeyEvents(slug)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(events, "Герои нашли артефакт") != 1 {
		t.Fatalf("дедуп по eventId не сработал: %q", events)
	}
	if !strings.Contains(events, "**День 2**: Артефакт украден.") {
		t.Fatalf("events = %q", events)
	}
	if !strings.Contains(events, "<!-- evt:k1 -->") {
		t.Fatalf("маркер явного eventId потерян: %q", events)
	}
}

func TestKeyEventContentDedup(t *testing.T) {
	store, slug := journalFixture(t)

	if err := store.AppendKeyEvent(slug, 1, "Кормак признался о голосе.", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendKeyEvent(slug, 1, "Кормак признался о голосе.", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendKeyEvent(slug, 2, "Кормак признался о голосе.", nil, false); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ReadKeyEvents(slug)
	if strings.Count(events, "Кормак признался") != 2 {
		t.Fatalf("разные дни — разные события: %q", events)
	}
	if err := store.AppendKeyEvent(slug, 1, "Кормак признался о голосе.", nil, true); err != nil {
		t.Fatal(err)
	}
	events, _ = store.ReadKeyEvents(slug)
	if strings.Count(events, "Кормак признался") != 3 {
		t.Fatalf("permanent — отдельное событие: %q", events)
	}
	rows, err := store.db.Query("SELECT event_id FROM key_events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var eventID *string
		if err := rows.Scan(&eventID); err != nil {
			t.Fatal(err)
		}
		if eventID == nil || *eventID == "" {
			t.Fatal("у события нет event_id")
		}
		if seen[*eventID] {
			t.Fatalf("event_id задублирован: %s", *eventID)
		}
		seen[*eventID] = true
	}
}

func TestKeyEventsEmptyAndSplit(t *testing.T) {
	store, slug := journalFixture(t)

	if err := store.AppendKeyEvent(slug, 1, "   \n ", nil, false); err != nil {
		t.Fatal(err)
	}
	if events, _ := store.ReadKeyEvents(slug); events != "" {
		t.Fatalf("пустое событие записалось: %q", events)
	}
	_ = store.AppendKeyEvent(slug, 1, "Обычное событие.", nil, false)
	_ = store.AppendKeyEvent(slug, 1, "Каэль дал нерушимую клятву.", nil, true)
	_ = store.AppendKeyEvent(slug, 2, "Город пал.", nil, true)
	events, _ := store.ReadKeyEvents(slug)
	if !strings.Contains(events, "- [важно] **День 1**:") || !strings.Contains(events, "- **День 1**: Обычное событие.") {
		t.Fatalf("events = %q", events)
	}
	permanent, regular, err := store.SplitKeyEvents(slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(permanent) != 2 || len(regular) != 1 {
		t.Fatalf("split = %d/%d", len(permanent), len(regular))
	}
	if !strings.HasPrefix(permanent[0], "- [важно] ") || !strings.HasPrefix(regular[0], "- ") || strings.HasPrefix(regular[0], "- [важно] ") {
		t.Fatalf("split lines = %v / %v", permanent, regular)
	}
}

func TestDayHeadlines(t *testing.T) {
	store, slug := journalFixture(t)

	_ = store.AppendTranscriptEntry(slug, 1, TranscriptEntry{Kind: "action", Text: "Что-то"})
	_ = store.AppendTranscriptEntry(slug, 2, TranscriptEntry{Kind: "action", Text: "Иное"})
	_ = store.SetDayHeadline(slug, 1, "Прибытие в Мораг")
	_ = store.SetDayHeadline(slug, 2, "Бой у часовни")
	headlines, err := store.ListDayHeadlines(slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(headlines) != 2 || headlines[0].Day != 1 || headlines[0].Headline != "Прибытие в Мораг" || headlines[1].Headline != "Бой у часовни" {
		t.Fatalf("headlines = %+v", headlines)
	}

	_ = store.AppendTranscriptEntry(slug, 3, TranscriptEntry{Kind: "action", Text: "z"})
	headlines, _ = store.ListDayHeadlines(slug)
	if len(headlines) != 2 {
		t.Fatalf("день без шапки не должен попадать: %+v", headlines)
	}
}

func TestBuildDayDigest(t *testing.T) {
	digest := BuildDayDigest([]string{
		"- [11:00] **Игрок @hero**: Я осматриваю дверь.",
		"- [11:05] **DM**: Дверь заперта.",
	}, 5)
	if !strings.HasPrefix(digest, AutoDigestMark) || !strings.Contains(digest, "осматриваю дверь") || !strings.Contains(digest, "Дверь заперта") {
		t.Fatalf("digest = %q", digest)
	}
	if strings.Contains(digest, "[11:00]") {
		t.Fatalf("метка времени не убрана: %q", digest)
	}
	stripped := BuildDayDigest([]string{"- [12:00] *Бросок костей* <!-- evt:abc -->"}, 5)
	if strings.Contains(stripped, "evt:abc") {
		t.Fatalf("маркер dedup остался: %q", stripped)
	}
	if BuildDayDigest(nil, 5) != "" {
		t.Fatal("пустой дайджест должен быть пустым")
	}
	var entries []string
	for i := 0; i < 20; i++ {
		entries = append(entries, "- [10:00] **DM**: событие "+strconv.Itoa(i))
	}
	limited := BuildDayDigest(entries, 5)
	if !strings.Contains(limited, "событие 19") || strings.Contains(limited, "событие 0") {
		t.Fatalf("ограничение хвоста сломано")
	}
}
