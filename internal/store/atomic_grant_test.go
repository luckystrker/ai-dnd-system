package store

import (
	"strings"
	"testing"
)

// atomicFixture — кампания с одним персонажем и днём 1.
func atomicFixture(t *testing.T) (*Store, *Campaign, *CharacterSheet) {
	t.Helper()
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Атомарность")
	if _, err := store.AddMember(campaign.ID, "u-dm", NewMemberInput{UserID: "u-player"}); err != nil {
		t.Fatal(err)
	}
	sheet, err := store.SaveCharacter(campaign.ID, "u-player", NewCharacterInput{
		Name: "Ария", CharacterClass: "воин", Race: "человек",
		HP: ptr(10), MaxHP: ptr(10),
	})
	if err != nil {
		t.Fatalf("SaveCharacter: %v", err)
	}
	return store, campaign, sheet
}

func reloadCharacter(t *testing.T, store *Store, campaign *Campaign, id string) CharacterSheet {
	t.Helper()
	sheets, err := store.ListCharacters(campaign.ID)
	if err != nil {
		t.Fatalf("ListCharacters: %v", err)
	}
	for _, sheet := range sheets {
		if sheet.ID == id {
			return sheet
		}
	}
	t.Fatalf("персонаж %s не найден", id)
	return CharacterSheet{}
}

// Уровень, способность, ключевое событие и транскрипт пишутся одной
// транзакцией: успешный вызов оставляет все четыре следа.
func TestLevelUpCharacterWritesAllEffects(t *testing.T) {
	t.Parallel()

	store, campaign, sheet := atomicFixture(t)
	ability := CharacterAbility{Name: "Второе дыхание", Description: "Лечение 1d10+1."}
	updated, err := store.LevelUpCharacter(campaign.ID, sheet.Name, 1, LevelUpInput{
		NewLevel: 2, MaxHP: ptr(16), Ability: &ability,
		KeyEventText: "Ария достигла 2 уровня.", KeyEventID: "levelup:" + sheet.ID + ":2",
		TranscriptText: "Ария достигла 2 уровня.",
	})
	if err != nil {
		t.Fatalf("LevelUpCharacter: %v", err)
	}
	if updated.Level != 2 || updated.MaxHP == nil || *updated.MaxHP != 16 {
		t.Fatalf("лист не обновлён: %+v", updated)
	}
	if len(updated.Abilities) != 1 || updated.Abilities[0].Name != "Второе дыхание" {
		t.Fatalf("способность не записана: %+v", updated.Abilities)
	}
	day, err := store.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	joined := strings.Join(day.Entries, "\n")
	if !strings.Contains(joined, "Ария достигла 2 уровня") {
		t.Fatalf("транскрипт не записан: %s", joined)
	}
	permanent, regular, err := store.SplitKeyEvents(campaign.Slug)
	if err != nil {
		t.Fatalf("SplitKeyEvents: %v", err)
	}
	events := strings.Join(append(permanent, regular...), "\n")
	if !strings.Contains(events, "Ария достигла 2 уровня") {
		t.Fatalf("ключевое событие не записано: %s", events)
	}
}

// Сбой на последнем шаге (ключевое событие) откатывает уровень, HP и
// способность: повтор после ошибки не получит conflict на полуприменённом
// состоянии.
func TestLevelUpCharacterRollsBackOnKeyEventFailure(t *testing.T) {
	t.Parallel()

	store, campaign, sheet := atomicFixture(t)
	if _, err := store.db.Exec(
		`CREATE TRIGGER fail_key_event BEFORE INSERT ON key_events
		 BEGIN SELECT RAISE(ABORT, 'boom'); END;`,
	); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	ability := CharacterAbility{Name: "Второе дыхание", Description: "Лечение."}
	_, err := store.LevelUpCharacter(campaign.ID, sheet.Name, 1, LevelUpInput{
		NewLevel: 2, MaxHP: ptr(16), Ability: &ability,
		KeyEventText: "Ария достигла 2 уровня.", KeyEventID: "levelup:rollback",
	})
	if err == nil {
		t.Fatal("ожидалась ошибка транзакции")
	}
	got := reloadCharacter(t, store, campaign, sheet.ID)
	if got.Level != 1 {
		t.Errorf("уровень после отката = %d, want 1", got.Level)
	}
	if got.MaxHP == nil || *got.MaxHP != 10 {
		t.Errorf("maxHP после отката = %v, want 10", got.MaxHP)
	}
	if len(got.Abilities) != 0 {
		t.Errorf("способность осталась после отката: %+v", got.Abilities)
	}
	day, err := store.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if day != nil && len(day.Entries) != 0 {
		t.Errorf("транскрипт остался после отката: %v", day.Entries)
	}
}

