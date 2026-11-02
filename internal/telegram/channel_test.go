package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	agoconfig "github.com/luckystrker/ago/config"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/stream"
	agotest "github.com/luckystrker/ago/testing"
	agotools "github.com/luckystrker/ago/tools"
	_ "modernc.org/sqlite"

	"github.com/luckystriker/ai-dnd-system/internal/dm"
	"github.com/luckystriker/ai-dnd-system/internal/store"
	ttrpg "github.com/luckystriker/ai-dnd-system/internal/tools"
)

type sentMessage struct {
	chatID   string
	threadID *int
	text     string
	photo    string
	caption  string
}

type fakeAPI struct {
	mu             sync.Mutex
	queue          chan []RawUpdate
	sent           []sentMessage
	photos         []sentMessage
	actions        []sentMessage
	callbacks      []string
	webhookDeleted bool
	files          map[string]FileInfo
	blobs          map[string][]byte
	lastOffset     int64
	getUpdates     int
}

func newFakeBotAPI() *fakeAPI {
	return &fakeAPI{
		queue: make(chan []RawUpdate, 16),
		files: map[string]FileInfo{},
		blobs: map[string][]byte{},
	}
}

func (f *fakeAPI) GetUpdates(ctx context.Context, offset int64, _ time.Duration, _ []string) ([]RawUpdate, error) {
	f.mu.Lock()
	f.getUpdates++
	f.lastOffset = offset
	f.mu.Unlock()
	select {
	case batch := <-f.queue:
		return batch, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeAPI) DeleteWebhook(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.webhookDeleted = true
	return nil
}

func (f *fakeAPI) SendMessage(_ context.Context, chatID string, threadID *int, text, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMessage{chatID: chatID, threadID: threadID, text: text})
	return nil
}

func (f *fakeAPI) SendRichText(_ context.Context, chatID string, threadID *int, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMessage{chatID: chatID, threadID: threadID, text: text})
	return nil
}

func (f *fakeAPI) SendChatAction(_ context.Context, chatID string, threadID *int, action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, sentMessage{chatID: chatID, threadID: threadID, text: action})
	return nil
}

func (f *fakeAPI) SendPhoto(_ context.Context, chatID string, threadID *int, photo, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.photos = append(f.photos, sentMessage{chatID: chatID, threadID: threadID, photo: photo, caption: caption})
	return nil
}

func (f *fakeAPI) GetFile(_ context.Context, fileID string) (FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.files[fileID]
	if !ok {
		return FileInfo{}, fmt.Errorf("unknown file %s", fileID)
	}
	return info, nil
}

func (f *fakeAPI) DownloadFile(_ context.Context, info FileInfo, max int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if info.FileSize > max {
		return nil, fmt.Errorf("file too large")
	}
	data, ok := f.blobs[info.FilePath]
	if !ok {
		return nil, fmt.Errorf("no blob %s", info.FilePath)
	}
	return data, nil
}

func (f *fakeAPI) AnswerCallbackQuery(_ context.Context, callbackID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callbacks = append(f.callbacks, callbackID+":"+text)
	return nil
}

func (f *fakeAPI) sentMessages() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sentMessage, len(f.sent))
	copy(out, f.sent)
	return out
}

func (f *fakeAPI) photoMessages() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sentMessage, len(f.photos))
	copy(out, f.photos)
	return out
}

func (f *fakeAPI) actionsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.actions)
}

func waitSent(t *testing.T, api *fakeAPI, count int) []sentMessage {
	t.Helper()
	waitFor(t, func() bool { return len(api.sentMessages()) >= count })
	return api.sentMessages()
}

func testCampaign(t *testing.T, st *store.Store, chatID string) *store.Campaign {
	t.Helper()
	return testCampaignOwnedBy(t, st, chatID, "dm-1")
}

func testCampaignOwnedBy(t *testing.T, st *store.Store, chatID, ownerID string) *store.Campaign {
	t.Helper()
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Три ворона", Length: store.LengthShort, Setting: "север", Theme: "выживание",
	}, store.NewOwnerInput{UserID: ownerID, Username: strPtr("dm")})
	if err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}
	if _, err := st.BindAndActivate(campaign.ID, ownerID, store.BoundChat{ChatID: chatID}); err != nil {
		t.Fatalf("BindAndActivate: %v", err)
	}
	return campaign
}

func testStorePath(t *testing.T) *store.Store {
	t.Helper()
	st, _ := testStoreFile(t)
	return st
}

func testStoreFile(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "campaigns.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, path
}

// modelFactory собирает DM на скриптованной модели и заданных ExtraTools.
func modelFactory(st *store.Store, model agoconfig.ModelConfig, extra ...agotools.Tool) DMFactory {
	return func(_ string, conversation stream.Stream, session func() *ttrpg.Session) (*dm.DM, error) {
		return dm.New(dm.Config{
			Model: model, Store: st, Stream: conversation, SessionFunc: session,
			SkipToolkit: true, ExtraTools: extra,
		})
	}
}

