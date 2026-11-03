package store

import (
	"testing"
)

func TestUpsertLocationCreatesProfile(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	location, err := store.UpsertLocation(campaign.Slug, UpsertLocationInput{
		Name: "Мораг", Description: ptr("Деревня у ручья"), DiscoveredDay: ptr(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if location.Name != "Мораг" || location.Description == nil || *location.Description != "Деревня у ручья" {
		t.Fatalf("location = %+v", location)
	}
	if location.DiscoveredDay == nil || *location.DiscoveredDay != 1 {
		t.Fatalf("discoveredDay = %+v", location.DiscoveredDay)
	}
	if len(location.Connections) != 0 || len(location.VisitedDays) != 0 || location.Current != nil || location.ID == "" {
		t.Fatalf("location = %+v", location)
	}
}

func TestUpsertLocationUpdatesByName(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	_, _ = store.UpsertLocation(campaign.Slug, UpsertLocationInput{Name: "Мораг", DiscoveredDay: ptr(1)})
	updated, err := store.UpsertLocation(campaign.Slug, UpsertLocationInput{
		Name: "мораг", Description: ptr("Полуразрушенная деревня"),
		Connections: &[]LocationConnection{{To: "Чёрный лес", Via: ptr("тропа")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Description == nil || *updated.Description != "Полуразрушенная деревня" {
		t.Fatalf("description = %+v", updated.Description)
	}
	if updated.DiscoveredDay == nil || *updated.DiscoveredDay != 1 || len(updated.Connections) != 1 || updated.Connections[0].To != "Чёрный лес" {
		t.Fatalf("updated = %+v", updated)
	}
	locations, _ := store.ListLocations(campaign.Slug)
	if len(locations) != 1 {
		t.Fatalf("locations = %+v", locations)
	}
}

func TestCurrentLocationExclusive(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	_, _ = store.UpsertLocation(campaign.Slug, UpsertLocationInput{Name: "Мораг", Current: ptr(true)})
	_, _ = store.UpsertLocation(campaign.Slug, UpsertLocationInput{Name: "Чёрный лес", Current: ptr(true)})
	locations, err := store.ListLocations(campaign.Slug)
	if err != nil {
		t.Fatal(err)
	}
	var morag, forest *Location
	for index := range locations {
		if locations[index].Name == "Мораг" {
			morag = &locations[index]
		}
		if locations[index].Name == "Чёрный лес" {
			forest = &locations[index]
		}
	}
	if morag == nil || forest == nil || morag.Current == nil || *morag.Current || forest.Current == nil || !*forest.Current {
		t.Fatalf("locations = %+v", locations)
	}
	current, err := store.CurrentLocation(campaign.Slug)
	if err != nil || current == nil || current.Name != "Чёрный лес" {
		t.Fatalf("current = %+v, %v", current, err)
	}
}

func TestMarkVisited(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	location, _ := store.UpsertLocation(campaign.Slug, UpsertLocationInput{Name: "Мораг", DiscoveredDay: ptr(1)})
	_, _ = store.MarkVisited(campaign.Slug, location.Slug, 3)
	_, _ = store.MarkVisited(campaign.Slug, location.Slug, 1)
	_, _ = store.MarkVisited(campaign.Slug, location.Slug, 3)
	reloaded, err := store.GetLocation(campaign.Slug, location.Slug)
	if err != nil || reloaded == nil {
		t.Fatalf("reloaded = %v, %v", reloaded, err)
	}
	if len(reloaded.VisitedDays) != 2 || reloaded.VisitedDays[0] != 1 || reloaded.VisitedDays[1] != 3 {
		t.Fatalf("visited = %v", reloaded.VisitedDays)
	}
}

func TestGetLocationByKeysAndSetCurrentError(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	location, _ := store.UpsertLocation(campaign.Slug, UpsertLocationInput{Name: "Развалины"})
	for _, key := range []string{location.ID, location.Slug, "развалины"} {
		found, err := store.GetLocation(campaign.Slug, key)
		if err != nil || found == nil || found.Name != "Развалины" {
			t.Fatalf("GetLocation(%q) = %v, %v", key, found, err)
		}
	}
	if found, err := store.GetLocation(campaign.Slug, "Нигде"); err != nil || found != nil {
		t.Fatalf("missing = %v, %v", found, err)
	}
	wantCode(t, store.SetCurrent(campaign.Slug, "несуществующая"), ErrNotFound)
}

func TestLocationConnectionsRoundTrip(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	_, _ = store.UpsertLocation(campaign.Slug, UpsertLocationInput{
		Name: "Перевал",
		Connections: &[]LocationConnection{
			{To: "Мораг", Via: ptr("тропа"), DiscoveredDay: ptr(2)},
			{To: "Чёрный лес"},
		},
	})
	reloaded, err := store.GetLocation(campaign.Slug, "Перевал")
	if err != nil || reloaded == nil || len(reloaded.Connections) != 2 {
		t.Fatalf("reloaded = %+v, %v", reloaded, err)
	}
	first := reloaded.Connections[0]
	if first.To != "Мораг" || first.Via == nil || *first.Via != "тропа" || first.DiscoveredDay == nil || *first.DiscoveredDay != 2 {
		t.Fatalf("first = %+v", first)
	}
	if reloaded.Connections[1].To != "Чёрный лес" || reloaded.Connections[1].Via != nil || reloaded.Connections[1].DiscoveredDay != nil {
		t.Fatalf("second = %+v", reloaded.Connections[1])
	}
}

func TestLocationSameNameDifferentCase(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тёмный лес")

	first, _ := store.UpsertLocation(campaign.Slug, UpsertLocationInput{Name: "Лагерь", DiscoveredDay: ptr(1)})
	second, _ := store.UpsertLocation(campaign.Slug, UpsertLocationInput{Name: "лагерь", Description: ptr("Тент у ручья")})
	if first.Slug != second.Slug {
		t.Fatalf("slugs = %q, %q", first.Slug, second.Slug)
	}
	locations, _ := store.ListLocations(campaign.Slug)
	if len(locations) != 1 || locations[0].DiscoveredDay == nil || *locations[0].DiscoveredDay != 1 {
		t.Fatalf("locations = %+v", locations)
	}
	if locations[0].Description == nil || *locations[0].Description != "Тент у ручья" {
		t.Fatalf("description = %+v", locations[0].Description)
	}
}
