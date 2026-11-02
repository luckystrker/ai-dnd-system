package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "123456:TEST-TOKEN"

type capturedRequest struct {
	path string
	body map[string]any
}

func newFakeAPI(t *testing.T, handler func(writer http.ResponseWriter, request *http.Request) bool) (*Client, *[]capturedRequest) {
	t.Helper()
	var (
		mu       sync.Mutex
		captured []capturedRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		captured = append(captured, capturedRequest{path: request.URL.Path, body: body})
		mu.Unlock()
		if handler != nil && handler(writer, request) {
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient(testToken,
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
		WithRetryBackoff(time.Millisecond),
	)
	return client, &captured
}

func TestClientSendMessageUsesHTMLAndThread(t *testing.T) {
	t.Parallel()

	client, requests := newFakeAPI(t, nil)
	thread := 7
	if err := client.SendMessage(context.Background(), "-100", &thread, "<b>привет</b>", "HTML"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if len(*requests) != 1 {
		t.Fatalf("запросов = %d", len(*requests))
	}
	request := (*requests)[0]
	if !strings.HasSuffix(request.path, "/sendMessage") {
		t.Fatalf("path = %q", request.path)
	}
	if request.body["text"] != "<b>привет</b>" || request.body["parse_mode"] != "HTML" {
		t.Fatalf("body = %+v", request.body)
	}
	if request.body["chat_id"] != "-100" || request.body["message_thread_id"] != float64(7) {
		t.Fatalf("body = %+v", request.body)
	}
}

func TestClientSendRichTextFallsBackToPlain(t *testing.T) {
	t.Parallel()

	var attempts int
	client, requests := newFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) bool {
		attempts++
		if attempts == 1 {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"ok":false,"error_code":400,"description":"can't parse entities"}`))
			return true
		}
		return false
	})
	if err := client.SendRichText(context.Background(), "1", nil, "Ты видишь **дракона**"); err != nil {
		t.Fatalf("SendRichText: %v", err)
	}
	if len(*requests) != 2 {
		t.Fatalf("запросов = %d, want 2 (HTML и fallback)", len(*requests))
	}
	if (*requests)[1].body["text"] != "Ты видишь дракона" {
		t.Fatalf("fallback текст = %v", (*requests)[1].body["text"])
	}
	if _, has := (*requests)[1].body["parse_mode"]; has {
		t.Fatalf("у fallback остался parse_mode: %+v", (*requests)[1].body)
	}
}

func TestClientRetriesOn429(t *testing.T) {
	t.Parallel()

	var attempts int
	client, _ := newFakeAPI(t, func(writer http.ResponseWriter, _ *http.Request) bool {
		attempts++
		if attempts == 1 {
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 0","parameters":{"retry_after":0}}`))
			return true
		}
		return false
	})
	if err := client.SendMessage(context.Background(), "1", nil, "x", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("попыток = %d, want 2", attempts)
	}
}

func TestClientRetries5xxAndRedactsToken(t *testing.T) {
	t.Parallel()

	client, _ := newFakeAPI(t, func(writer http.ResponseWriter, _ *http.Request) bool {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"ok":false,"error_code":500,"description":"Internal Server Error"}`))
		return true
	})
	err := client.SendMessage(context.Background(), "1", nil, "x", "")
	if err == nil {
		t.Fatal("ожидалась ошибка после исчерпания попыток")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("неожиданная отмена: %v", err)
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "TEST-TOKEN") {
		t.Fatalf("токен утёк в ошибку: %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 500 {
		t.Fatalf("ошибка = %v", err)
	}
}

func TestClientNetworkErrorRedactsToken(t *testing.T) {
	t.Parallel()

	client := NewClient(testToken, WithBaseURL("http://127.0.0.1:1"), WithRetryBackoff(time.Millisecond))
	err := client.SendMessage(context.Background(), "1", nil, "x", "")
	if err == nil {
		t.Fatal("ожидалась сетевая ошибка")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("токен утёк в сетевую ошибку: %v", err)
	}
}

func TestClientGetUpdatesKeepsRawPayload(t *testing.T) {
	t.Parallel()

	client, requests := newFakeAPI(t, func(writer http.ResponseWriter, _ *http.Request) bool {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"result":[{"update_id":11,"message":{"message_id":1,"text":"привет","chat":{"id":5,"type":"private"},"from":{"id":7,"first_name":"Дэн"}}},{"update_id":12,"callback_query":{"id":"cb1"}}]}`))
		return true
	})
	updates, err := client.GetUpdates(context.Background(), 11, 50*time.Second, []string{"message", "callback_query"})
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(updates) != 2 || updates[0].UpdateID != 11 || updates[1].UpdateID != 12 {
		t.Fatalf("updates = %+v", updates)
	}
	var raw map[string]any
	if err := json.Unmarshal(updates[0].Payload, &raw); err != nil || raw["update_id"] != float64(11) {
		t.Fatalf("raw payload = %s", updates[0].Payload)
	}
	body := (*requests)[0].body
	if body["offset"] != float64(11) || body["timeout"] != float64(50) {
		t.Fatalf("body = %+v", body)
	}
	allowed, ok := body["allowed_updates"].([]any)
	if !ok || len(allowed) != 2 {
		t.Fatalf("allowed_updates = %+v", body["allowed_updates"])
	}
}

