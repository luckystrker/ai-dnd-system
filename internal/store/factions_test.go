package store

import (
	"testing"
)

func TestUpsertFaction(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	faction, err := store.UpsertFaction(campaign.Slug, UpsertFactionInput{
		Name: "Гильдия купцов", Description: ptr("Торговцы Морага"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if faction.Name != "Гильдия купцов" || faction.Standing != 0 || faction.Description == nil || faction.ID == "" {
		t.Fatalf("faction = %+v", faction)
	}
}

func TestUpsertFactionUpdatesByName(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	_, _ = store.UpsertFaction(campaign.Slug, UpsertFactionInput{Name: "Орден клинка", Standing: ptr(1)})
	updated, _ := store.UpsertFaction(campaign.Slug, UpsertFactionInput{Name: "орден клинка", Description: ptr("Рыцари")})
	if updated.Standing != 1 || updated.Description == nil || *updated.Description != "Рыцари" {
		t.Fatalf("updated = %+v", updated)
	}
	factions, _ := store.ListFactions(campaign.Slug)
	if len(factions) != 1 {
		t.Fatalf("factions = %+v", factions)
	}
}

func TestAdjustStandingClamps(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	faction, _ := store.UpsertFaction(campaign.Slug, UpsertFactionInput{Name: "Клан гномов", Standing: ptr(4)})
	if _, err := store.AdjustStanding(campaign.Slug, faction.Slug, 3); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := store.GetFaction(campaign.Slug, faction.Slug)
	if reloaded.Standing != 5 {
		t.Fatalf("standing = %d", reloaded.Standing)
	}
	if _, err := store.AdjustStanding(campaign.Slug, faction.Slug, -20); err != nil {
		t.Fatal(err)
	}
	reloaded, _ = store.GetFaction(campaign.Slug, faction.Slug)
	if reloaded.Standing != -5 {
		t.Fatalf("standing = %d", reloaded.Standing)
	}
	wantCode(t, func() error {
		_, err := store.AdjustStanding(campaign.Slug, "несуществующая", 1)
		return err
	}(), ErrNotFound)
}

func TestGetFactionByKeys(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	faction, _ := store.UpsertFaction(campaign.Slug, UpsertFactionInput{Name: "Тайное общество"})
	for _, key := range []string{faction.ID, faction.Slug, "тайное общество"} {
		found, err := store.GetFaction(campaign.Slug, key)
		if err != nil || found == nil || found.Name != "Тайное общество" {
			t.Fatalf("GetFaction(%q) = %v, %v", key, found, err)
		}
	}
	if found, _ := store.GetFaction(campaign.Slug, "Незнакомцы"); found != nil {
		t.Fatalf("found = %+v", found)
	}
}

func TestStandingSurvivesReload(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	_, _ = store.UpsertFaction(campaign.Slug, UpsertFactionInput{Name: "Культ", Standing: ptr(-3)})
	reloaded, _ := store.GetFaction(campaign.Slug, "Культ")
	if reloaded.Standing != -3 {
		t.Fatalf("standing = %d", reloaded.Standing)
	}
}
