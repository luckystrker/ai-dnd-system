package tools

import (
	"path/filepath"
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/engine"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// randomSeq повторяет последнее значение после исчерпания — как постоянная
// лямбда в TS-тестах.
func randomSeq(values ...float64) engine.RandomSource {
	index := 0
	return func() float64 {
		if len(values) == 0 {
			return 0
		}
		if index >= len(values) {
			return values[len(values)-1]
		}
		value := values[index]
		index++
		return value
	}
}

func d20Face(face int) float64 { return (float64(face) - 0.5) / 20 }

type testEnv struct {
	deps     Deps
	store    *store.Store
	campaign *store.Campaign
	dm       *Session
	player   *Session
	outsider *Session
	sheet    *store.CharacterSheet
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "campaigns.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	campaign, err := db.CreateCampaign(store.NewCampaignInput{
		Title: "Тестовая кампания", Length: store.LengthMedium, Setting: "Лес", Theme: "хоррор",
	}, store.NewOwnerInput{UserID: "u-dm", Name: ptr("Ведущий")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BindAndActivate(campaign.ID, "u-dm", store.BoundChat{ChatID: "chat-1", MessageThreadID: ptr(7)}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddMember(campaign.ID, "u-dm", store.NewMemberInput{UserID: "u-player", Name: ptr("Игрок")}); err != nil {
		t.Fatal(err)
	}
	sheet, err := db.SaveCharacter(campaign.ID, "u-player", store.NewCharacterInput{
		Name: "Ария", CharacterClass: "плут", Race: "эльф", Level: ptr(1),
		Stats:      map[string]float64{"dex": 16, "str": 12, "con": 14},
		Appearance: ptr("silver-haired elf rogue"),
		MaxHP:      ptr(10), HP: ptr(10),
		Equipment: []string{"короткий меч (1d6)"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{
		deps:     Deps{DB: db, Dice: NewDiceHistory()},
		store:    db,
		campaign: campaign,
		dm: &Session{CampaignID: campaign.ID, CampaignSlug: campaign.Slug, UserID: "u-dm",
			ChatID: "chat-1", MessageThreadID: ptr(7)},
		player: &Session{CampaignID: campaign.ID, CampaignSlug: campaign.Slug, UserID: "u-player",
			ChatID: "chat-1", MessageThreadID: ptr(7)},
		outsider: &Session{CampaignID: campaign.ID, CampaignSlug: campaign.Slug, UserID: "u-outsider",
			ChatID: "chat-1"},
		sheet: sheet,
	}
}

func wantToolCode(t *testing.T, err error, code ErrorCode) *ToolError {
	t.Helper()
	toolErr, ok := err.(*ToolError)
	if !ok {
		t.Fatalf("ожидалась *ToolError %q, получено %T %v", code, err, err)
	}
	if toolErr.Code != code {
		t.Fatalf("code = %q, want %q (%s)", toolErr.Code, code, toolErr.Message)
	}
	return toolErr
}