func newTestChannel(t *testing.T, api API, st *store.Store, factory DMFactory, mutate func(*Config)) *Channel {
	t.Helper()
	cfg := Config{
		API: api, Store: st, BotUsername: "dnd_bot",
		CoalesceWindow:     2 * time.Millisecond,
		Limits:             Limits{ChatPerMinute: 100, UserPerMinute: 100, Window: time.Minute},
		MaxAttachmentBytes: 1024, MaxUpdateBytes: 1 << 20,
		PollTimeout: 5 * time.Millisecond, ErrorBackoff: 5 * time.Millisecond,
		NewDM: factory,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	channel, err := NewChannel(cfg)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	t.Cleanup(func() { _ = channel.Stop(context.Background()) })
	return channel
}

func rawUpdate(t *testing.T, id int64, message *Message) RawUpdate {
	t.Helper()
	payload, err := json.Marshal(Update{UpdateID: id, Message: message})
	if err != nil {
		t.Fatal(err)
	}
	return RawUpdate{UpdateID: id, Payload: payload}
}

func privateMessage(userID int64, text string) *Message {
	message := topicMessage(int64(userID), "private", nil, false)
	message.From = &User{ID: userID, FirstName: fmt.Sprintf("u%d", userID), Username: fmt.Sprintf("u%d", userID)}
	message.Text = text
	return message
}

func groupMessage(chatID, userID int64, text string) *Message {
	message := topicMessage(chatID, "supergroup", nil, false)
	message.From = &User{ID: userID, FirstName: fmt.Sprintf("u%d", userID), Username: fmt.Sprintf("u%d", userID)}
	message.Text = text
	return message
}

// Сквозной тест: update → inbox → агент на скриптованной модели → тул →
// ответ в fake Bot API.
func TestChannelEndToEndToolAndReply(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	testCampaign(t, st, "-100")
	api := newFakeBotAPI()
	var toolCalls int32
	var mu sync.Mutex
	tick, err := agotools.NewFunctionTool("tick", "Ticks", func(args struct{}) (string, error) {
		mu.Lock()
		toolCalls++
		mu.Unlock()
		return "tock", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	model := agotest.NewTestConfig(
		&events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{ID: "c1", Name: "tick", Arguments: "{}"}}}},
		&events.ModelResponse{Message: &events.ModelMessage{Content: "**Дверь** со скрипом открывается."}},
	)
	channel := newTestChannel(t, api, st, modelFactory(st, model, tick), nil)
	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 100, "ДМ, я открываю дверь"))); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	sent := waitSent(t, api, 1)
	if sent[0].text != "**Дверь** со скрипом открывается." || sent[0].chatID != "-100" {
		t.Fatalf("ответ = %+v", sent[0])
	}
	mu.Lock()
	calls := toolCalls
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("вызовов тула = %d, want 1", calls)
	}
	if api.actionsCount() == 0 {
		t.Fatal("typing не отправлен")
	}
	pending, err := st.PendingTelegramUpdates()
	if err != nil || len(pending) != 0 {
		t.Fatalf("inbox после хода: %+v, %v", pending, err)
	}
}

// Дедуп update_id: повторная доставка не запускает второй ход.
func TestChannelDeduplicatesUpdateID(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	model := agotest.NewTestConfig(plainResponse("Ответ"))
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)
	raw := rawUpdate(t, 7, privateMessage(1, "привет"))
	for i := 0; i < 3; i++ {
		if err := channel.ingest(raw); err != nil {
			t.Fatalf("ingest %d: %v", i+1, err)
		}
	}
	waitSent(t, api, 1)
	time.Sleep(40 * time.Millisecond)
	if sent := api.sentMessages(); len(sent) != 1 {
		t.Fatalf("ответов = %d, want 1", len(sent))
	}
	if calls := len(model.Calls()); calls != 1 {
		t.Fatalf("вызовов модели = %d, want 1", calls)
	}
}

// Рестарт без потерь: необработанный апдейт из inbox дочитывается, offset
// продолжается с сохранённого значения.
func TestChannelRecoversUnprocessedInbox(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	raw := rawUpdate(t, 5, privateMessage(1, "висящее сообщение"))
	if _, err := st.SaveTelegramUpdate(5, "1:", string(raw.Payload)); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTelegramOffset(6); err != nil {
		t.Fatal(err)
	}
	model := agotest.NewTestConfig(plainResponse("Дочитано"))
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- channel.Run(ctx) }()
	sent := waitSent(t, api, 1)
	if sent[0].text != "Дочитано" {
		t.Fatalf("ответ = %+v", sent[0])
	}
	waitFor(t, func() bool {
		pending, _ := st.PendingTelegramUpdates()
		return len(pending) == 0
	})
	waitFor(t, func() bool {
		api.mu.Lock()
		defer api.mu.Unlock()
		return api.lastOffset == 6
	})
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run не остановился")
	}
}

