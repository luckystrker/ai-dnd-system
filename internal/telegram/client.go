package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL — корень Bot API.
const DefaultBaseURL = "https://api.telegram.org"

const (
	// DefaultMaxRetries — попыток на один вызов при 429/5xx (первая не в счёт).
	DefaultMaxRetries = 3
	// DefaultRetryBackoff — базовая пауза перед повтором 5xx.
	DefaultRetryBackoff = 300 * time.Millisecond
	// DefaultMaxDownloadBytes — политика вложений TS: 25 МБ.
	DefaultMaxDownloadBytes = 25 * 1024 * 1024

	// requestTimeout — предел одного запроса Bot API. Long-poll выставляет
	// свой контекст (таймаут опроса + запас), скачивание — свой.
	requestTimeout  = 70 * time.Second
	downloadTimeout = 2 * time.Minute
)

// APIError — ответ Telegram с ok=false. Текст не содержит URL с токеном.
type APIError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	message := fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
	if e.RetryAfter > 0 {
		message += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	return message
}

// Client — Bot API на stdlib. Токен входит в URL запроса и нигде не логируется
// и не попадает в текст ошибок.
type Client struct {
	token            string
	baseURL          string
	http             *http.Client
	maxRetries       int
	retryBackoff     time.Duration
	maxDownloadBytes int64
	sleep            func(context.Context, time.Duration) error
}

// Option настраивает клиент.
type Option func(*Client)

// WithBaseURL подменяет корень Bot API (тесты, локальный fake-сервер).
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithHTTPClient подменяет HTTP-клиент.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) { c.http = client }
}

// WithRetryBackoff задаёт базовую паузу повторов.
func WithRetryBackoff(backoff time.Duration) Option {
	return func(c *Client) { c.retryBackoff = backoff }
}

// WithMaxRetries задаёт число повторов (не считая первой попытки).
func WithMaxRetries(retries int) Option {
	return func(c *Client) { c.maxRetries = retries }
}