// Выдача предметов/золота и запись журнала — одна транзакция.
func TestGrantCharacterWithLedgerWritesSheetAndLedger(t *testing.T) {
	t.Parallel()

	store, campaign, sheet := atomicFixture(t)
	eventID := "grant:gold:1:50"
	updated, err := store.GrantCharacterWithLedger(campaign.ID, sheet.Name, CharacterGrantPatch{
		Inventory: []string{"верёвка"}, Gold: 50, XP: 20,
	}, []GrantLedgerRow{{
		Row:     LedgerRow{Day: 1, Type: LedgerFound, ItemOrGold: "50 золотых", By: ptr("Ария")},
		EventID: &eventID,
	}})
	if err != nil {
		t.Fatalf("GrantCharacterWithLedger: %v", err)
	}
	if updated.Gold == nil || *updated.Gold != 50 || updated.XP == nil || *updated.XP != 20 {
		t.Fatalf("ресурсы не выданы: %+v", updated)
	}
	if len(updated.Inventory) != 1 || updated.Inventory[0] != "верёвка" {
		t.Fatalf("инвентарь не выдан: %+v", updated.Inventory)
	}
	ledger, err := store.ReadLedger(campaign.Slug, nil, 0)
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	if len(ledger) != 1 || !strings.Contains(ledger[0], "50 золотых") {
		t.Fatalf("журнал не записан: %v", ledger)
	}
}

// Сбой записи журнала откатывает выдачу листа: предметы и золото не остаются
// без записи в журнале.
func TestGrantCharacterWithLedgerRollsBackOnLedgerFailure(t *testing.T) {
	t.Parallel()

	store, campaign, sheet := atomicFixture(t)
	if _, err := store.db.Exec(
		`CREATE TRIGGER fail_ledger BEFORE INSERT ON ledger_rows
		 BEGIN SELECT RAISE(ABORT, 'boom'); END;`,
	); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	eventID := "grant:gold:rollback"
	_, err := store.GrantCharacterWithLedger(campaign.ID, sheet.Name, CharacterGrantPatch{
		Inventory: []string{"верёвка"}, Gold: 50, XP: 20,
	}, []GrantLedgerRow{{
		Row:     LedgerRow{Day: 1, Type: LedgerFound, ItemOrGold: "50 золотых"},
		EventID: &eventID,
	}})
	if err == nil {
		t.Fatal("ожидалась ошибка транзакции")
	}
	got := reloadCharacter(t, store, campaign, sheet.ID)
	if got.Gold != nil && *got.Gold == 50 {
		t.Errorf("золото осталось после отката: %v", got.Gold)
	}
	if got.XP != nil && *got.XP == 20 {
		t.Errorf("XP остались после отката: %v", got.XP)
	}
	if len(got.Inventory) != 0 {
		t.Errorf("инвентарь остался после отката: %v", got.Inventory)
	}
}

// Повторная выдача с тем же eventID не дублирует журнал (дедуп на уровне
// таблицы), но лист обновляется один раз за вызов.
func TestGrantCharacterWithLedgerDeduplicatesLedger(t *testing.T) {
	t.Parallel()

	store, campaign, sheet := atomicFixture(t)
	eventID := "grant:gold:dup"
	ledger := []GrantLedgerRow{{
		Row:     LedgerRow{Day: 1, Type: LedgerFound, ItemOrGold: "10 золотых"},
		EventID: &eventID,
	}}
	for i := 0; i < 2; i++ {
		if _, err := store.GrantCharacterWithLedger(campaign.ID, sheet.Name, CharacterGrantPatch{
			Gold: 10,
		}, ledger); err != nil {
			t.Fatalf("выдача %d: %v", i+1, err)
		}
	}
	rows, err := store.ReadLedger(campaign.Slug, nil, 0)
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("журнал задублирован: %v", rows)
	}
}
