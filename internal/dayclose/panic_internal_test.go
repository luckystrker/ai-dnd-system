package dayclose

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/chronicler"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

type panicArchivist struct{}

func (panicArchivist) CloseDay(context.Context, *store.Campaign, int, string) (chronicler.Report, error) {
	panic("archivist boom")
}

// process ловит панику воркера и всё равно снимает ключ дня: иначе день
// залипает в pending, а фоновая горутина роняет процесс (PLAN, находка ревью).
func TestProcessRecoversPanicAndReleasesKey(t *testing.T) {
	t.Parallel()

	st, err := store.Open(filepath.Join(t.TempDir(), "campaigns.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer func() { _ = st.Close() }()
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Паника", Length: store.LengthShort, Setting: "s", Theme: "t",
	}, store.NewOwnerInput{UserID: "dm-1"})
	if err != nil {
		t.Fatalf("campaign: %v", err)
	}
	if err := st.AppendTranscriptEntry(campaign.Slug, 1, store.TranscriptEntry{
		Kind: "dm", Text: "День прошёл.",
	}); err != nil {
		t.Fatalf("transcript: %v", err)
	}
	queue, err := New(Config{
		Store: st,
		NewArchivist: func(*store.Campaign) (Archivist, error) {
			return panicArchivist{}, nil
		},
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	key := campaign.ID + ":1"
	task := job{campaignID: campaign.ID, day: 1}
	queue.pending[key] = task

	queue.process(context.Background(), key, task) // не должно паниковать

	if _, exists := queue.pending[key]; exists {
		t.Fatal("ключ дня остался в pending после паники")
	}
}
