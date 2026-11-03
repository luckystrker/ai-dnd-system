package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "campaigns.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func dmOwner() NewOwnerInput {
	return NewOwnerInput{UserID: "u-dm", Name: ptr("Ведущий"), Username: ptr("dm")}
}

func newCampaign(t *testing.T, store *Store, title string) *Campaign {
	t.Helper()
	campaign, err := store.CreateCampaign(NewCampaignInput{
		Title: title, Length: LengthMedium, Setting: "Нуарный город", Theme: "детектив",
	}, dmOwner())
	if err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}
	return campaign
}

func wantCode(t *testing.T, err error, code StoreErrorCode) {
	t.Helper()
	var storeErr *StoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("ожидалась StoreError %q, получено %v", code, err)
	}
	if storeErr.Code != code {
		t.Fatalf("код ошибки = %q, want %q (%s)", storeErr.Code, code, storeErr.Message)
	}
}

func TestCreateCampaignPersistsMembersAndDescription(t *testing.T) {
	store := newTestStore(t)
	campaign, err := store.CreateCampaign(NewCampaignInput{
		Title: "Описание в БД", Length: LengthLong, Setting: "s", Theme: "t", Description: "Вступление...",
	}, dmOwner())
	if err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}
	if campaign.Status != StatusSetup {
		t.Errorf("status = %q", campaign.Status)
	}
	if len(campaign.Members) != 1 || campaign.Members[0].Role != RoleDM {
		t.Fatalf("members = %+v", campaign.Members)
	}
	description, err := store.ReadDescription(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if description != "Вступление..." {
		t.Errorf("description = %q", description)
	}
}

func TestGetCampaignByIDAndSlug(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Тени старого города")
	for _, key := range []string{campaign.ID, campaign.Slug} {
		found, err := store.GetCampaign(key)
		if err != nil || found == nil {
			t.Fatalf("GetCampaign(%q) = %v, %v", key, found, err)
		}
		if found.ID != campaign.ID {
			t.Errorf("id = %q", found.ID)
		}
	}
	missing, err := store.GetCampaign("nope")
	if err != nil || missing != nil {
		t.Fatalf("неизвестная кампания: %v, %v", missing, err)
	}
}

func TestBindAndActivateNullSafeOnMessageThread(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Топики")
	if _, err := store.BindAndActivate(campaign.ID, "u-dm", BoundChat{ChatID: "-100"}); err != nil {
		t.Fatal(err)
	}
	found, _ := store.FindByBoundChat("-100", nil, false)
	if found == nil || found.ID != campaign.ID {
		t.Fatalf("кампания без топика не найдена: %+v", found)
	}
	if found, _ := store.FindByBoundChat("-100", ptr(5), false); found != nil {
		t.Fatalf("кампания с топиком не должна находиться без топика: %+v", found)
	}
	if found, _ := store.FindByBoundChat("-101", nil, false); found != nil {
		t.Fatalf("чужой чат: %+v", found)
	}
}

func TestBindAndActivateFindsThreadCampaign(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Форум")
	if _, err := store.BindAndActivate(campaign.ID, "u-dm", BoundChat{ChatID: "-100", MessageThreadID: ptr(7)}); err != nil {
		t.Fatal(err)
	}
	found, _ := store.FindByBoundChat("-100", ptr(7), false)
	if found == nil || found.ID != campaign.ID {
		t.Fatalf("топик не найден: %+v", found)
	}
	if found, _ := store.FindByBoundChat("-100", nil, false); found != nil {
		t.Fatalf("без топика не должно находиться: %+v", found)
	}
}

