package store

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentUpsertNpcMemory(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	const writers = 20
	var wait sync.WaitGroup
	wait.Add(writers)
	for index := 0; index < writers; index++ {
		go func(index int) {
			defer wait.Done()
			_, err := store.UpsertNpc(campaign.Slug, NpcUpsertInput{
				Name:            "Борн",
				MemoryAppend:    ptr(fmt.Sprintf("Строка памяти %d", index)),
				MemoryAppendDay: ptr(1),
			})
			if err != nil {
				t.Errorf("UpsertNpc: %v", err)
			}
		}(index)
	}
	wait.Wait()

	npc, err := store.GetNpc(campaign.Slug, "Борн")
	if err != nil || npc == nil {
		t.Fatalf("GetNpc = %v, %v", npc, err)
	}
	lines := strings.Split(npc.Memory, "\n")
	if len(lines) != writers {
		t.Fatalf("потеряны записи памяти: %d из %d\n%s", len(lines), writers, npc.Memory)
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if seen[line] {
			t.Fatalf("дубль строки памяти: %q", line)
		}
		seen[line] = true
	}
}

func TestConcurrentAutoRegisterRespectsLimit(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Участники")

	const writers = 12
	var wait sync.WaitGroup
	wait.Add(writers)
	for index := 0; index < writers; index++ {
		go func(index int) {
			defer wait.Done()
			if _, err := store.AutoRegister(campaign.ID, NewMemberInput{
				UserID: fmt.Sprintf("u%d", index),
			}); err != nil {
				t.Errorf("AutoRegister: %v", err)
			}
		}(index)
	}
	wait.Wait()

	updated, err := store.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	players := 0
	for _, member := range updated.Members {
		if member.Role == RolePlayer {
			players++
		}
	}
	if players > MaxParty {
		t.Fatalf("лимит партии нарушен: %d игроков при максимуме %d", players, MaxParty)
	}
}

func TestConcurrentFTSIndexHasNoDuplicates(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Поиск")

	const events = 200
	for index := 0; index < events; index++ {
		if err := store.AppendKeyEvent(campaign.Slug, 1, fmt.Sprintf("Событие %d", index), nil, false); err != nil {
			t.Fatal(err)
		}
	}

	const searchers = 8
	var wait sync.WaitGroup
	wait.Add(searchers)
	for index := 0; index < searchers; index++ {
		go func() {
			defer wait.Done()
			if _, err := store.SearchCampaignMemory(campaign.Slug, "событие", SearchOptions{}); err != nil {
				t.Errorf("SearchCampaignMemory: %v", err)
			}
		}()
	}
	wait.Wait()

	var indexed int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM fts_journal WHERE source = 'key_events'",
	).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if indexed != events {
		t.Fatalf("fts_journal содержит %d строк на %d источников", indexed, events)
	}
	var distinct int
	if err := store.db.QueryRow(
		"SELECT COUNT(DISTINCT source_rowid) FROM fts_journal WHERE source = 'key_events'",
	).Scan(&distinct); err != nil {
		t.Fatal(err)
	}
	if distinct != events {
		t.Fatalf("уникальных source_rowid %d, want %d", distinct, events)
	}
}
