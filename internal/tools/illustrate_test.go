package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func TestRunwareClientGenerate(t *testing.T) {
	var requestBody []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"taskType":"imageInference","imageURL":"https://img.example/scene.png"}]}`))
	}))
	defer server.Close()

	client := NewRunwareClient(RunwareConfig{
		APIKey: "test-key", Endpoint: server.URL, Client: server.Client(),
		Model: "model-1", Width: 512, Height: 768,
	})
	url, err := client.Generate(context.Background(), "a dark tavern")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if url != "https://img.example/scene.png" {
		t.Fatalf("url = %q", url)
	}
	if len(requestBody) != 1 || requestBody[0]["positivePrompt"] != "a dark tavern" ||
		requestBody[0]["model"] != "model-1" || requestBody[0]["width"] != float64(512) {
		t.Fatalf("body = %+v", requestBody)
	}
}

func TestRunwareClientErrors(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":"boom"}`))
	}))
	defer failing.Close()
	client := NewRunwareClient(RunwareConfig{APIKey: "k", Endpoint: failing.URL, Client: failing.Client()})
	if _, err := client.Generate(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "Runware API error 500") {
		t.Fatalf("error = %v", err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"data":[]}`))
	}))
	defer empty.Close()
	client = NewRunwareClient(RunwareConfig{APIKey: "k", Endpoint: empty.URL, Client: empty.Client()})
	if _, err := client.Generate(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "imageURL") {
		t.Fatalf("error = %v", err)
	}
}

func TestScenePrompt(t *testing.T) {
	prompt := BuildScenePrompt(ScenePromptInput{
		SceneDescription: "A dark tavern",
		Appearances:      []string{"silver-haired elf"},
		Setting:          "Лес",
		Theme:            "хоррор",
	})
	want := "A dark tavern, silver-haired elf, Setting: Лес, Theme: хоррор, high fantasy, cinematic lighting, detailed illustration"
	if prompt != want {
		t.Fatalf("\n got %q\nwant %q", prompt, want)
	}
	sheets := []store.CharacterSheet{
		{Name: "Ария", Appearance: ptr("silver-haired elf")},
		{Name: "Без внешности"},
	}
	appearances := AppearancesForCharacters([]string{"ария", "без внешности", "нет"}, sheets)
	if len(appearances) != 1 || appearances[0] != "silver-haired elf" {
		t.Fatalf("appearances = %v", appearances)
	}
}

type fakeGenerator struct {
	url    string
	err    error
	prompt string
}

func (f *fakeGenerator) Generate(_ context.Context, prompt string) (string, error) {
	f.prompt = prompt
	return f.url, f.err
}

type fakePoster struct {
	chatID   string
	threadID *int
	url      string
	caption  string
	err      error
}

func (p *fakePoster) PostImage(_ context.Context, chatID string, threadID *int, imageURL, caption string) error {
	p.chatID = chatID
	p.threadID = threadID
	p.url = imageURL
	p.caption = caption
	return p.err
}

func TestIllustrateScene(t *testing.T) {
	env := newTestEnv(t)
	generator := &fakeGenerator{url: "https://img.example/s.png"}
	poster := &fakePoster{}
	env.deps.Images = generator
	env.deps.Poster = poster

	result, err := env.deps.illustrateScene(context.Background(), env.player, illustrateArgs{
		SceneDescription: "A dark tavern", Characters: []string{"Ария"},
	})
	if err != nil {
		t.Fatalf("illustrateScene: %v", err)
	}
	if result.(map[string]any)["imageUrl"] != "https://img.example/s.png" {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(generator.prompt, "silver-haired elf rogue") || !strings.Contains(generator.prompt, "Setting: Лес") {
		t.Fatalf("prompt = %q", generator.prompt)
	}
	if poster.chatID != "chat-1" || poster.url != "https://img.example/s.png" {
		t.Fatalf("poster = %+v", poster)
	}
	if poster.threadID == nil || *poster.threadID != 7 {
		t.Fatalf("thread = %+v", poster.threadID)
	}
}

func TestIllustrateSceneUnavailable(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.deps.illustrateScene(context.Background(), env.player, illustrateArgs{SceneDescription: "x"})
	wantToolCode(t, err, CodeUnavailable)

	env.deps.Images = &fakeGenerator{url: "x"}
	_, err = env.deps.illustrateScene(context.Background(), env.player, illustrateArgs{SceneDescription: "x"})
	wantToolCode(t, err, CodeUnavailable) // нет poster

	other, err := env.store.CreateCampaign(store.NewCampaignInput{
		Title: "Без чата", Length: store.LengthShort, Setting: "s", Theme: "t",
	}, store.NewOwnerInput{UserID: "u-dm"})
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{CampaignID: other.ID, CampaignSlug: other.Slug, UserID: "u-dm"}
	_, err = env.deps.illustrateScene(context.Background(), session, illustrateArgs{SceneDescription: "x"})
	wantToolCode(t, err, CodeUnavailable)
}
