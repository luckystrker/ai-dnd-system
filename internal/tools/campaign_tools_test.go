package tools

import (
	"strings"
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func TestSaveCampaignAndStart(t *testing.T) {
	env := newTestEnv(t)
	fresh := &Session{UserID: "u-new", ChatID: "-500"}

	saved, err := env.deps.saveCampaign(fresh, saveCampaignArgs{
		Title: "Новая кампания", Length: "short", Setting: "Город", Theme: "детектив",
	})
	if err != nil {
		t.Fatalf("saveCampaign: %v", err)
	}
	campaignMap := saved.(map[string]any)["campaign"].(map[string]any)
	campaignID := campaignMap["id"].(string)

	started, err := env.deps.startCampaign(fresh, startCampaignArgs{CampaignID: campaignID})
	if err != nil {
		t.Fatalf("startCampaign: %v", err)
	}
	startedMap := started.(map[string]any)
	if startedMap["campaign"].(map[string]any)["status"] != "active" {
		t.Fatalf("started = %+v", startedMap)
	}
	bound, err := env.store.GetCampaign(campaignID)
	if err != nil || bound == nil || bound.BoundChat == nil || bound.BoundChat.ChatID != "-500" {
		t.Fatalf("campaign not bound: %+v, %v", bound, err)
	}
}

func TestSaveCampaignRequiresIdentity(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.deps.saveCampaign(&Session{}, saveCampaignArgs{
		Title: "X", Length: "short", Setting: "s", Theme: "t",
	})
	wantToolCode(t, err, CodeAccessDenied)
}

func TestPlayerCanWriteCampaignMemory(t *testing.T) {
	env := newTestEnv(t)

	updated, err := env.deps.updateCharacter(env.player, updateCharacterArgs{
		Character: "Ария", HP: ptr(6), Location: ptr("подвал"),
	})
	if err != nil {
		t.Fatalf("updateCharacter on player turn: %v", err)
	}
	character := updated.(map[string]any)["character"].(map[string]any)
	if character["hp"] != 6 {
		t.Fatalf("hp = %v", character["hp"])
	}
	if _, err := env.deps.upsertNpc(env.player, upsertNpcArgs{
		Name: "Трактирщик", Role: ptr("трактирщик"),
		MemoryAppend: ptr("Игроки спасли его кошку."), MemoryAppendDay: ptr(1),
	}); err != nil {
		t.Fatalf("upsertNpc on player turn: %v", err)
	}
	npc, err := env.deps.getNpc(env.player, getNpcArgs{Name: "Трактирщик"})
	if err != nil {
		t.Fatalf("getNpc: %v", err)
	}
	memory := npc.(map[string]any)["npc"].(map[string]any)["memory"]
	if memory == nil || !strings.Contains(memory.(string), "спасли его кошку") {
		t.Fatalf("memory = %v", memory)
	}
	if _, err := env.deps.appendKeyEvent(env.player, appendKeyEventArgs{Event: "Партия спустилась в подвал."}); err != nil {
		t.Fatalf("appendKeyEvent: %v", err)
	}
	if _, err := env.deps.createQuest(env.player, createQuestArgs{
		Title: "Спасти кошку", Objective: "Найти кошку", Difficulty: "easy",
	}); err != nil {
		t.Fatalf("createQuest: %v", err)
	}
}

func TestOutsiderDenied(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.deps.updateCharacter(env.outsider, updateCharacterArgs{Character: "Ария", HP: ptr(5)})
	wantToolCode(t, err, CodeAccessDenied)

	_, err = env.deps.appendKeyEvent(env.outsider, appendKeyEventArgs{Event: "Чужое событие"})
	wantToolCode(t, err, CodeAccessDenied)

	_, err = env.deps.upsertNpc(env.outsider, upsertNpcArgs{Name: "Чужой NPC"})
	wantToolCode(t, err, CodeAccessDenied)
}

func TestAdminOperationsRequireDM(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.deps.inviteMember(env.player, inviteMemberArgs{CampaignID: env.campaign.ID, UserID: "u-x"})
	wantToolCode(t, err, CodeAccessDenied)

	_, err = env.deps.finishCampaign(env.player, finishCampaignArgs{})
	wantToolCode(t, err, CodeAccessDenied)

	// start_campaign для чужой setup-кампании: store проверяет dm.
	other, err := env.store.CreateCampaign(store.NewCampaignInput{
		Title: "Чужая", Length: store.LengthShort, Setting: "s", Theme: "t",
	}, store.NewOwnerInput{UserID: "u-other"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.deps.startCampaign(env.player, startCampaignArgs{CampaignID: other.ID})
	wantToolCode(t, err, CodeAccessDenied)
}

func TestChroniclerTrustedWritesOnlyOwnCampaign(t *testing.T) {
	env := newTestEnv(t)
	chronicler := &Session{CampaignID: env.campaign.ID, CampaignSlug: env.campaign.Slug, Trusted: true}

	if _, err := env.deps.updateCharacter(chronicler, updateCharacterArgs{Character: "Ария", HP: ptr(9)}); err != nil {
		t.Fatalf("chronicler update: %v", err)
	}
	if _, err := env.deps.upsertNpc(chronicler, upsertNpcArgs{Name: "Свидетель", MemoryAppend: ptr("Видел партию.")}); err != nil {
		t.Fatalf("chronicler npc: %v", err)
	}

	// Вторая кампания содержит персонажа с тем же именем: запись из сессии A
	// не должна затронуть чужие данные.
	other, err := env.store.CreateCampaign(store.NewCampaignInput{
		Title: "Другая", Length: store.LengthShort, Setting: "s", Theme: "t",
	}, store.NewOwnerInput{UserID: "u-other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.SaveCharacter(other.ID, "u-other", store.NewCharacterInput{
		Name: "Ария", CharacterClass: "плут", Race: "эльф", Stats: map[string]float64{"dex": 10},
		MaxHP: ptr(10), HP: ptr(10),
	}); err != nil {
		t.Fatal(err)
	}
	otherSheet, _ := env.store.ListCharacters(other.ID)
	if _, err := env.deps.updateCharacter(chronicler, updateCharacterArgs{Character: "Ария", HP: ptr(9)}); err != nil {
		t.Fatalf("chronicler update: %v", err)
	}
	otherAfter, _ := env.store.ListCharacters(other.ID)
	if otherAfter[0].HP == nil || *otherAfter[0].HP != 10 || otherSheet[0].HP == nil {
		t.Fatalf("чужая кампания изменена: %+v", otherAfter[0])
	}
}

func TestGetCharacterOwnAndExplicit(t *testing.T) {
	env := newTestEnv(t)

	own, err := env.deps.getCharacter(env.player, getCharacterArgs{})
	if err != nil {
		t.Fatalf("getCharacter own: %v", err)
	}
	name := own.(map[string]any)["character"].(map[string]any)["name"]
	if name != "Ария" {
		t.Fatalf("name = %v", name)
	}
	explicit, err := env.deps.getCharacter(env.dm, getCharacterArgs{Character: ptr("ария")})
	if err != nil || explicit == nil {
		t.Fatalf("getCharacter explicit: %v", err)
	}
	_, err = env.deps.getCharacter(env.player, getCharacterArgs{Character: ptr("Нет такой")})
	wantToolCode(t, err, CodeNotFound)
}

func TestGrantCharacterWritesLedger(t *testing.T) {
	env := newTestEnv(t)

	granted, err := env.deps.grantCharacter(env.player, grantCharacterArgs{
		Character: "Ария", Items: []string{"зелье лечения"}, Gold: ptr(25), XP: ptr(100),
	})
	if err != nil {
		t.Fatalf("grantCharacter: %v", err)
	}
	character := granted.(map[string]any)["character"].(map[string]any)
	if character["gold"] != 25 || character["xp"] != 100 {
		t.Fatalf("character = %+v", character)
	}
	ledger, err := env.store.ReadLedger(env.campaign.Slug, nil, 0)
	if err != nil || len(ledger) != 2 {
		t.Fatalf("ledger = %v, %v", ledger, err)
	}
}

func TestGetGameContextNoCampaign(t *testing.T) {
	env := newTestEnv(t)
	result, err := env.deps.getGameContext(&Session{UserID: "u-none", ChatID: "-999"})
	if err != nil {
		t.Fatalf("getGameContext: %v", err)
	}
	if result.(map[string]any)["campaign"] != nil {
		t.Fatalf("campaign = %v", result)
	}
}
