package telegram

import (
	"strconv"
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

// Замолчавшие чаты не копятся в last: записи старше 10 троттлов вычищаются.
func TestTypingNotifierSweepsStaleChats(t *testing.T) {
	t.Parallel()

	notifier := NewTypingNotifier(newFakeBotAPI())
	now := time.Unix(1_700_000_000, 0)
	notifier.SetClock(func() time.Time { return now })
	notifier.mu.Lock()
	for index := 0; index < 1000; index++ {
		notifier.last["chat-"+strconv.Itoa(index)] = now
	}
	notifier.mu.Unlock()

	later := now.Add(time.Minute)
	notifier.SetClock(func() time.Time { return later })
	session := &tools.Session{ChatID: "-100", ChatType: "supergroup"}
	notifier.Activity(func() *tools.Session { return session })()

	notifier.mu.Lock()
	size := len(notifier.last)
	notifier.mu.Unlock()
	if size != 1 {
		t.Fatalf("last = %d, want 1 после чистки", size)
	}
}