// Топик форума — отдельная сессия и отдельный message_thread_id ответа;
// реплай в супергруппе (is_topic_message=false) остаётся в сессии чата.
func TestChannelTopicsAndReplies(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	model := agotest.NewTestConfig(plainResponse("Ответ топика"), plainResponse("Ответ реплая"))
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)

	topic := groupMessage(-100, 1, "ДМ, ход в топике")
	thread := 7
	topic.MessageThreadID = &thread
	topic.IsTopicMessage = true
	if err := channel.ingest(rawUpdate(t, 1, topic)); err != nil {
		t.Fatal(err)
	}
	waitSent(t, api, 1)

	reply := groupMessage(-100, 1, "ДМ, ход реплаем")
	parent := 4242
	reply.MessageThreadID = &parent
	reply.IsTopicMessage = false
	if err := channel.ingest(rawUpdate(t, 2, reply)); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 2)
	if sent[0].threadID == nil || *sent[0].threadID != 7 {
		t.Fatalf("топик ответа = %v", sent[0].threadID)
	}
	if sent[1].threadID != nil {
		t.Fatalf("реплай получил message_thread_id: %v", *sent[1].threadID)
	}
	if ok, _ := st.HasStreamEvents(StreamID("-100:7")); !ok {
		t.Fatal("нет durable-потока топика")
	}
	if ok, _ := st.HasStreamEvents(StreamID("-100:")); !ok {
		t.Fatal("нет durable-потока чата")
	}
}

// /join обрабатывается кодом без LLM и регистрирует игрока.
func TestChannelJoinCommand(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaign(t, st, "-100")
	api := newFakeBotAPI()
	model := agotest.NewTestConfig()
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)

	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 200, "/join"))); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "присоединился") {
		t.Fatalf("ответ на /join = %q", sent[0].text)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, member := range updated.Members {
		if member.UserID == "200" {
			found = true
		}
	}
	if !found {
		t.Fatalf("игрок не добавлен: %+v", updated.Members)
	}
	if calls := len(model.Calls()); calls != 0 {
		t.Fatalf("/join разбудил LLM: %d вызовов", calls)
	}

	if err := channel.ingest(rawUpdate(t, 2, groupMessage(-100, 200, "/join"))); err != nil {
		t.Fatal(err)
	}
	sent = waitSent(t, api, 2)
	if !strings.Contains(sent[1].text, "уже участвуешь") {
		t.Fatalf("повторный /join = %q", sent[1].text)
	}
}

func TestChannelJoinFullParty(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaign(t, st, "-100")
	for index := 0; index < store.MaxParty; index++ {
		if _, err := st.AddMember(campaign.ID, "dm-1", store.NewMemberInput{
			UserID: fmt.Sprintf("p%d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	api := newFakeBotAPI()
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), nil)
	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 999, "/join"))); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "заполнена") {
		t.Fatalf("ответ = %q", sent[0].text)
	}
}

// /kick: dm удаляет игрока реплаем, персонаж и история остаются.
func TestChannelKickByReply(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaignOwnedBy(t, st, "-100", "1")
	if _, err := st.AutoRegister(campaign.ID, store.NewMemberInput{
		UserID: "200", Name: strPtr("Иван"), Username: strPtr("ivan"),
	}); err != nil {
		t.Fatal(err)
	}
	sheet, err := st.SaveCharacter(campaign.ID, "200", store.NewCharacterInput{
		Name: "Иван", CharacterClass: "воин", Race: "человек",
		HP: intPtr(12), MaxHP: intPtr(12),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendTranscriptEntry(campaign.Slug, 1, store.TranscriptEntry{
		Kind: "dm", Text: "история до кика",
	}); err != nil {
		t.Fatal(err)
	}
	api := newFakeBotAPI()
	model := agotest.NewTestConfig()
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)

	kick := groupMessage(-100, 1, "/kick")
	kick.ReplyToMessage = groupMessage(-100, 200, "я тут")
	if err := channel.ingest(rawUpdate(t, 1, kick)); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "удалён") {
		t.Fatalf("ответ на /kick = %q", sent[0].text)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range updated.Members {
		if member.UserID == "200" {
			t.Fatalf("игрок не удалён: %+v", updated.Members)
		}
	}
	sheets, err := st.ListCharacters(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, existing := range sheets {
		if existing.ID == sheet.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("персонаж удалён вместе с участником")
	}
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil || day == nil {
		t.Fatalf("ReadDay: %v", err)
	}
	if !strings.Contains(strings.Join(day.Entries, "\n"), "история до кика") {
		t.Fatalf("история удалена вместе с участником: %v", day.Entries)
	}
	if calls := len(model.Calls()); calls != 0 {
		t.Fatalf("/kick разбудил LLM: %d вызовов", calls)
	}
}

// Посторонний /kick не должен занимать слот партии: autoJoin для команд не
// выполняется, участник не добавляется.
func TestChannelKickStrangerDoesNotJoin(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaignOwnedBy(t, st, "-100", "1")
	api := newFakeBotAPI()
	model := agotest.NewTestConfig()
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)

	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 200, "/kick @u200"))); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "только DM") {
		t.Fatalf("ответ постороннему на /kick = %q", sent[0].text)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range updated.Members {
		if member.UserID == "200" {
			t.Fatalf("посторонний занял слот партии: %+v", updated.Members)
		}
	}
	if calls := len(model.Calls()); calls != 0 {
		t.Fatalf("/kick постороннего разбудил LLM: %d вызовов", calls)
	}
}

