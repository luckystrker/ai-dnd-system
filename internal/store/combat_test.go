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

func TestApplyCombatSnapshotHPIsAtomic(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "persist-campaign")
	sheet, err := store.SaveCharacter(campaign.ID, "u-dm", NewCharacterInput{
		Name: "Ария", CharacterClass: "плуг", Race: "эльф", MaxHP: ptr(10), HP: ptr(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := CombatSnapshot{
		Combat: engine.CombatOrder{Started: true, Round: 1, Current: 0, Order: []engine.CombatantEntry{
			{ID: sheet.ID, Name: "Ария", Side: engine.SideParty, HP: ptr(10), MaxHP: ptr(10)},
		}},
		Enemies: []SerializedEnemy{{ID: "g", Name: "Гоблин", HP: 10, AC: 10}},
	}
	snapshot.Combat.Order[0].HP = ptr(6)
	if err := store.ApplyCombatSnapshotHP(campaign.Slug, "Ария", 6, snapshot); err != nil {
		t.Fatalf("ApplyCombatSnapshotHP: %v", err)
	}
	sheets, _ := store.ListCharacters(campaign.ID)
	if sheets[0].HP == nil || *sheets[0].HP != 6 {
		t.Fatalf("sheet hp = %+v", sheets[0].HP)
	}
	loaded, err := store.LoadCombatState(campaign.Slug)
	if err != nil || loaded == nil || loaded.Combat.Order[0].HP == nil || *loaded.Combat.Order[0].HP != 6 {
		t.Fatalf("snapshot = %+v, %v", loaded, err)
	}

	// Если запись снимка падает, HP листа должен откатиться.
	if _, err := store.db.Exec("DROP TABLE combat_snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyCombatSnapshotHP(campaign.Slug, "Ария", 2, snapshot); err == nil {
		t.Fatal("ожидалась ошибка записи снимка")
	}
	sheets, _ = store.ListCharacters(campaign.ID)
	if sheets[0].HP == nil || *sheets[0].HP != 6 {
		t.Fatalf("HP листа не откатился: %+v", sheets[0].HP)
	}
}
