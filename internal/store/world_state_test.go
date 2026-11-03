package store

import (
	"strings"
	"testing"
)

func TestWorldState(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "test-campaign")

	if err := store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "Погибшие", Text: "Ян", Day: ptr(1)}); err != nil {
		t.Fatal(err)
	}
	categories, _, err := store.ReadWorldState(campaign.Slug)
	if err != nil {
		t.Fatal(err)
	}
	items := categories["Погибшие"]
	if len(items) != 1 || items[0] != "- Ян (день 1)" {
		t.Fatalf("items = %v", items)
	}

	// Идемпотентно по категории+тексту: обновляется день.
	_ = store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "Погибшие", Text: "Ян", Day: ptr(5)})
	categories, _, _ = store.ReadWorldState(campaign.Slug)
	if len(categories["Погибшие"]) != 1 || categories["Погибшие"][0] != "- Ян (день 5)" {
		t.Fatalf("items = %v", categories["Погибшие"])
	}

	_ = store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "Погибшие", Text: "Марк", Day: ptr(2)})
	_ = store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "Изменения", Text: "Мораг спасён", Day: ptr(2)})
	categories, _, _ = store.ReadWorldState(campaign.Slug)
	if len(categories["Погибшие"]) != 2 || len(categories["Изменения"]) != 1 {
		t.Fatalf("categories = %v", categories)
	}
}

func TestWorldStateDefaultsAndEmpty(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "test-campaign")

	_ = store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "Погибшие", Text: "   "})
	categories, _, _ := store.ReadWorldState(campaign.Slug)
	if len(categories) != 0 {
		t.Fatalf("пустой текст записался: %v", categories)
	}
	if empty, _, err := store.ReadWorldState("no-such-campaign"); err != nil || len(empty) != 0 {
		t.Fatalf("unknown = %v, %v", empty, err)
	}

	_ = store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "   ", Text: "Что-то случилось"})
	categories, _, _ = store.ReadWorldState(campaign.Slug)
	if _, ok := categories["Изменения"]; !ok {
		t.Fatalf("дефолтная категория: %v", categories)
	}

	_ = store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "Изменения", Text: "Мораг благодарен"})
	categories, _, _ = store.ReadWorldState(campaign.Slug)
	items := categories["Изменения"]
	found := false
	for _, item := range items {
		if item == "- Мораг благодарен" {
			found = true
		}
	}
	if !found {
		t.Fatalf("запись без дня = %v", items)
	}
}

func TestRenderWorldState(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "test-campaign")

	_ = store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "Погибшие", Text: "Ян", Day: ptr(1)})
	_ = store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "Изменения", Text: "Мораг благодарен"})
	rendered, err := store.RenderWorldState(campaign.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "## Погибшие") || !strings.Contains(rendered, "- Ян (день 1)") ||
		!strings.Contains(rendered, "## Изменения") || !strings.Contains(rendered, "- Мораг благодарен") {
		t.Fatalf("rendered = %q", rendered)
	}

	emptyStore := newTestStore(t)
	emptyCampaign := newCampaign(t, emptyStore, "empty")
	if rendered, _ := emptyStore.RenderWorldState(emptyCampaign.Slug); rendered != "" {
		t.Fatalf("rendered = %q", rendered)
	}
	if rendered, _ := emptyStore.RenderWorldState("no-such-campaign"); rendered != "" {
		t.Fatalf("rendered = %q", rendered)
	}
}
