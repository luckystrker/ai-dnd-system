package telegram

import (
	"testing"
	"time"

	"github.com/luckystriker/ai-dnd-system/internal/tools"
)

func TestTypingNotifierThrottlesPerChat(t *testing.T) {
	t.Parallel()

	api := newFakeBotAPI()
	notifier := NewTypingNotifier(api)
	now := time.Unix(1_700_000_000, 0)
	notifier.SetClock(func() time.Time { return now })
	thread := 7
	session := &tools.Session{ChatID: "-100", ChatType: "supergroup", MessageThreadID: &thread}
	activity := notifier.Activity(func() *tools.Session { return session })

	activity()
	activity()
	waitFor(t, func() bool { return api.actionsCount() >= 1 })
	time.Sleep(20 * time.Millisecond)
	if got := api.actionsCount(); got != 1 {
		t.Fatalf("typing отправлен %d раз, want 1 (троттлинг)", got)
	}

	now = now.Add(5 * time.Second)
	activity()
	waitFor(t, func() bool { return api.actionsCount() >= 2 })
	api.mu.Lock()
	action := api.actions[len(api.actions)-1]
	api.mu.Unlock()
	if action.chatID != "-100" || action.threadID == nil || *action.threadID != 7 || action.text != "typing" {
		t.Fatalf("typing = %+v", action)
	}

	// В личке ключ без топика — отдельный троттл.
	notifier.SetClock(func() time.Time { return now })
	private := &tools.Session{ChatID: "1", ChatType: "private"}
	notifier.Activity(func() *tools.Session { return private })()
	waitFor(t, func() bool { return api.actionsCount() >= 3 })
}
