// Команда e2e — Go-замена двух TS-скриптов (до слайса 8 — `scripts/e2e-*.ts`):
// гонит Telegram-апдейт через настоящий канал против fake Bot API.
//
// Офлайн (по умолчанию) модель скриптованная: проверяются HTTP-клиент,
// durable-inbox, очередь, гейт, DM, тулы save_campaign/start_campaign и
// отправка ответа. Живой режим (-live) идёт той же дорогой с настоящей
// моделью и требует LLM_API_KEY; кампания создаётся сразу в БД, потому что
// поведение модели на опроснике недетерминировано.
//
// Запуск:
//
//	go run ./cmd/e2e
//	go run ./cmd/e2e -live
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	agoconfig "github.com/luckystrker/ago/config"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/stream"
	agotest "github.com/luckystrker/ago/testing"

	"github.com/luckystriker/ai-dnd-system/internal/config"
	"github.com/luckystriker/ai-dnd-system/internal/dm"
	"github.com/luckystriker/ai-dnd-system/internal/store"
	"github.com/luckystriker/ai-dnd-system/internal/telegram"
	ttrpg "github.com/luckystriker/ai-dnd-system/internal/tools"
)

const (
	testChatID = -7770001
	testUserID = 9990001
	testSlug   = "testovyy-pohod"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("e2e", flag.ContinueOnError)
	live := fs.Bool("live", false, "живая модель (нужен LLM_API_KEY)")
	dbPath := fs.String("db", "", "путь к SQLite (по умолчанию временная)")
	timeout := fs.Duration("timeout", 180*time.Second, "сколько ждать ответа")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "ttrpg-e2e-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if *dbPath == "" {
		*dbPath = filepath.Join(dir, "campaigns.db")
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	api := newFakeAPI()
	server := httptest.NewServer(api)
	defer server.Close()

	client := telegram.NewClient("e2e-token", telegram.WithBaseURL(server.URL))
	modelFactory, cleanup, err := buildModelFactory(*live)
	if err != nil {
		return err
	}
	defer cleanup()

	channel, err := telegram.NewChannel(telegram.Config{
		API:            client,
		Store:          st,
		BotUsername:    "dnd_bot",
		CoalesceWindow: 50 * time.Millisecond,
		Limits:         telegram.Limits{ChatPerMinute: 1000, UserPerMinute: 1000, Window: time.Minute},
		PollTimeout:    time.Second,
		ErrorBackoff:   50 * time.Millisecond,
		NewDM: func(chatKey string, conversation stream.Stream, session func() *ttrpg.Session) (*dm.DM, error) {
			return dm.New(dm.Config{
				Model:       modelFactory(),
				Store:       st,
				Stream:      conversation,
				SessionFunc: session,
				ToolDeps:    ttrpg.Deps{DB: st},
			})
		},
	})
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- channel.Run(runCtx) }()

	var checks []check
	if *live {
		campaign, err := seedCampaign(st)
		if err != nil {
			return err
		}
		sendUpdate(api, "/newcampaign уже не нужен", 1)
		fmt.Fprintf(os.Stderr, "живой e2e: кампания %s привязана к %s\n", campaign.Slug, telegram.ChatIDString(testChatID))
		sendUpdate(api, "Дэн: Я вхожу в таверну и оглядываюсь. Что я вижу?", 2)
		reply := waitReply(api, 1, *timeout)
		checks = append(checks,
			check{"ответ отправлен", reply != ""},
			check{"ответ непустой", strings.TrimSpace(reply) != ""},
		)
	} else {
		sendUpdate(api, "/newcampaign Создай кампанию без вопросов: название «Тестовый поход», length short, setting «приграничье с гоблинами», theme «оборона деревни».", 1)
		if reply := waitReply(api, 1, *timeout); reply == "" {
			return errors.New("нет ответа на /newcampaign")
		}
		campaign, err := st.GetCampaign(testSlug)
		checks = append(checks,
			check{"save_campaign создал кампанию", err == nil && campaign != nil},
		)

		sendUpdate(api, "testovyy-pohod", 2)
		waitReply(api, 2, *timeout)
		active, _ := st.GetCampaign(testSlug)
		checks = append(checks,
			check{"start_campaign: status active", active != nil && active.Status == store.StatusActive},
			check{"start_campaign: привязана к чату", active != nil && active.BoundChat != nil &&
				active.BoundChat.ChatID == telegram.ChatIDString(testChatID)},
		)

		sendUpdate(api, "Дэн: Я вхожу в таверну и оглядываюсь. Что я вижу?", 3)
		reply := waitReply(api, 3, *timeout)
		checks = append(checks,
			check{"обычный ход: ответ отправлен", reply != ""},
			check{"обычный ход: ответ непустой", strings.TrimSpace(reply) != ""},
		)

		day, _ := st.ReadDay(testSlug, 1)
		checks = append(checks, check{"transcript дня записан", day != nil && len(day.Entries) >= 2})
	}

	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	var stopErr error
	if err := channel.Stop(stopCtx); err != nil {
		stopErr = err
	}
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "канал завершился: %v\n", err)
		}
	case <-stopCtx.Done():
	}

	failed := 0
	for _, item := range checks {
		if item.ok {
			fmt.Printf("ok   %s\n", item.name)
		} else {
			fmt.Printf("FAIL %s\n", item.name)
			failed++
		}
	}
	if stopErr != nil {
		fmt.Printf("FAIL остановка канала: %v\n", stopErr)
		failed++
	}
	if failed > 0 {
		return fmt.Errorf("%d проверок провалено", failed)
	}
	fmt.Println("e2e пройден")
	return nil
}