// /join без привязанной кампании отвечает новым текстом отказа.
func TestChannelJoinWithoutCampaignRefused(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	model := agotest.NewTestConfig()
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)

	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 200, "/join"))); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "нет активной кампании") {
		t.Fatalf("ответ на /join без кампании = %q", sent[0].text)
	}
	if calls := len(model.Calls()); calls != 0 {
		t.Fatalf("/join разбудил LLM: %d вызовов", calls)
	}
}

// Команды в форум-топике находят кампанию, привязанную ко всему чату: тот же
// поиск, что у обычных сообщений (точный топик → чат).
func TestChannelCommandsInTopicUseChatCampaign(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaignOwnedBy(t, st, "-100", "1")
	if _, err := st.AutoRegister(campaign.ID, store.NewMemberInput{
		UserID: "200", Username: strPtr("ivan"),
	}); err != nil {
		t.Fatal(err)
	}
	api := newFakeBotAPI()
	model := agotest.NewTestConfig()
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)
	thread := 7

	kick := groupMessage(-100, 1, "/kick @ivan")
	kick.MessageThreadID = &thread
	kick.IsTopicMessage = true
	if err := channel.ingest(rawUpdate(t, 1, kick)); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "удалён") {
		t.Fatalf("ответ на /kick из топика = %q", sent[0].text)
	}

	join := groupMessage(-100, 300, "/join")
	join.MessageThreadID = &thread
	join.IsTopicMessage = true
	if err := channel.ingest(rawUpdate(t, 2, join)); err != nil {
		t.Fatal(err)
	}
	sent = waitSent(t, api, 2)
	if !strings.Contains(sent[1].text, "присоединился") {
		t.Fatalf("ответ на /join из топика = %q", sent[1].text)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	players := map[string]bool{}
	for _, member := range updated.Members {
		players[member.UserID] = true
	}
	if players["200"] || !players["300"] {
		t.Fatalf("состав после команд из топика: %+v", updated.Members)
	}
	if calls := len(model.Calls()); calls != 0 {
		t.Fatalf("команды из топика разбудили LLM: %d вызовов", calls)
	}
}

// /kick @username ищет цель без учёта регистра.
func TestChannelKickByUsername(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaignOwnedBy(t, st, "-100", "1")
	if _, err := st.AutoRegister(campaign.ID, store.NewMemberInput{
		UserID: "200", Username: strPtr("ivan"),
	}); err != nil {
		t.Fatal(err)
	}
	api := newFakeBotAPI()
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), nil)

	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 1, "/kick @Ivan"))); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "удалён") {
		t.Fatalf("ответ на /kick = %q", sent[0].text)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range updated.Members {
		if member.UserID == "200" {
			t.Fatalf("игрок не удалён: %+v", updated.Members)
		}
	}
}

// Не-dm не может кикать игроков.
func TestChannelKickRefusedForNonDM(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaignOwnedBy(t, st, "-100", "1")
	for _, user := range []store.NewMemberInput{
		{UserID: "200", Username: strPtr("u200")},
		{UserID: "300", Username: strPtr("u300")},
	} {
		if _, err := st.AutoRegister(campaign.ID, user); err != nil {
			t.Fatal(err)
		}
	}
	api := newFakeBotAPI()
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), nil)

	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 200, "/kick @u300"))); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "только DM") {
		t.Fatalf("ответ не-dm = %q", sent[0].text)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, member := range updated.Members {
		if member.UserID == "300" {
			found = true
		}
	}
	if !found {
		t.Fatalf("не-dm всё равно кикнул игрока: %+v", updated.Members)
	}
}

// DM кикнуть нельзя, в том числе себя.
func TestChannelKickDMRefused(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaignOwnedBy(t, st, "-100", "1")
	api := newFakeBotAPI()
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), nil)

	kick := groupMessage(-100, 1, "/kick")
	kick.ReplyToMessage = groupMessage(-100, 1, "мой ход")
	if err := channel.ingest(rawUpdate(t, 1, kick)); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "DM нельзя") {
		t.Fatalf("ответ на кик dm = %q", sent[0].text)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, member := range updated.Members {
		if member.UserID == "1" && member.Role == store.RoleDM {
			found = true
		}
	}
	if !found {
		t.Fatalf("dm удалён из кампании: %+v", updated.Members)
	}
}

// Кик несуществующего игрока не меняет состав и отвечает отказом.
func TestChannelKickUnknownPlayer(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaignOwnedBy(t, st, "-100", "1")
	api := newFakeBotAPI()
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), nil)

	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 1, "/kick @ghost"))); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "не найден") {
		t.Fatalf("ответ на кик неизвестного = %q", sent[0].text)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Members) != 1 {
		t.Fatalf("состав изменился: %+v", updated.Members)
	}
}