func TestClientSendPhotoAndTypingAndCallback(t *testing.T) {
	t.Parallel()

	client, requests := newFakeAPI(t, nil)
	thread := 3
	if err := client.SendPhoto(context.Background(), "-1", &thread, "https://img/x.png", "сцена"); err != nil {
		t.Fatalf("SendPhoto: %v", err)
	}
	if err := client.SendChatAction(context.Background(), "-1", &thread, "typing"); err != nil {
		t.Fatalf("SendChatAction: %v", err)
	}
	if err := client.AnswerCallbackQuery(context.Background(), "cb1", "Unsupported action."); err != nil {
		t.Fatalf("AnswerCallbackQuery: %v", err)
	}
	if len(*requests) != 3 {
		t.Fatalf("запросов = %d", len(*requests))
	}
	if (*requests)[0].body["photo"] != "https://img/x.png" || (*requests)[0].body["caption"] != "сцена" {
		t.Fatalf("photo body = %+v", (*requests)[0].body)
	}
	if (*requests)[0].body["message_thread_id"] != float64(3) {
		t.Fatalf("thread не передан: %+v", (*requests)[0].body)
	}
	if (*requests)[1].body["action"] != "typing" {
		t.Fatalf("action = %+v", (*requests)[1].body)
	}
	if (*requests)[2].body["callback_query_id"] != "cb1" {
		t.Fatalf("callback = %+v", (*requests)[2].body)
	}
}

func TestClientGetFileAndDownloadLimit(t *testing.T) {
	t.Parallel()

	var downloadPath string
	client, requests := newFakeAPI(t, func(writer http.ResponseWriter, request *http.Request) bool {
		if strings.HasPrefix(request.URL.Path, "/file/bot") {
			downloadPath = request.URL.Path
			_, _ = writer.Write([]byte("данные"))
			return true
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"result":{"file_id":"f1","file_size":6,"file_path":"photos/f1.jpg"}}`))
		return true
	})
	info, err := client.GetFile(context.Background(), "f1")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if info.FilePath != "photos/f1.jpg" || info.FileSize != 6 {
		t.Fatalf("info = %+v", info)
	}
	if (*requests)[0].body["file_id"] != "f1" {
		t.Fatalf("body = %+v", (*requests)[0].body)
	}
	data, err := client.DownloadFile(context.Background(), info, 25*1024*1024)
	if err != nil || string(data) != "данные" {
		t.Fatalf("download = %q, %v", data, err)
	}
	if !strings.Contains(downloadPath, "/file/bot") || !strings.HasSuffix(downloadPath, "/photos/f1.jpg") {
		t.Fatalf("путь скачивания = %q", downloadPath)
	}

	// Файл больше лимита — ошибка до скачивания, без утечки токена.
	over, _ := newFakeAPI(t, func(writer http.ResponseWriter, _ *http.Request) bool {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"result":{"file_id":"big","file_size":104857601,"file_path":"big.bin"}}`))
		return true
	})
	big, err := over.GetFile(context.Background(), "big")
	if err != nil {
		t.Fatalf("GetFile big: %v", err)
	}
	if _, err := over.DownloadFile(context.Background(), big, 25*1024*1024); err == nil {
		t.Fatal("файл больше лимита скачался")
	} else if strings.Contains(err.Error(), testToken) {
		t.Fatalf("токен утёк: %v", err)
	}
}

func TestClientDownloadEnforcesLimitWithoutKnownSize(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(make([]byte, 64))
	}))
	defer server.Close()
	client := NewClient(testToken, WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	if _, err := client.DownloadFile(context.Background(), FileInfo{FilePath: "x"}, 10); err == nil {
		t.Fatal("превышение лимита не поймано")
	}
}

func TestClientSendRichTextSplitsLongMessages(t *testing.T) {
	t.Parallel()

	client, requests := newFakeAPI(t, nil)
	text := strings.Repeat("длинная строка\n", 1000)
	if err := client.SendRichText(context.Background(), "1", nil, text); err != nil {
		t.Fatalf("SendRichText: %v", err)
	}
	if len(*requests) < 2 {
		t.Fatalf("сообщений = %d, want >1", len(*requests))
	}
	for index, request := range *requests {
		chunk, _ := request.body["text"].(string)
		if utf16Len(chunk) > MessageLimit {
			t.Fatalf("кусок %d = %d единиц", index, utf16Len(chunk))
		}
	}
}

func TestClientDeleteWebhook(t *testing.T) {
	t.Parallel()

	client, requests := newFakeAPI(t, nil)
	if err := client.DeleteWebhook(context.Background()); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if len(*requests) != 1 || !strings.HasSuffix((*requests)[0].path, "/deleteWebhook") {
		t.Fatalf("requests = %+v", *requests)
	}
}