func TestAdvanceDayAndEnvironment(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Дни")
	if _, err := store.BindAndActivate(campaign.ID, "u-dm", BoundChat{ChatID: "-1"}); err != nil {
		t.Fatal(err)
	}
	advanced, err := store.AdvanceDay(campaign.ID, "u-dm")
	if err != nil || advanced.CurrentDay == nil || *advanced.CurrentDay != 2 {
		t.Fatalf("AdvanceDay = %+v, %v", advanced, err)
	}
	updated, err := store.SetEnvironment(campaign.ID, EnvironmentPatch{
		TimeOfDay: ptr(TimeEvening), Weather: ptr("дождь"), InGameDate: ptr("7 Месяца Туманов"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TimeOfDay == nil || *updated.TimeOfDay != TimeEvening || updated.Weather == nil || *updated.Weather != "дождь" {
		t.Fatalf("окружение = %+v", updated)
	}
	if _, err := store.SetEnvironment(campaign.ID, EnvironmentPatch{TimeOfDay: ptr(TimeNight)}); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := store.GetCampaign(campaign.ID)
	if reloaded.TimeOfDay == nil || *reloaded.TimeOfDay != TimeNight {
		t.Fatalf("timeOfDay = %+v", reloaded.TimeOfDay)
	}
	advanced, _ = store.AdvanceDay(campaign.ID, "u-dm")
	if advanced.TimeOfDay == nil || *advanced.TimeOfDay != TimeMorning {
		t.Fatalf("advance_day должен сбрасывать утро: %+v", advanced.TimeOfDay)
	}
}

func TestAddMemberAndAutoRegister(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Участники")
	if _, err := store.AddMember(campaign.ID, "u-dm", NewMemberInput{UserID: "u1"}); err != nil {
		t.Fatal(err)
	}
	_, err := store.AddMember(campaign.ID, "u-dm", NewMemberInput{UserID: "u1"})
	wantCode(t, err, ErrDuplicate)

	updated, err := store.AutoRegister(campaign.ID, NewMemberInput{UserID: "u2", Role: ptr(RoleDM)})
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range updated.Members {
		if member.UserID == "u2" && member.Role != RolePlayer {
			t.Fatalf("autoRegister должен понижать до player: %+v", member)
		}
	}
}

func TestSaveCharacterAndUpdateRoundTrip(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Персонажи")
	if _, err := store.AddMember(campaign.ID, "u-dm", NewMemberInput{UserID: "u-player"}); err != nil {
		t.Fatal(err)
	}
	sheet, err := store.SaveCharacter(campaign.ID, "u-player", NewCharacterInput{
		Name: "Ария", CharacterClass: "плут", Race: "эльфийка",
		Stats: map[string]float64{"dex": 18}, Appearance: ptr("Silver hair"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sheet.Slug != "ariya" {
		t.Errorf("slug = %q", sheet.Slug)
	}
	updated, err := store.UpdateCharacter(campaign.ID, "Ария", CharacterStatePatch{
		HP: ptr(12), Conditions: &[]string{"poisoned"}, Inventory: &[]string{"кинжал", "верёвка"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.HP == nil || *updated.HP != 12 || len(updated.Conditions) != 1 || len(updated.Inventory) != 2 {
		t.Fatalf("updated = %+v", updated)
	}
	sheets, err := store.ListCharacters(campaign.ID)
	if err != nil || len(sheets) != 1 {
		t.Fatalf("ListCharacters = %v, %v", sheets, err)
	}
	if sheets[0].Appearance == nil || *sheets[0].Appearance != "Silver hair" {
		t.Errorf("appearance = %+v", sheets[0].Appearance)
	}
	if sheets[0].Stats["dex"] != 18 {
		t.Errorf("stats = %v", sheets[0].Stats)
	}
}

func TestSaveCharacterStoresAbilitiesAndGrantAppends(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Персонажи старт")
	if _, err := store.AddMember(campaign.ID, "u-dm", NewMemberInput{UserID: "u-player"}); err != nil {
		t.Fatal(err)
	}
	sheet, err := store.SaveCharacter(campaign.ID, "u-player", NewCharacterInput{
		Name: "Мирин", CharacterClass: "волшебник", Race: "человек",
		Equipment: []string{"посох", "гримуар", "фонарь"},
		Abilities: []CharacterAbility{
			{Name: "Огненный снаряд", Description: "1к10 урона огнём на дистанции."},
			{Name: "Волшебная стрела", Description: "3 снаряда, гарантированное попадание.", Level: ptr(1)},
		},
		Gold: ptr(15), MaxHP: ptr(7),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sheet.Inventory) != 3 || len(sheet.Abilities) != 2 || *sheet.Gold != 15 || *sheet.HP != 7 || *sheet.MaxHP != 7 {
		t.Fatalf("sheet = %+v", sheet)
	}
	if sheet.Abilities[1].Level == nil || *sheet.Abilities[1].Level != 1 {
		t.Fatalf("level ability = %+v", sheet.Abilities[1])
	}
	granted, err := store.GrantCharacter(campaign.ID, "Мирин", CharacterGrantPatch{
		Inventory: []string{"зелье лечения"},
		Abilities: []CharacterAbility{{Name: "Щит", Description: "Реакция: +2 к КД до конца хода."}},
		Gold:      40, XP: 250,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(granted.Inventory) != 4 || len(granted.Abilities) != 3 || *granted.Gold != 55 || *granted.XP != 250 {
		t.Fatalf("granted = %+v", granted)
	}
	// Повторная выдача той же способности не дублирует её.
	granted, _ = store.GrantCharacter(campaign.ID, "Мирин", CharacterGrantPatch{
		Abilities: []CharacterAbility{{Name: "щит", Description: "Дубль."}},
	})
	if len(granted.Abilities) != 3 {
		t.Fatalf("способность задублировалась: %+v", granted.Abilities)
	}
}

func TestFinishCampaignFreesChat(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Финал")
	if _, err := store.BindAndActivate(campaign.ID, "u-dm", BoundChat{ChatID: "-99"}); err != nil {
		t.Fatal(err)
	}
	finished, err := store.FinishCampaign(campaign.ID, "u-dm")
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != StatusFinished || finished.BoundChat != nil {
		t.Fatalf("finished = %+v", finished)
	}
	if found, _ := store.FindByBoundChat("-99", nil, false); found != nil {
		t.Fatalf("чат не освободился: %+v", found)
	}
	replacement := newCampaign(t, store, "Следующая")
	rebound, err := store.BindAndActivate(replacement.ID, "u-dm", BoundChat{ChatID: "-99"})
	if err != nil || rebound.Status != StatusActive {
		t.Fatalf("rebind = %+v, %v", rebound, err)
	}
	_, err = store.FinishCampaign(campaign.ID, "u-player")
	wantCode(t, err, ErrAccessDenied)
}

func TestRoleEnforcement(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Права")
	if _, err := store.AddMember(campaign.ID, "u-dm", NewMemberInput{UserID: "u-player"}); err != nil {
		t.Fatal(err)
	}
	_, err := store.AdvanceDay(campaign.ID, "u-player")
	wantCode(t, err, ErrAccessDenied)
	_, err = store.SaveCharacter(campaign.ID, "u-outsider", NewCharacterInput{Name: "X", CharacterClass: "y", Race: "z"})
	wantCode(t, err, ErrAccessDenied)
}

func TestSecondStoreSeesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	campaign, err := writer.CreateCampaign(NewCampaignInput{
		Title: "Общая база", Length: LengthShort, Setting: "s", Theme: "t",
	}, dmOwner())
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	found, err := reader.GetCampaign(campaign.Slug)
	if err != nil || found == nil || found.ID != campaign.ID {
		t.Fatalf("reader = %+v, %v", found, err)
	}
}

func TestQuestsAndThreads(t *testing.T) {
	store := newTestStore(t)
	campaign := newCampaign(t, store, "Квесты")
	quest, err := store.CreateQuest(campaign.ID, NewQuestInput{
		Title: "Амулет для кузнеца", Objective: "Найти амулет", Difficulty: QuestEasy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if quest.Slug != "amulet-dlya-kuznetsa" || quest.CreatedDay != 1 || quest.Status != QuestOffered {
		t.Fatalf("quest = %+v", quest)
	}
	_, err = store.CreateQuest(campaign.ID, NewQuestInput{Title: "амулет для кузнеца", Objective: "x", Difficulty: QuestEasy})
	wantCode(t, err, ErrDuplicate)

	updated, err := store.UpdateQuest(campaign.ID, quest.Slug, QuestPatch{Status: ptr(QuestActive), Title: ptr("Амулет кузнеца")})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != QuestActive || updated.Title != "Амулет кузнеца" {
		t.Fatalf("updated quest = %+v", updated)
	}
	_, err = store.UpdateQuest(campaign.ID, quest.ID, QuestPatch{Status: ptr(QuestCompleted)})
	wantCode(t, err, ErrConflict)

	thread, err := store.AppendThread(campaign.ID, NewThreadInput{Text: "  Обещали вернуть долг.  "})
	if err != nil {
		t.Fatal(err)
	}
	if thread.Text != "Обещали вернуть долг." || thread.Kind != ThreadUnresolved || thread.Status != "open" {
		t.Fatalf("thread = %+v", thread)
	}
	resolved, err := store.ResolveThread(campaign.ID, "вернуть долг", ptr(3))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != "resolved" || resolved.DayClosed == nil || *resolved.DayClosed != 3 {
		t.Fatalf("resolved = %+v", resolved)
	}
	_, err = store.ResolveThread(campaign.ID, "нет такой", nil)
	wantCode(t, err, ErrNotFound)
	threads, err := store.ListThreads(campaign.ID)
	if err != nil || len(threads) != 1 {
		t.Fatalf("threads = %v, %v", threads, err)
	}
}
