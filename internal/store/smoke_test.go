package store

import "testing"

func TestAssertCampaignSlug(t *testing.T) {
	valid := []string{"temyy-les", "campaign-2", "a", "abc123"}
	for _, slug := range valid {
		if err := AssertCampaignSlug(slug); err != nil {
			t.Errorf("AssertCampaignSlug(%q) = %v", slug, err)
		}
	}
	invalid := []string{"", "../etc/passwd", "UPPER", "two words", "-lead", "trail-", "a--b", string(make([]byte, 61))}
	for _, slug := range invalid {
		if err := AssertCampaignSlug(slug); err == nil {
			t.Errorf("AssertCampaignSlug(%q) должен падать", slug)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Тени старого города": "teni-starogo-goroda",
		"Тёмный лес":          "temnyy-les",
		"  Hello, World!  ":   "hello-world",
		"!!!":                 "untitled",
	}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSmokeCampaignFlow(t *testing.T) {
	store := newTestStore(t)
	campaign, err := store.CreateCampaign(NewCampaignInput{
		Title: "Проклятие ледяной ведьмы", Length: LengthMedium, Setting: "Лес", Theme: "хоррор",
	}, dmOwner())
	if err != nil {
		t.Fatal(err)
	}
	if campaign.Slug != "proklyatie-ledyanoy-vedmy" {
		t.Fatalf("slug = %q", campaign.Slug)
	}
	if _, err := store.BindAndActivate(campaign.ID, "u-dm", BoundChat{ChatID: "100"}); err != nil {
		t.Fatal(err)
	}
	bound, _ := store.FindByBoundChat("100", nil, true)
	if bound == nil || bound.ID != campaign.ID {
		t.Fatalf("bound = %+v", bound)
	}

	if _, err := store.AddMember(campaign.ID, "u-dm", NewMemberInput{UserID: "u-player", Username: ptr("hero")}); err != nil {
		t.Fatal(err)
	}
	sheet, err := store.SaveCharacter(campaign.ID, "u-player", NewCharacterInput{
		Name: "Торин Дубощит", CharacterClass: "воин", Race: "дварф",
		Stats: map[string]float64{"str": 16, "dex": 12}, MaxHP: ptr(12),
		Equipment: []string{"боевой топор (1d8)"}, Gold: ptr(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sheet.HP == nil || *sheet.HP != 12 {
		t.Fatalf("hp = %+v", sheet.HP)
	}
	granted, err := store.GrantCharacter(campaign.ID, "Торин Дубощит", CharacterGrantPatch{
		Gold: 5, XP: 50, Conditions: []string{"вдохновлён"},
	})
	if err != nil {
		t.Fatalf("GrantCharacter: %v", err)
	}
	if granted.Gold == nil || *granted.Gold != 15 || granted.XP == nil || *granted.XP != 50 || len(granted.Conditions) != 1 {
		t.Fatalf("granted = %+v", granted)
	}

	quest, err := store.CreateQuest(campaign.ID, NewQuestInput{
		Title: "Амулет для кузнеца", Objective: "Найти амулет", Difficulty: QuestEasy,
		GiverNpcSlug: ptr("kuznets"), RewardPlan: &QuestRewardPlan{XP: ptr(46), Gold: ptr(50)},
	})
	if err != nil || quest.Slug != "amulet-dlya-kuznetsa" {
		t.Fatalf("quest = %+v, %v", quest, err)
	}
	if _, err := store.UpdateQuest(campaign.ID, quest.Slug, QuestPatch{Status: ptr(QuestActive)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendThread(campaign.ID, NewThreadInput{Text: "Вернуть долг кузнецу", Kind: ptr(ThreadDebt)}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTranscriptEntry(campaign.Slug, 1, TranscriptEntry{Kind: "player", Author: ptr("hero"), Text: "Иду к кузнецу."}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDaySummary(campaign.Slug, 1, "Партия прибыла в деревню."); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertWorldChange(campaign.Slug, WorldChange{Category: "Погибшие", Text: "Ведьма", Day: ptr(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertLocation(campaign.Slug, UpsertLocationInput{Name: "Деревня", Current: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertFaction(campaign.Slug, UpsertFactionInput{Name: "Кузнецы", Standing: ptr(2)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNpc(campaign.Slug, NpcUpsertInput{Name: "Кузнец", Role: ptr("кузнец")}); err != nil {
		t.Fatal(err)
	}

	hits, err := store.SearchCampaignMemory(campaign.Slug, "кузнец", SearchOptions{})
	if err != nil || len(hits) == 0 {
		t.Fatalf("search = %+v, %v", hits, err)
	}
	if _, err := store.AdvanceDay(campaign.ID, "u-dm"); err != nil {
		t.Fatal(err)
	}
	finished, err := store.FinishCampaign(campaign.ID, "u-dm")
	if err != nil || finished.Status != StatusFinished {
		t.Fatalf("finished = %+v, %v", finished, err)
	}
}
