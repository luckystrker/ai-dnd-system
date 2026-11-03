package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openActivityStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "campaigns.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, path
}

// Без записи активности граница сессии считается первой: ok = false.
func TestChatActivityMissing(t *testing.T) {
	t.Parallel()

	st, _ := openActivityStore(t)
	last, ok, err := st.ChatActivity("campaign-1")
	if err != nil {
		t.Fatalf("ChatActivity: %v", err)
	}
	if ok {
		t.Fatalf("активность найдена без записи: %v", last)
	}
	if !last.IsZero() {
		t.Fatalf("время = %v, want zero", last)
	}
}

// SetChatActivity переживает переоткрытие базы: граница сессии не зависит от
// памяти процесса.
func TestChatActivitySurvivesReopen(t *testing.T) {
	t.Parallel()

	st, path := openActivityStore(t)
	want := time.Date(2026, 11, 1, 12, 30, 0, 0, time.UTC)
	if err := st.SetChatActivity("campaign-1", want); err != nil {
		t.Fatalf("SetChatActivity: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open после рестарта: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	got, ok, err := reopened.ChatActivity("campaign-1")
	if err != nil {
		t.Fatalf("ChatActivity: %v", err)
	}
	if !ok || !got.Equal(want) {
		t.Fatalf("активность после рестарта = %v (ok=%v), want %v", got, ok, want)
	}
}

// TouchChatActivity атомарно отдаёт прошлое время и пишет новое.
func TestTouchChatActivityReturnsPrevious(t *testing.T) {
	t.Parallel()

	st, _ := openActivityStore(t)
	first := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	second := first.Add(3 * time.Hour)

	previous, ok, err := st.TouchChatActivity("campaign-1", first)
	if err != nil {
		t.Fatalf("Touch 1: %v", err)
	}
	if ok || !previous.IsZero() {
		t.Fatalf("первое касание: previous = %v, ok = %v", previous, ok)
	}
	previous, ok, err = st.TouchChatActivity("campaign-1", second)
	if err != nil {
		t.Fatalf("Touch 2: %v", err)
	}
	if !ok || !previous.Equal(first) {
		t.Fatalf("второе касание: previous = %v (ok=%v), want %v", previous, ok, first)
	}
	last, ok, err := st.ChatActivity("campaign-1")
	if err != nil || !ok || !last.Equal(second) {
		t.Fatalf("ChatActivity = %v (ok=%v, err=%v), want %v", last, ok, err, second)
	}
}

// Ключи независимы: активность одного чата не сдвигает границу другого.
func TestChatActivityKeepsKeysSeparate(t *testing.T) {
	t.Parallel()

	st, _ := openActivityStore(t)
	early := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	late := early.Add(30 * time.Hour)
	if err := st.SetChatActivity("chat-a", early); err != nil {
		t.Fatalf("Set chat-a: %v", err)
	}
	if err := st.SetChatActivity("chat-b", late); err != nil {
		t.Fatalf("Set chat-b: %v", err)
	}
	for key, want := range map[string]time.Time{"chat-a": early, "chat-b": late} {
		got, ok, err := st.ChatActivity(key)
		if err != nil || !ok || !got.Equal(want) {
			t.Fatalf("%s = %v (ok=%v, err=%v), want %v", key, got, ok, err, want)
		}
	}
}
