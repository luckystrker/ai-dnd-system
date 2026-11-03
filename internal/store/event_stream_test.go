package store

import (
	"path/filepath"
	"testing"

	"github.com/luckystrker/ago/events"
)

// История потока переживает закрытие базы: событие, DataInput и ToolResult
// проходят через events.MarshalEvent/UnmarshalEvent без потерь.
func TestEventStreamStorageRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "campaigns.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	storage := NewEventStreamStorage(st)

	request := &events.ModelRequest{Parts: []events.Input{
		events.TextInput{Content: "Иду к двери"},
		events.DataInput{Data: map[string]any{"roll": float64(14), "advantage": true}},
	}}
	if err := storage.SaveEvent("tg:1", request); err != nil {
		t.Fatalf("SaveEvent request: %v", err)
	}
	toolResult := events.FromResultCall(events.ToolCallEvent{ID: "call-1", Name: "roll_dice"}, events.ToolResult{
		Parts: []events.Input{events.TextInput{Content: "Итого: 14"}},
	})
	if err := storage.SaveEvent("tg:1", toolResult); err != nil {
		t.Fatalf("SaveEvent result: %v", err)
	}
	if err := storage.SaveEvent("tg:1", &events.ModelResponse{Message: &events.ModelMessage{Content: "Дверь открыта"}}); err != nil {
		t.Fatalf("SaveEvent response: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("повторный Open: %v", err)
	}
	defer st.Close()
	storage = NewEventStreamStorage(st)
	history, err := storage.GetHistory("tg:1")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("событий = %d, want 3", len(history))
	}
	restored, ok := events.AsValue(history[0]).(events.ModelRequest)
	if !ok || len(restored.Parts) != 2 {
		t.Fatalf("запрос восстановлен как %T (%+v)", history[0], history[0])
	}
	if text, ok := events.AsValue(restored.Parts[0]).(events.TextInput); !ok || text.Content != "Иду к двери" {
		t.Fatalf("текстовая часть = %+v", restored.Parts[0])
	}
	data, ok := events.AsValue(restored.Parts[1]).(events.DataInput)
	if !ok {
		t.Fatalf("DataInput восстановлен как %T", restored.Parts[1])
	}
	mapped, ok := data.Data.(map[string]any)
	if !ok || mapped["roll"] != float64(14) || mapped["advantage"] != true {
		t.Fatalf("DataInput = %+v", data.Data)
	}
	if result, ok := events.AsValue(history[1]).(events.ToolResultEvent); !ok || result.ParentID != "call-1" {
		t.Fatalf("ToolResultEvent = %T %+v", history[1], history[1])
	}
	if response, ok := events.AsValue(history[2]).(events.ModelResponse); !ok || response.Content() != "Дверь открыта" {
		t.Fatalf("ответ = %T", history[2])
	}

	// SetHistory переписывает историю (так работает компакция и Repair).
	if err := storage.SetHistory("tg:1", history[2:]); err != nil {
		t.Fatalf("SetHistory: %v", err)
	}
	short, err := storage.GetHistory("tg:1")
	if err != nil || len(short) != 1 {
		t.Fatalf("после SetHistory = %d, %v", len(short), err)
	}

	// Drop очищает только свой поток.
	if err := storage.SaveEvent("tg:2", &events.ModelResponse{Message: &events.ModelMessage{Content: "другой чат"}}); err != nil {
		t.Fatal(err)
	}
	if err := storage.DropHistory("tg:1"); err != nil {
		t.Fatalf("DropHistory: %v", err)
	}
	if left, _ := storage.GetHistory("tg:1"); len(left) != 0 {
		t.Fatalf("после Drop осталось %d событий", len(left))
	}
	if other, _ := storage.GetHistory("tg:2"); len(other) != 1 {
		t.Fatalf("чужой поток пострадал: %d", len(other))
	}
}