// NewClient создаёт клиент Bot API.
func NewClient(token string, opts ...Option) *Client {
	client := &Client{
		token:   token,
		baseURL: DefaultBaseURL,
		// Таймаут задаётся контекстом каждого вызова: у long-poll он
		// зависит от TELEGRAM_POLL_TIMEOUT_SECONDS, у остальных — 70 с.
		http:             &http.Client{},
		maxRetries:       DefaultMaxRetries,
		retryBackoff:     DefaultRetryBackoff,
		maxDownloadBytes: DefaultMaxDownloadBytes,
		sleep:            sleepContext,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// call выполняет метод Bot API с ретраями на 429 и 5xx. Любая ошибка клиента
// очищается от токена: URL запроса в тексте не остаётся.
//
// retryTransport разрешает повтор при сетевой ошибке (обрыв соединения).
// Только для идемпотентных методов: sendMessage/sendPhoto после обрыва могли
// уже дойти до Telegram, и повтор дал бы игроку дубль ответа.
func (c *Client) call(ctx context.Context, method string, payload any, out any, retryTransport bool, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram %s: encode: %w", method, err)
	}
	endpoint := c.baseURL + "/bot" + c.token + "/" + method
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			wait := c.retryBackoff * time.Duration(1<<(attempt-1))
			if apiErr, ok := lastErr.(*APIError); ok && apiErr.RetryAfter > 0 {
				wait = apiErr.RetryAfter
			}
			if wait > 10*time.Second {
				wait = 10 * time.Second
			}
			if err := sleepContext(ctx, wait); err != nil {
				return err
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
		if err != nil {
			return fmt.Errorf("telegram %s: request: %w", method, c.redact(err))
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := c.http.Do(request)
		if err != nil {
			lastErr = &transportError{method: method, err: c.redact(err)}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !retryTransport {
				return lastErr
			}
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		response.Body.Close()
		if readErr != nil {
			lastErr = &transportError{method: method, err: c.redact(readErr)}
			if !retryTransport {
				return lastErr
			}
			continue
		}
		var envelope struct {
			OK          bool            `json:"ok"`
			Result      json.RawMessage `json:"result"`
			ErrorCode   int             `json:"error_code"`
			Description string          `json:"description"`
			Parameters  struct {
				RetryAfter int `json:"retry_after"`
			} `json:"parameters"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			lastErr = &transportError{method: method, err: fmt.Errorf("invalid JSON response: %w", err)}
			if !retryTransport {
				return lastErr
			}
			continue
		}
		if !envelope.OK {
			apiErr := &APIError{
				Method:      method,
				Code:        envelope.ErrorCode,
				Description: c.redactText(envelope.Description),
			}
			if envelope.Parameters.RetryAfter > 0 {
				apiErr.RetryAfter = time.Duration(envelope.Parameters.RetryAfter) * time.Second
			}
			if envelope.ErrorCode == http.StatusTooManyRequests || envelope.ErrorCode >= 500 {
				lastErr = apiErr
				continue
			}
			return apiErr
		}
		if out != nil && len(envelope.Result) > 0 {
			if err := json.Unmarshal(envelope.Result, out); err != nil {
				return fmt.Errorf("telegram %s: decode result: %w", method, err)
			}
		}
		return nil
	}
	return lastErr
}

// transportError — сетевая ошибка без токена в тексте.
type transportError struct {
	method string
	err    error
}

func (e *transportError) Error() string { return "telegram " + e.method + ": " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

// redact превращает ошибку в безопасную: вырезает токен и /bot<token> из URL.
func (c *Client) redact(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(c.redactText(err.Error()))
}

func (c *Client) redactText(text string) string {
	if c.token == "" {
		return text
	}
	text = strings.ReplaceAll(text, "/bot"+c.token, "/bot<redacted>")
	text = strings.ReplaceAll(text, c.token, "<redacted>")
	return text
}

// GetUpdates — long-poll. offset — update_id + 1 (0 — с начала очереди).
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration, allowed []string) ([]RawUpdate, error) {
	payload := map[string]any{
		"offset":  offset,
		"timeout": int(timeout / time.Second),
	}
	if len(allowed) > 0 {
		payload["allowed_updates"] = allowed
	}
	var raws []json.RawMessage
	// Long-poll держит соединение timeout секунд; клиентский контекст должен
	// быть длиннее, иначе опрос обрывался бы раньше ответа Telegram.
	pollCtx, cancel := context.WithTimeout(ctx, timeout+15*time.Second)
	defer cancel()
	if err := c.call(pollCtx, "getUpdates", payload, &raws, true, 0); err != nil {
		return nil, err
	}
	updates := make([]RawUpdate, 0, len(raws))
	for _, raw := range raws {
		var head struct {
			UpdateID int64 `json:"update_id"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			continue
		}
		updates = append(updates, RawUpdate{UpdateID: head.UpdateID, Payload: []byte(raw)})
	}
	return updates, nil
}

// DeleteWebhook снимает вебхук: решение владельца — long-poll.
func (c *Client) DeleteWebhook(ctx context.Context) error {
	return c.call(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil, true, requestTimeout)
}

// SendMessage отправляет текст. parseMode пустой — plain text.
func (c *Client) SendMessage(ctx context.Context, chatID string, threadID *int, text, parseMode string) error {
	payload := map[string]any{"chat_id": chatID, "text": text}
	if threadID != nil {
		payload["message_thread_id"] = *threadID
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	return c.call(ctx, "sendMessage", payload, nil, false, requestTimeout)
}

// SendRichText отправляет ответ модели: Markdown → HTML, при любой ошибке
// HTML-отправки тот же кусок уходит plain-текстом (как TS-канал).
func (c *Client) SendRichText(ctx context.Context, chatID string, threadID *int, text string) error {
	for _, chunk := range SplitMessageText(text) {
		if err := c.SendMessage(ctx, chatID, threadID, MarkdownToTelegramHTML(chunk), "HTML"); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if plainErr := c.SendMessage(ctx, chatID, threadID, StripMarkdown(chunk), ""); plainErr != nil {
				return plainErr
			}
		}
	}
	return nil
}

// SendChatAction показывает индикатор (typing).
func (c *Client) SendChatAction(ctx context.Context, chatID string, threadID *int, action string) error {
	payload := map[string]any{"chat_id": chatID, "action": action}
	if threadID != nil {
		payload["message_thread_id"] = *threadID
	}
	return c.call(ctx, "sendChatAction", payload, nil, false, requestTimeout)
}

// SendPhoto публикует картинку по URL с подписью.
func (c *Client) SendPhoto(ctx context.Context, chatID string, threadID *int, photo, caption string) error {
	payload := map[string]any{"chat_id": chatID, "photo": photo}
	if caption != "" {
		payload["caption"] = caption
	}
	if threadID != nil {
		payload["message_thread_id"] = *threadID
	}
	return c.call(ctx, "sendPhoto", payload, nil, false, requestTimeout)
}

// AnswerCallbackQuery отвечает на нажатие кнопки.
func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	payload := map[string]any{"callback_query_id": callbackID}
	if text != "" {
		payload["text"] = text
	}
	return c.call(ctx, "answerCallbackQuery", payload, nil, false, requestTimeout)
}

// GetFile резолвит file_id в путь для скачивания.
func (c *Client) GetFile(ctx context.Context, fileID string) (FileInfo, error) {
	var info FileInfo
	if err := c.call(ctx, "getFile", map[string]any{"file_id": fileID}, &info, true, requestTimeout); err != nil {
		return FileInfo{}, err
	}
	return info, nil
}

// DownloadFile скачивает файл с лимитом maxBytes (25MB по умолчанию).
// Размер проверяется до запроса по FileSize и после — через LimitReader:
// Telegram может не прислать file_size.
func (c *Client) DownloadFile(ctx context.Context, info FileInfo, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = c.maxDownloadBytes
	}
	if info.FileSize > maxBytes {
		return nil, fmt.Errorf("telegram getFile: файл %s больше лимита %d байт (file_size=%d)",
			info.FilePath, maxBytes, info.FileSize)
	}
	if info.FilePath == "" {
		return nil, errors.New("telegram getFile: пустой file_path")
	}
	// file_path от Telegram может содержать подкаталоги; сегменты кодируем.
	segments := strings.Split(strings.TrimPrefix(info.FilePath, "/"), "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	endpoint := c.baseURL + "/file/bot" + c.token + "/" + strings.Join(segments, "/")
	downloadCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("telegram download: request: %w", c.redact(err))
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("telegram download: %w", c.redact(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return nil, fmt.Errorf("telegram download: %d %s", response.StatusCode, http.StatusText(response.StatusCode))
	}
	if declared, err := strconv.ParseInt(response.Header.Get("Content-Length"), 10, 64); err == nil && declared > maxBytes {
		return nil, fmt.Errorf("telegram download: файл %s больше лимита %d байт", info.FilePath, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("telegram download: %w", c.redact(err))
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("telegram download: файл %s больше лимита %d байт", info.FilePath, maxBytes)
	}
	return data, nil
}
