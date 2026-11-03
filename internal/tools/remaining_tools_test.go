package tools

import (
	"strings"
	"testing"
)

func TestSaveCharacterAndListCampaigns(t *testing.T) {
	env := newTestEnv(t)

	created, err := env.deps.saveCharacter(env.dm, saveCharacterArgs{
		CampaignID: env.campaign.ID, Name: "Борис", CharacterClass: "воин", Race: "человек",
		Stats: map[string]int{"str": 16}, MaxHP: ptr(12),
	})
	if err != nil {
		t.Fatalf("saveCharacter: %v", err)
	}
	character := created.(map[string]any)["character"].(map[string]any)
	if character["name"] != "Борис" || character["hp"] != 12 {
		t.Fatalf("character = %+v", character)
	}

	list, err := env.deps.listCampaigns(env.player)
	if err != nil {
		t.Fatalf("listCampaigns: %v", err)
	}
	campaigns := list.(map[string]any)["campaigns"].([]map[string]any)
	if len(campaigns) != 1 || campaigns[0]["slug"] != env.campaign.Slug {
		t.Fatalf("campaigns = %+v", campaigns)
	}
	if campaigns[0]["characterCount"] != 2 {
		t.Fatalf("characterCount = %v", campaigns[0]["characterCount"])
	}
}

func TestInviteAndFinishCampaign(t *testing.T) {
	env := newTestEnv(t)

	invited, err := env.deps.inviteMember(env.dm, inviteMemberArgs{
		CampaignID: env.campaign.ID, UserID: "u-new", Name: ptr("Новый"),
	})
	if err != nil {
		t.Fatalf("inviteMember: %v", err)
	}
	if invited.(map[string]any)["memberCount"] != 3 {
		t.Fatalf("invite = %+v", invited)
	}
	finished, err := env.deps.finishCampaign(env.dm, finishCampaignArgs{})
	if err != nil {
		t.Fatalf("finishCampaign: %v", err)
	}
	if finished.(map[string]any)["campaign"].(map[string]any)["status"] != "finished" {
		t.Fatalf("finished = %+v", finished)
	}
}

func TestListQuestsNpcsAndLocations(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.deps.createQuest(env.player, createQuestArgs{
		Title: "Квест", Objective: "Сделать", Difficulty: "medium",
	}); err != nil {
		t.Fatal(err)
	}
	quests, err := env.deps.listQuests(env.player, listQuestsArgs{})
	if err != nil || len(quests.(map[string]any)["quests"].([]map[string]any)) != 1 {
		t.Fatalf("quests = %+v, %v", quests, err)
	}
	all, err := env.deps.listQuests(env.player, listQuestsArgs{IncludeAll: ptr(true)})
	if err != nil || len(all.(map[string]any)["quests"].([]map[string]any)) != 1 {
		t.Fatalf("all = %+v, %v", all, err)
	}

	if _, err := env.deps.upsertNpc(env.player, upsertNpcArgs{Name: "Борн", Role: ptr("трактирщик")}); err != nil {
		t.Fatal(err)
	}
	npcs, err := env.deps.listNpcs(env.player)
	if err != nil || npcs.(map[string]any)["count"] != 1 {
		t.Fatalf("npcs = %+v, %v", npcs, err)
	}

	if _, err := env.deps.upsertLocation(env.player, upsertLocationArgs{Name: "Мораг", DiscoveredDay: ptr(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.deps.upsertLocation(env.player, upsertLocationArgs{Name: "Тайник"}); err != nil {
		t.Fatal(err)
	}
	locations, err := env.deps.listLocations(env.player, listLocationsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if locations.(map[string]any)["count"] != 1 {
		t.Fatalf("locations = %+v", locations)
	}
	allLocations, err := env.deps.listLocations(env.player, listLocationsArgs{IncludeUndiscovered: ptr(true)})
	if err != nil || allLocations.(map[string]any)["count"] != 2 {
		t.Fatalf("all locations = %+v, %v", allLocations, err)
	}
}

func TestRecordWorldChange(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.deps.recordWorldChange(env.player, recordWorldChangeArgs{
		Category: "Решения", Text: "Партия пощадила гоблина.", Day: ptr(2),
	}); err != nil {
		t.Fatalf("recordWorldChange: %v", err)
	}
	state, err := env.deps.listWorldState(env.player)
	if err != nil {
		t.Fatal(err)
	}
	rendered := state.(map[string]any)["state"].(string)
	if !strings.Contains(rendered, "## Решения") || !strings.Contains(rendered, "Партия пощадила гоблина. (день 2)") {
		t.Fatalf("state = %q", rendered)
	}
}