type check struct {
	name string
	ok   bool
}

// buildModelFactory возвращает фабрику модели: скрипт для офлайн-прогона или
// живую модель по env.
func buildModelFactory(live bool) (func() agoconfig.ModelConfig, func(), error) {
	if !live {
		testConfig := agotest.NewTestConfig(
			// /newcampaign → save_campaign.
			toolCallResponse("c1", "save_campaign",
				`{"title":"Тестовый поход","length":"short","setting":"приграничье с гоблинами","theme":"оборона деревни"}`),
			textResponse("Кампания «Тестовый поход» сохранена."),
			// start_campaign.
			toolCallResponse("c2", "start_campaign", `{"campaignId":"`+testSlug+`"}`),
			textResponse("Кампания запущена в этом чате."),
			// Обычный ход.
			textResponse("Внутри пахнет пивом и мокрой шерстью. У стойки дремлет трактирщик. Что делает Дэн?"),
		)
		return func() agoconfig.ModelConfig { return testConfig }, func() {}, nil
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	settings := cfg.LLM
	return func() agoconfig.ModelConfig {
		model, _, err := settings.SessionConfig("ttrpg-e2e").ModelConfig()
		if err != nil {
			return nil
		}
		return model
	}, func() {}, nil
}

func toolCallResponse(id, name, arguments string) *events.ModelResponse {
	return &events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{
		ID: id, Name: name, Arguments: arguments,
	}}}}
}

func textResponse(text string) *events.ModelResponse {
	return &events.ModelResponse{Message: &events.ModelMessage{Content: text}}
}

// fakeAPI — интерфейс Bot API поверх httptest: ровно те методы, которые зовёт
// канал.
type fakeAPI struct {
	mu       sync.Mutex
	updates  []telegram.RawUpdate
	sent     []string
	lastChat string
}

func newFakeAPI() *fakeAPI { return &fakeAPI{} }

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := r.URL.Path
	if index := strings.LastIndex(method, "/"); index >= 0 {
		method = method[index+1:]
	}
	switch method {
	case "getUpdates":
		f.handleGetUpdates(w, r)
	case "sendMessage":
		var payload struct {
			ChatID string `json:"chat_id"`
			Text   string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		f.mu.Lock()
		f.sent = append(f.sent, payload.Text)
		f.lastChat = payload.ChatID
		messageID := len(f.sent)
		f.mu.Unlock()
		writeOK(w, map[string]any{"message_id": messageID})
	case "sendChatAction", "deleteWebhook", "sendPhoto", "answerCallbackQuery":
		writeOK(w, true)
	case "getMe":
		writeOK(w, map[string]any{"id": 42, "is_bot": true, "username": "dnd_bot"})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAPI) handleGetUpdates(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Offset  int64 `json:"offset"`
		Timeout int   `json:"timeout"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	// Ждём не дольше 200 мс: тест не должен висеть на long-poll.
	time.Sleep(200 * time.Millisecond)
	f.mu.Lock()
	var result []json.RawMessage
	remaining := f.updates[:0]
	for _, update := range f.updates {
		if update.UpdateID < payload.Offset {
			continue
		}
		result = append(result, append(json.RawMessage(nil), update.Payload...))
		remaining = append(remaining, update)
	}
	f.updates = remaining
	f.mu.Unlock()
	writeOK(w, result)
}

func writeOK(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (f *fakeAPI) push(raw telegram.RawUpdate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, raw)
}

func (f *fakeAPI) replies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

func sendUpdate(api *fakeAPI, text string, updateID int64) {
	message := map[string]any{
		"message_id": updateID,
		"date":       time.Now().Unix(),
		"from": map[string]any{
			"id": testUserID, "is_bot": false, "first_name": "Smoke", "username": "smoke_e2e",
		},
		"chat": map[string]any{"id": testChatID, "type": "private"},
		"text": text,
	}
	payload, _ := json.Marshal(map[string]any{"update_id": updateID, "message": message})
	api.push(telegram.RawUpdate{UpdateID: updateID, Payload: payload})
}

func waitReply(api *fakeAPI, after int, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		replies := api.replies()
		if len(replies) >= after {
			return replies[after-1]
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ""
}

// seedCampaign создаёт активную кампанию для живого прогона: опросник
// создания проверяет не e2e канала, а промпт, и зависит от модели.
func seedCampaign(st *store.Store) (*store.Campaign, error) {
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Тестовый поход", Length: store.LengthShort,
		Setting: "приграничье с гоблинами", Theme: "оборона деревни",
	}, store.NewOwnerInput{UserID: strconv.Itoa(testUserID), Username: ptr("smoke_e2e")})
	if err != nil {
		return nil, err
	}
	if _, err := st.BindAndActivate(campaign.ID, strconv.Itoa(testUserID), store.BoundChat{ChatID: telegram.ChatIDString(testChatID)}); err != nil {
		return nil, err
	}
	hp := 12
	if _, err := st.SaveCharacter(campaign.ID, strconv.Itoa(testUserID), store.NewCharacterInput{
		Name: "Дэн", CharacterClass: "воин", Race: "человек", HP: &hp, MaxHP: &hp,
	}); err != nil {
		return nil, err
	}
	return campaign, nil
}

func ptr[T any](value T) *T { return &value }
