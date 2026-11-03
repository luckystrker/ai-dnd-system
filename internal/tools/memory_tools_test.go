package tools

import (
	"strings"
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func TestCreateUpdateCompleteQuest(t *testing.T) {
	env := newTestEnv(t)

	created, err := env.deps.createQuest(env.player, createQuestArgs{
		Title: "Амулет для кузнеца", Objective: "Найти амулет", Difficulty: "easy",
		GiverNpc: ptr("Кузнец"),
	})
	if err != nil {
		t.Fatalf("createQuest: %v", err)
	}
	quest := created.(map[string]any)["quest"].(map[string]any)
	if quest["status"] != "offered" || quest["title"] != "Амулет для кузнеца" {
		t.Fatalf("quest = %+v", quest)
	}
	if _, err := env.deps.createQuest(env.player, createQuestArgs{
		Title: "амулет для кузнеца", Objective: "x", Difficulty: "easy",
	}); err == nil {
		t.Fatal("дубль квеста должен падать")
	}
	if _, err := env.deps.updateQuest(env.player, updateQuestArgs{
		Quest: "Амулет для кузнеца", Status: ptr("active"),
	}); err != nil {
		t.Fatalf("updateQuest: %v", err)
	}
	completed, err := env.deps.completeQuest(env.player, completeQuestArgs{
		Quest: "Амулет для кузнеца", Result: "completed",
	})
	if err != nil {
		t.Fatalf("completeQuest: %v", err)
	}
	result := completed.(map[string]any)
	if len(result["granted"].([]map[string]any)) != 1 {
		t.Fatalf("granted = %+v", result["granted"])
	}
	granted := result["granted"].([]map[string]any)[0]
	if granted["xp"].(int) != 36 || granted["gold"].(int) != 50 {
		t.Fatalf("granted = %+v", granted)
	}
	quests, _ := env.store.ListQuests(env.campaign.ID)
	if quests[0].Status != "completed" {
		t.Fatalf("status = %q", quests[0].Status)
	}
	events, _ := env.store.ReadKeyEvents(env.campaign.Slug)
	if !strings.Contains(events, "завершён") {
		t.Fatalf("key events = %q", events)
	}
	ledger, _ := env.store.ReadLedger(env.campaign.Slug, nil, 0)
	if len(ledger) != 1 || !strings.Contains(ledger[0], "награда за квест") {
		t.Fatalf("ledger = %v", ledger)
	}
	_, err = env.deps.completeQuest(env.player, completeQuestArgs{Quest: "Амулет для кузнеца", Result: "completed"})
	wantToolCode(t, err, CodeConflict)
}

func TestThreads(t *testing.T) {
	env := newTestEnv(t)

	created, err := env.deps.appendThread(env.player, appendThreadArgs{
		Text: "Обещали вернуть долг стражнику", Kind: ptr("debt"),
	})
	if err != nil {
		t.Fatalf("appendThread: %v", err)
	}
	if created.(map[string]any)["thread"].(map[string]any)["kind"] != "debt" {
		t.Fatalf("thread = %+v", created)
	}
	list, err := env.deps.listThreads(env.player, listThreadsArgs{})
	if err != nil || len(list.(map[string]any)["threads"].([]map[string]any)) != 1 {
		t.Fatalf("listThreads = %+v, %v", list, err)
	}
	resolved, err := env.deps.resolveThread(env.player, resolveThreadArgs{Thread: "вернуть долг", Day: ptr(3)})
	if err != nil {
		t.Fatalf("resolveThread: %v", err)
	}
	thread := resolved.(map[string]any)["thread"].(map[string]any)
	if thread["status"] != "resolved" || thread["dayClosed"] != 3 {
		t.Fatalf("thread = %+v", thread)
	}
	open, _ := env.deps.listThreads(env.player, listThreadsArgs{})
	if len(open.(map[string]any)["threads"].([]map[string]any)) != 0 {
		t.Fatalf("open = %+v", open)
	}
	all, _ := env.deps.listThreads(env.player, listThreadsArgs{IncludeResolved: ptr(true)})
	if len(all.(map[string]any)["threads"].([]map[string]any)) != 1 {
		t.Fatalf("all = %+v", all)
	}
}

func TestNpcDeathWritesWorldState(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.deps.upsertNpc(env.player, upsertNpcArgs{
		Name: "Кузнец", Role: ptr("кузнец"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.deps.upsertNpc(env.player, upsertNpcArgs{
		Name: "Кузнец", Status: ptr("dead"), Location: ptr("кузница"),
	}); err != nil {
		t.Fatal(err)
	}
	rendered, err := env.store.RenderWorldState(env.campaign.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "## Погибшие") || !strings.Contains(rendered, "Кузнец (кузница)") {
		t.Fatalf("world state = %q", rendered)
	}
	listed, err := env.deps.listWorldState(env.player)
	if err != nil {
		t.Fatal(err)
	}
	if listed.(map[string]any)["empty"].(bool) {
		t.Fatalf("world state пуст: %+v", listed)
	}
}

func TestLocationsAndMoveParty(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.deps.upsertLocation(env.player, upsertLocationArgs{
		Name: "Мораг", Description: ptr("Деревня"), DiscoveredDay: ptr(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.deps.upsertLocation(env.player, upsertLocationArgs{
		Name: "Часовня", DiscoveredDay: ptr(1),
	}); err != nil {
		t.Fatal(err)
	}
	moved, err := env.deps.moveParty(env.player, movePartyArgs{Location: "Часовня", Via: ptr("тропа")})
	if err != nil {
		t.Fatalf("moveParty: %v", err)
	}
	if moved.(map[string]any)["location"] != "Часовня" {
		t.Fatalf("moved = %+v", moved)
	}
	current, _ := env.store.CurrentLocation(env.campaign.ID)
	if current == nil || current.Name != "Часовня" {
		t.Fatalf("current = %+v", current)
	}
	record, _ := env.store.ReadDay(env.campaign.Slug, 1)
	found := false
	for _, entry := range record.Entries {
		if strings.Contains(entry, "Партия переместилась в «Часовня» через тропа.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("transcript = %v", record.Entries)
	}
	_, err = env.deps.moveParty(env.player, movePartyArgs{Location: "Нигде"})
	wantToolCode(t, err, CodeNotFound)
}

func TestFactionsAndLedger(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.deps.upsertFaction(env.player, upsertFactionArgs{
		Name: "Гильдия купцов", Standing: ptr(4),
	}); err != nil {
		t.Fatal(err)
	}
	adjusted, err := env.deps.adjustStanding(env.player, adjustStandingArgs{Faction: "гильдия купцов", Delta: 3})
	if err != nil {
		t.Fatalf("adjustStanding: %v", err)
	}
	if adjusted.(map[string]any)["faction"].(map[string]any)["standing"] != 5 {
		t.Fatalf("adjusted = %+v", adjusted)
	}
	_, err = env.deps.adjustStanding(env.player, adjustStandingArgs{Faction: "Нет такой", Delta: 1})
	wantToolCode(t, err, CodeNotFound)

	env.store.AppendLedgerRow(env.campaign.Slug, store.LedgerRow{Day: 1, Type: store.LedgerFound, ItemOrGold: "кинжал"}, nil)
	env.store.AppendLedgerRow(env.campaign.Slug, store.LedgerRow{Day: 2, Type: store.LedgerSpent, ItemOrGold: "еда"}, nil)
	ledger, err := env.deps.listLedger(env.player, listLedgerArgs{Type: ptr("found")})
	if err != nil {
		t.Fatalf("listLedger: %v", err)
	}
	if ledger.(map[string]any)["count"] != 1 {
		t.Fatalf("ledger = %+v", ledger)
	}
}

func TestJournalToolWrappers(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.deps.appendKeyEvent(env.player, appendKeyEventArgs{
		Event: "Партия нашла артефакт.", Permanent: ptr(true),
	}); err != nil {
		t.Fatal(err)
	}
	events, _ := env.store.ReadKeyEvents(env.campaign.Slug)
	if !strings.Contains(events, "[важно]") {
		t.Fatalf("events = %q", events)
	}
	if _, err := env.deps.updateDaySummary(env.player, updateDaySummaryArgs{
		Summary: "Партия вошла в подвал.", Headline: ptr("Подвал"),
	}); err != nil {
		t.Fatal(err)
	}
	day, _ := env.store.ReadDay(env.campaign.Slug, 1)
	if day.Summary == nil || day.Headline == nil {
		t.Fatalf("day = %+v", day)
	}
	if _, err := env.deps.appendCampaignSummary(env.player, appendCampaignSummaryArgs{
		Day: 1, Text: "Первый день.",
	}); err != nil {
		t.Fatal(err)
	}
	summary, _ := env.store.ReadCampaignSummary(env.campaign.Slug)
	if !strings.Contains(summary, "## День 1") {
		t.Fatalf("summary = %q", summary)
	}
	if _, err := env.deps.readDay(env.player, readDayArgs{Day: ptr(99)}); err == nil {
		t.Fatal("несуществующий день должен давать ошибку")
	}
}

func TestSearchAndReadDayTools(t *testing.T) {
	env := newTestEnv(t)
	if err := env.store.AppendTranscriptEntry(env.campaign.Slug, 3, store.TranscriptEntry{Kind: "dm", Text: "Каэль снимает проклятие."}); err != nil {
		t.Fatal(err)
	}
	day, err := env.deps.readDay(env.player, readDayArgs{Day: ptr(3)})
	if err != nil {
		t.Fatalf("readDay: %v", err)
	}
	if len(day.(map[string]any)["entries"].([]string)) != 1 {
		t.Fatalf("day = %+v", day)
	}
	found, err := env.deps.searchMemory(env.player, searchMemoryArgs{Query: "прокляти"})
	if err != nil {
		t.Fatalf("searchMemory: %v", err)
	}
	if found.(map[string]any)["count"].(int) == 0 {
		t.Fatalf("search = %+v", found)
	}
}
