package store

import (
	"strings"
	"testing"
)

func TestUpsertNpcCreatesProfile(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	npc, err := store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "Трактирщик Борн", Role: ptr("трактирщик")})
	if err != nil {
		t.Fatal(err)
	}
	if npc.Name != "Трактирщик Борн" || npc.Status != NpcAlive || npc.Role == nil || *npc.Role != "трактирщик" {
		t.Fatalf("npc = %+v", npc)
	}
	if npc.Memory != "" || npc.ID == "" || npc.CreatedAt == "" {
		t.Fatalf("npc = %+v", npc)
	}
}

func TestUpsertNpcMemoryDayMarkers(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	if _, err := store.UpsertNpc(campaign.Slug, NpcUpsertInput{
		Name: "Борн", MemoryAppend: ptr("Игроки купили комнату."), MemoryAppendDay: ptr(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNpc(campaign.Slug, NpcUpsertInput{
		Name: "Борн", MemoryAppend: ptr("Борн видел странный свет."),
	}); err != nil {
		t.Fatal(err)
	}
	npc, err := store.GetNpc(campaign.Slug, "Борн")
	if err != nil || npc == nil {
		t.Fatalf("GetNpc = %v, %v", npc, err)
	}
	if !strings.Contains(npc.Memory, "[День 1] Игроки купили комнату.") || !strings.Contains(npc.Memory, "Борн видел странный свет.") {
		t.Fatalf("memory = %q", npc.Memory)
	}
}

func TestUpsertNpcDoesNotDuplicateInlineDay(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	for _, input := range []NpcUpsertInput{
		{Name: "Борн", MemoryAppend: ptr("[День 1] Ночью Дэн вернулся в деревню."), MemoryAppendDay: ptr(1)},
		{Name: "Борн", MemoryAppend: ptr("[День 2] [День 2] Кормак признался о голосе."), MemoryAppendDay: ptr(2)},
	} {
		if _, err := store.UpsertNpc(campaign.Slug, input); err != nil {
			t.Fatal(err)
		}
	}
	npc, _ := store.GetNpc(campaign.Slug, "Борн")
	if strings.Contains(npc.Memory, "[День 1] [День 1]") || strings.Contains(npc.Memory, "[День 2] [День 2]") {
		t.Fatalf("двойной маркер: %q", npc.Memory)
	}
	if !strings.HasPrefix(npc.Memory, "- [День 1] Ночью Дэн вернулся в деревню.") {
		t.Fatalf("memory = %q", npc.Memory)
	}
	if !strings.Contains(npc.Memory, "\n- [День 2] Кормак признался о голосе.") {
		t.Fatalf("memory = %q", npc.Memory)
	}

	// Встроенная дата используется, если memoryAppendDay не задан.
	if _, err := store.UpsertNpc(campaign.Slug, NpcUpsertInput{
		Name: "Борн", MemoryAppend: ptr("[День 3] Борн видел странный свет."),
	}); err != nil {
		t.Fatal(err)
	}
	npc, _ = store.GetNpc(campaign.Slug, "Борн")
	if !strings.Contains(npc.Memory, "- [День 3] Борн видел странный свет.") || strings.Contains(npc.Memory, "[День 3] [День 3]") {
		t.Fatalf("memory = %q", npc.Memory)
	}
}

func TestUpsertNpcUpdatesByCaseInsensitiveName(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	_, _ = store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "Борн", Status: ptr(NpcAlive), FirstSeenDay: ptr(1)})
	updated, err := store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "борн", Status: ptr(NpcDead), Location: ptr("морг")})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != NpcDead || updated.Location == nil || *updated.Location != "морг" || updated.FirstSeenDay == nil || *updated.FirstSeenDay != 1 {
		t.Fatalf("updated = %+v", updated)
	}
	npcs, _ := store.ListNpcs(campaign.Slug)
	if len(npcs) != 1 {
		t.Fatalf("npcs = %+v", npcs)
	}
}

func TestUpsertNpcMergesRelationships(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	_, _ = store.UpsertNpc(campaign.Slug, NpcUpsertInput{
		Name: "Борн", Relationships: map[string]NpcRelationship{"Ария": {Attitude: 2}},
	})
	npc, err := store.UpsertNpc(campaign.Slug, NpcUpsertInput{
		Name: "Борн",
		Relationships: map[string]NpcRelationship{
			"Ария":  {Attitude: 3, Notes: ptr("должник")},
			"Гимли": {Attitude: -1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if npc.Relationships["Ария"].Attitude != 3 || npc.Relationships["Ария"].Notes == nil || *npc.Relationships["Ария"].Notes != "должник" {
		t.Fatalf("Ария = %+v", npc.Relationships["Ария"])
	}
	if npc.Relationships["Гимли"].Attitude != -1 {
		t.Fatalf("Гимли = %+v", npc.Relationships["Гимли"])
	}
}

func TestGetNpcByIdSlugName(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	npc, _ := store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "Капитан стражи"})
	for _, key := range []string{npc.ID, npc.Slug, "капитан стражи"} {
		found, err := store.GetNpc(campaign.Slug, key)
		if err != nil || found == nil || found.Name != "Капитан стражи" {
			t.Fatalf("GetNpc(%q) = %v, %v", key, found, err)
		}
	}
	missing, err := store.GetNpc(campaign.Slug, "Незнакомец")
	if err != nil || missing != nil {
		t.Fatalf("missing = %v, %v", missing, err)
	}
}

func TestNpcSlugCollisions(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	first, _ := store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "Иван"})
	second, _ := store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "ivan"})
	if first.Slug != "ivan" || second.Slug != "ivan-2" {
		t.Fatalf("slugs = %q, %q", first.Slug, second.Slug)
	}
	npcs, _ := store.ListNpcs(campaign.Slug)
	if len(npcs) != 2 {
		t.Fatalf("npcs = %+v", npcs)
	}
}

func TestLastMemoryLine(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	_, _ = store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "Борн", MemoryAppend: ptr("Видел героев."), MemoryAppendDay: ptr(1)})
	_, _ = store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "Борн", MemoryAppend: ptr("Герои спасли ему жизнь."), MemoryAppendDay: ptr(3)})
	npc, _ := store.GetNpc(campaign.Slug, "Борн")
	line, err := store.LastMemoryLine(campaign.Slug, npc.Slug, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "спасли ему жизнь") || strings.Contains(line, "Видел героев") {
		t.Fatalf("line = %q", line)
	}

	quiet, _ := store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "Молчаливый"})
	if line, _ := store.LastMemoryLine(campaign.Slug, quiet.Slug, 100); line != "" {
		t.Fatalf("line = %q", line)
	}

	talkative, _ := store.UpsertNpc(campaign.Slug, NpcUpsertInput{
		Name: "Болтун", MemoryAppend: ptr(strings.Repeat("А", 300)), MemoryAppendDay: ptr(1),
	})
	line, _ = store.LastMemoryLine(campaign.Slug, talkative.Slug, 50)
	if !strings.HasSuffix(line, "…") || len([]rune(line)) > 60 {
		t.Fatalf("line = %q", line)
	}
}
