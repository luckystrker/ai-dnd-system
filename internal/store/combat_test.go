package store

import (
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/engine"
)

func combatSequence(faces ...int) engine.RandomSource {
	index := 0
	return func() float64 {
		face := 10
		if index < len(faces) {
			face = faces[index]
		}
		index++
		return (float64(face) - 0.5) / 20
	}
}

func sampleCombatOrder() engine.CombatOrder {
	order := engine.RollInitiative([]engine.CombatantInput{
		{Name: "Герой", Side: engine.SideParty, HP: ptr(14), AC: ptr(16)},
		{Name: "Гоблин", Side: engine.SideEnemy, HP: ptr(7), AC: ptr(12)},
	}, combatSequence(18, 9))
	return engine.CombatOrder{Started: true, Round: 2, Current: 0, Acted: false, Order: order}
}

func TestCombatStateSaveLoad(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "persist-campaign")

	loaded, err := store.LoadCombatState(campaign.Slug)
	if err != nil || loaded != nil {
		t.Fatalf("пустой снимок = %+v, %v", loaded, err)
	}

	combat := sampleCombatOrder()
	enemies := []SerializedEnemy{{ID: "goblin", Name: "Гоблин", HP: 5, AC: 12}}
	if err := store.SaveCombatState(campaign.Slug, CombatSnapshot{Combat: combat, Enemies: enemies}); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.LoadCombatState(campaign.Slug)
	if err != nil || loaded == nil {
		t.Fatalf("load = %+v, %v", loaded, err)
	}
	if len(loaded.Combat.Order) != 2 || loaded.Combat.Order[0].Name != combat.Order[0].Name {
		t.Fatalf("combat = %+v", loaded.Combat)
	}
	if len(loaded.Enemies) != 1 || loaded.Enemies[0].ID != "goblin" || loaded.Enemies[0].HP != 5 {
		t.Fatalf("enemies = %+v", loaded.Enemies)
	}

	if err := store.ClearCombatState(campaign.Slug); err != nil {
		t.Fatal(err)
	}
	if loaded, _ := store.LoadCombatState(campaign.Slug); loaded != nil {
		t.Fatalf("после clear = %+v", loaded)
	}
	// Повторный clear не падает.
	if err := store.ClearCombatState(campaign.Slug); err != nil {
		t.Fatal(err)
	}
}

func TestCombatStateRejectsInactive(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "persist-campaign")

	_ = store.SaveCombatState(campaign.Slug, CombatSnapshot{
		Combat:  engine.EmptyCombatOrder(),
		Enemies: []SerializedEnemy{{ID: "e", Name: "E", HP: 1, AC: 10}},
	})
	if loaded, _ := store.LoadCombatState(campaign.Slug); loaded != nil {
		t.Fatalf("не начатый бой = %+v", loaded)
	}

	_ = store.SaveCombatState(campaign.Slug, CombatSnapshot{Combat: sampleCombatOrder(), Enemies: []SerializedEnemy{}})
	if loaded, _ := store.LoadCombatState(campaign.Slug); loaded != nil {
		t.Fatalf("без врагов = %+v", loaded)
	}
}

func TestCombatStateCorruptedJSON(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "persist-campaign")

	_ = store.SaveCombatState(campaign.Slug, CombatSnapshot{
		Combat:  sampleCombatOrder(),
		Enemies: []SerializedEnemy{{ID: "g", Name: "G", HP: 1, AC: 10}},
	})
	if _, err := store.db.Exec(
		"UPDATE combat_snapshot SET combat = 'not json' WHERE campaign_id = ?", campaign.ID,
	); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.LoadCombatState(campaign.Slug); err != nil || loaded != nil {
		t.Fatalf("битый JSON = %+v, %v", loaded, err)
	}
}

func TestCombatStateUnknownCampaign(t *testing.T) {
	store := newTestStore(t)
	if loaded, err := store.LoadCombatState("no-such-campaign"); err != nil || loaded != nil {
		t.Fatalf("load = %+v, %v", loaded, err)
	}
	if err := store.SaveCombatState("no-such-campaign", CombatSnapshot{
		Combat: sampleCombatOrder(), Enemies: []SerializedEnemy{{ID: "e", Name: "E", HP: 1, AC: 10}},
	}); err != nil {
		t.Fatal(err)
	}
	if loaded, _ := store.LoadCombatState("no-such-campaign"); loaded != nil {
		t.Fatalf("unknown = %+v", loaded)
	}
	if err := store.ClearCombatState("no-such-campaign"); err != nil {
		t.Fatal(err)
	}
}
