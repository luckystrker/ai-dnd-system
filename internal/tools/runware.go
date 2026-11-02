package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RunwareTimeout — лимит генерации; дольше не ждём, DM продолжает текстом.
const RunwareTimeout = 120 * time.Second

const (
	defaultRunwareModel  = "runware:101@1"
	defaultRunwareSize   = 1024
	defaultRunwareAPIURL = "https://api.runware.ai/v1"
)

// RunwareConfig — настройки клиента Runware (env: RUNWARE_*).
type RunwareConfig struct {
	APIKey   string
	Model    string
	Width    int
	Height   int
	Endpoint string
	Client   *http.Client
}

// RunwareFromEnv собирает конфиг из окружения; nil — ключ не задан.
func RunwareFromEnv(getenv func(string) string) *RunwareConfig {
	apiKey := strings.TrimSpace(getenv("RUNWARE_API_KEY"))
	if apiKey == "" {
		return nil
	}
	config := &RunwareConfig{APIKey: apiKey, Model: defaultRunwareModel,
		Width: defaultRunwareSize, Height: defaultRunwareSize,
		Endpoint: defaultRunwareAPIURL, Client: &http.Client{Timeout: RunwareTimeout}}
	if model := strings.TrimSpace(getenv("RUNWARE_MODEL")); model != "" {
		config.Model = model
	}
	config.Width = envInt(getenv, "RUNWARE_WIDTH", defaultRunwareSize)
	config.Height = envInt(getenv, "RUNWARE_HEIGHT", defaultRunwareSize)
	return config
}

func envInt(getenv func(string) string, key string, fallback int) int {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// RunwareClient — минимальный REST-клиент imageInference.
type RunwareClient struct {
	config RunwareConfig
}

// NewRunwareClient создаёт клиент с настройками по умолчанию.
func NewRunwareClient(config RunwareConfig) *RunwareClient {
	if config.Model == "" {
		config.Model = defaultRunwareModel
	}
	if config.Width <= 0 {
		config.Width = defaultRunwareSize
	}
	if config.Height <= 0 {
		config.Height = defaultRunwareSize
	}
	if config.Endpoint == "" {
		config.Endpoint = defaultRunwareAPIURL
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: RunwareTimeout}
	}
	return &RunwareClient{config: config}
}

type runwareTask struct {
	TaskType     string `json:"taskType"`
	ImageURL     string `json:"imageURL"`
	Error        string `json:"error"`
	ErrorMessage string `json:"errorMessage"`
}

// Generate генерирует одну картинку и возвращает URL.
func (c *RunwareClient) Generate(ctx context.Context, prompt string) (string, error) {
	if c == nil || strings.TrimSpace(c.config.APIKey) == "" {
		return "", fmt.Errorf("RUNWARE_API_KEY is not configured")
	}
	body := []map[string]any{{
		"taskType":       "imageInference",
		"taskUUID":       newRunwareUUID(),
		"model":          c.config.Model,
		"positivePrompt": prompt,
		"negativePrompt": DefaultNegativePrompt,
		"width":          c.config.Width,
		"height":         c.config.Height,
	}}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.Endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	response, err := c.config.Client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if readErr != nil {
		return "", readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("Runware API error %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	var envelope any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("Runware: invalid JSON response: %w", err)
	}
	tasks := extractRunwareTasks(envelope)
	for _, task := range tasks {
		if task.ImageURL != "" {
			return task.ImageURL, nil
		}
		if task.Error != "" {
			return "", fmt.Errorf("Runware: %s", task.Error)
		}
		if task.ErrorMessage != "" {
			return "", fmt.Errorf("Runware: %s", task.ErrorMessage)
		}
	}
	return "", fmt.Errorf("Runware не вернул imageURL")
}

func extractRunwareTasks(payload any) []runwareTask {
	if items, ok := payload.([]any); ok {
		return decodeRunwareTasks(items)
	}
	if object, ok := payload.(map[string]any); ok {
		if items, ok := object["data"].([]any); ok {
			return decodeRunwareTasks(items)
		}
	}
	return nil
}

func decodeRunwareTasks(items []any) []runwareTask {
	tasks := make([]runwareTask, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		task := runwareTask{}
		task.TaskType, _ = object["taskType"].(string)
		task.ImageURL, _ = object["imageURL"].(string)
		task.Error, _ = object["error"].(string)
		task.ErrorMessage, _ = object["errorMessage"].(string)
		tasks = append(tasks, task)
	}
	return tasks
}

func newRunwareUUID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