// /join в завершённой кампании отказывает, даже если привязка к чату осталась.
func TestChannelJoinFinishedCampaignRefused(t *testing.T) {
	t.Parallel()

	st, path := testStoreFile(t)
	campaign := testCampaignOwnedBy(t, st, "-100", "1")
	setCampaignStatus(t, path, campaign.ID, "finished")
	api := newFakeBotAPI()
	model := agotest.NewTestConfig()
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)

	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 200, "/join"))); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if !strings.Contains(sent[0].text, "нет активной кампании") {
		t.Fatalf("ответ /join в finished = %q", sent[0].text)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range updated.Members {
		if member.UserID == "200" {
			t.Fatalf("игрок добавлен в завершённую кампанию: %+v", updated.Members)
		}
	}
	if calls := len(model.Calls()); calls != 0 {
		t.Fatalf("/join разбудил LLM: %d вызовов", calls)
	}
}

func setCampaignStatus(t *testing.T, path, campaignID, status string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE campaigns SET status = ? WHERE id = ?`, status, campaignID); err != nil {
		t.Fatalf("update status: %v", err)
	}
}

// Склейка: сообщения за окно уходят одним ходом в формате «Имя: текст».
func TestChannelCoalescesMessages(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	model := agotest.NewTestConfig(plainResponse("Ход принят"))
	channel := newTestChannel(t, api, st, modelFactory(st, model), func(cfg *Config) {
		cfg.CoalesceWindow = 200 * time.Millisecond
	})
	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 100, "ДМ, осматриваю двор"))); err != nil {
		t.Fatal(err)
	}
	if err := channel.ingest(rawUpdate(t, 2, groupMessage(-100, 101, "Я прикрою."))); err != nil {
		t.Fatal(err)
	}
	waitSent(t, api, 1)
	calls := model.Calls()
	if len(calls) != 1 {
		t.Fatalf("вызовов модели = %d, want 1", len(calls))
	}
	rendered := ""
	for _, snapshot := range calls[0] {
		rendered += events.RenderForPrompt(snapshot) + "\n"
	}
	if !strings.Contains(rendered, "u100: ДМ, осматриваю двор") || !strings.Contains(rendered, "u101: Я прикрою.") {
		t.Fatalf("склейка не в формате «Имя: текст»:\n%s", rendered)
	}
}

// Переписка игроков между собой пишется в транскрипт, но LLM не будит.
func TestChannelGateWritesTranscriptWithoutLLM(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaign(t, st, "-100")
	api := newFakeBotAPI()
	model := agotest.NewTestConfig()
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)
	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 100, "Давай сначала осмотрим двор."))); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		pending, _ := st.PendingTelegramUpdates()
		return len(pending) == 0
	})
	if calls := len(model.Calls()); calls != 0 {
		t.Fatalf("переписка игроков разбудила LLM: %d вызовов", calls)
	}
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(day.Entries, "\n"), "осмотрим двор") {
		t.Fatalf("сообщение не попало в транскрипт: %v", day.Entries)
	}
	if len(api.sentMessages()) != 0 {
		t.Fatal("гейт всё-таки отправил ответ")
	}
}

// Превышение rate-limit пишется в транскрипт, но LLM не будится: в TS
// сообщение молча терялось.
func TestChannelRateLimitWritesTranscript(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaign(t, st, "-100")
	api := newFakeBotAPI()
	model := agotest.NewTestConfig(plainResponse("Первый ход"))
	channel := newTestChannel(t, api, st, modelFactory(st, model), func(cfg *Config) {
		cfg.Limits = Limits{ChatPerMinute: 1, UserPerMinute: 100, Window: time.Minute}
	})
	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 100, "ДМ, первый"))); err != nil {
		t.Fatal(err)
	}
	waitSent(t, api, 1)
	if err := channel.ingest(rawUpdate(t, 2, groupMessage(-100, 100, "второе сообщение сверх лимита"))); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		pending, _ := st.PendingTelegramUpdates()
		return len(pending) == 0
	})
	if calls := len(model.Calls()); calls != 1 {
		t.Fatalf("вызовов модели = %d, want 1", calls)
	}
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(day.Entries, "\n"), "сверх лимита") {
		t.Fatalf("лимитированное сообщение потерялось: %v", day.Entries)
	}
}

// Вложение скачивается через getFile и попадает в ввод модели как BinaryInput.
func TestChannelAttachmentBecomesBinaryInput(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	api.files["photo-1"] = FileInfo{FileID: "photo-1", FileSize: 4, FilePath: "photos/1.jpg"}
	api.blobs["photos/1.jpg"] = []byte("jpeg")
	model := agotest.NewTestConfig(plainResponse("Вижу фото"))
	channel := newTestChannel(t, api, st, modelFactory(st, model), func(cfg *Config) {
		cfg.MaxAttachmentBytes = 1024
	})
	message := privateMessage(1, "смотри")
	message.Photo = []PhotoSize{{FileID: "photo-1", FileSize: 4, Width: 10, Height: 10}}
	if err := channel.ingest(rawUpdate(t, 1, message)); err != nil {
		t.Fatal(err)
	}
	waitSent(t, api, 1)
	var binary *events.BinaryInput
	for _, snapshot := range model.Calls()[0] {
		for _, part := range requestParts(snapshot) {
			if item, ok := events.AsValue(part).(events.BinaryInput); ok {
				item := item
				binary = &item
			}
		}
	}
	if binary == nil || string(binary.Data) != "jpeg" || binary.MediaType != "image/jpeg" {
		t.Fatalf("BinaryInput = %+v", binary)
	}
}

// Durable-история: после перезапуска канала новый агент видит прошлый ход.
func TestChannelDurableHistoryAcrossRestart(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	model := agotest.NewTestConfig(plainResponse("Первый ответ"), plainResponse("Второй ответ"))

	first := newTestChannel(t, api, st, modelFactory(st, model), nil)
	if err := first.ingest(rawUpdate(t, 1, privateMessage(1, "Иду к двери"))); err != nil {
		t.Fatal(err)
	}
	waitSent(t, api, 1)
	if err := first.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	second := newTestChannel(t, api, st, modelFactory(st, model), nil)
	if err := second.ingest(rawUpdate(t, 2, privateMessage(1, "Открываю дверь"))); err != nil {
		t.Fatal(err)
	}
	waitSent(t, api, 2)
	calls := model.Calls()
	if len(calls) != 2 {
		t.Fatalf("вызовов модели = %d", len(calls))
	}
	rendered := ""
	for _, snapshot := range calls[1] {
		rendered += events.RenderForPrompt(snapshot) + "\n"
	}
	if !strings.Contains(rendered, "Иду к двери") || !strings.Contains(rendered, "Первый ответ") {
		t.Fatalf("после рестарта история потеряна:\n%s", rendered)
	}
}

func TestChannelCallbackQuery(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), nil)
	payload, _ := json.Marshal(Update{UpdateID: 3, CallbackQuery: &CallbackQuery{ID: "cb1"}})
	if err := channel.ingest(RawUpdate{UpdateID: 3, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		api.mu.Lock()
		defer api.mu.Unlock()
		return len(api.callbacks) == 1
	})
	api.mu.Lock()
	answer := api.callbacks[0]
	api.mu.Unlock()
	if !strings.Contains(answer, "cb1") || !strings.Contains(answer, "Unsupported action.") {
		t.Fatalf("ответ на callback = %q", answer)
	}
}

// Маркер хода: после Close() постер не отправляет картинку.
func TestSenderDoesNotPostAfterTurnClosed(t *testing.T) {
	t.Parallel()

	api := newFakeBotAPI()
	sender := NewSender(api)
	turn := NewTurn()
	ctx := WithTurn(context.Background(), turn)
	if err := sender.PostImage(ctx, "-1", nil, "https://img/x.png", "сцена"); err != nil {
		t.Fatalf("открытый ход: %v", err)
	}
	turn.Close()
	if err := sender.PostImage(ctx, "-1", nil, "https://img/y.png", "поздняя"); !errors.Is(err, ErrTurnOver) {
		t.Fatalf("ошибка = %v, want ErrTurnOver", err)
	}
	photos := api.photoMessages()
	if len(photos) != 1 || photos[0].photo != "https://img/x.png" {
		t.Fatalf("фото после закрытия хода: %+v", photos)
	}
}

func TestRepairHistoryDropsInterruptedTurn(t *testing.T) {
	t.Parallel()

	storage := store.NewEventStreamStorage(testStorePath(t))
	history := stream.NewHistory("repair", storage)
	request := func(text string) *events.ModelRequest {
		return &events.ModelRequest{Parts: []events.Input{events.TextInput{Content: text}}}
	}
	eventsList := []events.Event{
		request("первый"), &events.ModelResponse{Message: &events.ModelMessage{Content: "ответ"}},
		request("оборванный"), &events.ToolCallEvent{ID: "c1", Name: "tick", Arguments: "{}"},
	}
	if err := history.Replace(eventsList); err != nil {
		t.Fatal(err)
	}
	dropped, err := RepairHistory(history)
	if err != nil {
		t.Fatalf("RepairHistory: %v", err)
	}
	if dropped != 2 {
		t.Fatalf("выброшено = %d, want 2", dropped)
	}
	remaining, err := history.GetEvents()
	if err != nil || len(remaining) != 2 {
		t.Fatalf("осталось %d: %v", len(remaining), err)
	}

	// Последний ответ с tool_calls без результатов тоже не опора: остаётся
	// предыдущий нормальный ответ.
	withCalls := stream.NewHistory("calls", storage)
	if err := withCalls.Replace([]events.Event{
		request("первый"), &events.ModelResponse{Message: &events.ModelMessage{Content: "ответ"}},
		request("второй"), &events.ModelResponse{ToolCalls: events.ToolCallsEvent{Calls: []events.ToolCallEvent{{ID: "c2", Name: "tick"}}}},
	}); err != nil {
		t.Fatal(err)
	}
	dropped, err = RepairHistory(withCalls)
	if err != nil || dropped != 2 {
		t.Fatalf("оборванный tool-call: dropped=%d err=%v", dropped, err)
	}
	if left, _ := withCalls.GetEvents(); len(left) != 2 {
		t.Fatalf("опорой стал ответ с tool_calls: %d событий", len(left))
	}

	// История без ответа модели чистится целиком.
	empty := stream.NewHistory("empty", storage)
	if err := empty.Replace([]events.Event{request("висяк")}); err != nil {
		t.Fatal(err)
	}
	if _, err := RepairHistory(empty); err != nil {
		t.Fatal(err)
	}
	if left, _ := empty.GetEvents(); len(left) != 0 {
		t.Fatalf("висящий запрос остался: %d", len(left))
	}
}

func requestParts(ev events.Event) []events.Input {
	switch item := events.AsValue(ev).(type) {
	case events.ModelRequest:
		return item.Parts
	case events.DrainedModelRequest:
		return item.Parts
	}
	return nil
}

func plainResponse(text string) *events.ModelResponse {
	return &events.ModelResponse{Message: &events.ModelMessage{Content: text}}
}

// Отменённый ход не отправляет ни ответа, ни картинки: апдейт остаётся
// необработанным, чтобы после рестарта его дочитали.
func TestChannelCanceledTurnSendsNothing(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	model := agotest.NewTestConfig(plainResponse("поздний ответ"))
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	batch := Batch{Key: "1:", Messages: []Incoming{{UpdateID: 1, Message: privateMessage(1, "ход")}}}
	err := channel.handleBatch(ctx, batch)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ошибка = %v, want context.Canceled", err)
	}
	time.Sleep(20 * time.Millisecond)
	if sent := api.sentMessages(); len(sent) != 0 {
		t.Fatalf("отменённый ход отправил ответ: %+v", sent)
	}
	if photos := api.photoMessages(); len(photos) != 0 {
		t.Fatalf("отменённый ход отправил картинку: %+v", photos)
	}
	if calls := len(model.Calls()); calls != 1 {
		// Модель вызывается (и падает на отменённом контексте) — это ок;
		// важно, что ответа нет.
		t.Logf("вызовов модели = %d", calls)
	}
}

// Сбой модели отдаёт игрокам готовый нейтральный ответ из internal/dm.
func TestChannelNeutralReplyOnModelFailure(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), nil)
	if err := channel.ingest(rawUpdate(t, 1, privateMessage(1, "играем"))); err != nil {
		t.Fatal(err)
	}
	sent := waitSent(t, api, 1)
	if sent[0].text != dm.NeutralFailureReply {
		t.Fatalf("ответ = %q, want %q", sent[0].text, dm.NeutralFailureReply)
	}
}

// Негабаритный апдейт без поздних соседей не должен крутиться в long-poll:
// offset сохраняется и для пропущенного апдейта.
func TestChannelOversizedUpdateAdvancesOffset(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := newFakeBotAPI()
	api.queue <- []RawUpdate{{UpdateID: 42, Payload: []byte(`{"update_id":42,"padding":"` + strings.Repeat("x", 2048) + `"}`)}}
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), func(cfg *Config) {
		cfg.MaxUpdateBytes = 1024
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- channel.Run(ctx) }()
	waitFor(t, func() bool {
		offset, err := st.TelegramOffset()
		return err == nil && offset == 43
	})
	pending, err := st.PendingTelegramUpdates()
	if err != nil || len(pending) != 0 {
		t.Fatalf("негабарит попал в inbox: %+v, %v", pending, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run не остановился")
	}
}

// Если агент чата не собрался, сообщение не теряется: оно уходит в транскрипт.
func TestChannelFactoryFailureKeepsTranscript(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaign(t, st, "-100")
	api := newFakeBotAPI()
	failing := func(string, stream.Stream, func() *ttrpg.Session) (*dm.DM, error) {
		return nil, errors.New("skills dir is broken")
	}
	channel := newTestChannel(t, api, st, failing, nil)
	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 100, "ДМ, я открываю дверь"))); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		pending, _ := st.PendingTelegramUpdates()
		return len(pending) == 0
	})
	day, err := st.ReadDay(campaign.Slug, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(day.Entries, "\n"), "открываю дверь") {
		t.Fatalf("сообщение потерялось: %v", day.Entries)
	}
}

// Служебные отправки (callback, /join, postText) идут под контекстом Run:
// при остановке процесса они отменяются вместе с ним.
func TestChannelServiceCallsUseRunContext(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	api := &contextCapturingAPI{
		fakeAPI:   newFakeBotAPI(),
		callbacks: make(chan context.Context, 1),
		texts:     make(chan context.Context, 1),
	}
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- channel.Run(ctx) }()
	waitFor(t, func() bool { return channel.baseContext().Done() != nil })

	if err := channel.route(1, &Update{CallbackQuery: &CallbackQuery{ID: "cb-1"}}); err != nil {
		t.Fatalf("route callback: %v", err)
	}
	callbackCtx := <-api.callbacks
	channel.postText(groupMessage(-100, 100, "привет"), "служебный текст")
	textCtx := <-api.texts

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run не остановился")
	}
	if callbackCtx.Err() == nil {
		t.Fatal("answerCallbackQuery не отменился вместе с каналом")
	}
	if textCtx.Err() == nil {
		t.Fatal("postText не отменился вместе с каналом")
	}
}

type contextCapturingAPI struct {
	*fakeAPI
	callbacks chan context.Context
	texts     chan context.Context
}

func (a *contextCapturingAPI) AnswerCallbackQuery(ctx context.Context, _, _ string) error {
	a.callbacks <- ctx
	return nil
}

func (a *contextCapturingAPI) SendRichText(ctx context.Context, _ string, _ *int, _ string) error {
	a.texts <- ctx
	return nil
}

// Кампания, привязанная к форум-топику, тоже автоприсоединяет пишущих в нём.
func TestChannelAutoJoinsWriterInTopic(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign, err := st.CreateCampaign(store.NewCampaignInput{
		Title: "Три ворона", Length: store.LengthShort, Setting: "север", Theme: "выживание",
	}, store.NewOwnerInput{UserID: "dm-1", Username: strPtr("dm")})
	if err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}
	thread := 7
	if _, err := st.BindAndActivate(campaign.ID, "dm-1", store.BoundChat{
		ChatID: "-100", MessageThreadID: &thread,
	}); err != nil {
		t.Fatalf("BindAndActivate: %v", err)
	}
	api := newFakeBotAPI()
	channel := newTestChannel(t, api, st, modelFactory(st, agotest.NewTestConfig()), nil)
	message := groupMessage(-100, 300, "я в топике")
	message.MessageThreadID = &thread
	message.IsTopicMessage = true
	if err := channel.ingest(rawUpdate(t, 1, message)); err != nil {
		t.Fatal(err)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, member := range updated.Members {
		if member.UserID == "300" {
			found = true
		}
	}
	if !found {
		t.Fatalf("пишущий в топике не зарегистрирован: %+v", updated.Members)
	}
}

// Любой не-бот, пишущий в привязанном чате, автоматически становится игроком
// без LLM — как обещает строка промпта про /invite.
func TestChannelAutoJoinsWriter(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaign(t, st, "-100")
	api := newFakeBotAPI()
	model := agotest.NewTestConfig()
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)

	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 200, "привет, я новый игрок"))); err != nil {
		t.Fatal(err)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, member := range updated.Members {
		if member.UserID == "200" && member.Role == store.RolePlayer {
			found = true
		}
	}
	if !found {
		t.Fatalf("пишущий не зарегистрирован: %+v", updated.Members)
	}
	if calls := len(model.Calls()); calls != 0 {
		t.Fatalf("автоприсоединение разбудило LLM: %d вызовов", calls)
	}
	if sent := api.sentMessages(); len(sent) != 0 {
		t.Fatalf("автоприсоединение ответило в чат: %+v", sent)
	}
}

// Окно follow-up гейта приходит из конфига канала (env), а не хардкодится.
func TestChannelFollowUpWindowFromConfig(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	channel := newTestChannel(t, newFakeBotAPI(), st, modelFactory(st, agotest.NewTestConfig()), func(cfg *Config) {
		cfg.FollowUpWindow = 3 * time.Minute
	})
	channel.gate.mu.Lock()
	window := channel.gate.followUp
	channel.gate.mu.Unlock()
	if window != 3*time.Minute {
		t.Fatalf("follow-up = %s, want 3m", window)
	}
}

// При полной партии новый пишущий не регистрируется, но сообщение
// обрабатывается обычным путём (уходит в транскрипт).
func TestChannelAutoJoinStopsAtPartyLimit(t *testing.T) {
	t.Parallel()

	st := testStorePath(t)
	campaign := testCampaign(t, st, "-100")
	for index := 1; index <= store.MaxParty; index++ {
		if _, err := st.AutoRegister(campaign.ID, store.NewMemberInput{
			UserID: "player-" + strconv.Itoa(index),
		}); err != nil {
			t.Fatalf("AutoRegister: %v", err)
		}
	}
	api := newFakeBotAPI()
	model := agotest.NewTestConfig()
	channel := newTestChannel(t, api, st, modelFactory(st, model), nil)

	if err := channel.ingest(rawUpdate(t, 1, groupMessage(-100, 700, "возьмите меня в игру"))); err != nil {
		t.Fatal(err)
	}
	updated, err := st.GetCampaign(campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	players := 0
	for _, member := range updated.Members {
		if member.Role == store.RolePlayer {
			players++
		}
		if member.UserID == "700" {
			t.Fatalf("сверх лимита зарегистрирован 700: %+v", updated.Members)
		}
	}
	if players != store.MaxParty {
		t.Fatalf("игроков = %d, want %d", players, store.MaxParty)
	}
	waitFor(t, func() bool {
		day, err := st.ReadDay(campaign.Slug, 1)
		return err == nil && day != nil && strings.Contains(strings.Join(day.Entries, "\n"), "возьмите меня")
	})
}
