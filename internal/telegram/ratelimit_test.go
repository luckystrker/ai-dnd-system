package telegram

import (
	"testing"
	"time"
)

// Порт test/rate-limit.test.ts: окно, границы, изоляция ключей и очистка.
func TestSlidingWindowLimiter(t *testing.T) {
	t.Parallel()

	window := time.Minute
	base := time.Unix(1_700_000_000, 0)

	t.Run("allows up to the limit", func(t *testing.T) {
		limiter := NewSlidingWindowLimiter()
		for i := 0; i < 3; i++ {
			if !limiter.Allow("chat-1", 3, window, base) {
				t.Fatalf("запрос %d отклонён", i+1)
			}
		}
	})
	t.Run("blocks once full", func(t *testing.T) {
		limiter := NewSlidingWindowLimiter()
		for i := 0; i < 3; i++ {
			limiter.Allow("k", 3, window, base)
		}
		if limiter.Allow("k", 3, window, base) {
			t.Fatal("лимит не сработал")
		}
		if limiter.Allow("k", 3, window, base.Add(500*time.Millisecond)) {
			t.Fatal("лимит не сработал внутри окна")
		}
	})
	t.Run("frees capacity after the window slides", func(t *testing.T) {
		limiter := NewSlidingWindowLimiter()
		for i := 0; i < 3; i++ {
			limiter.Allow("k", 3, window, base)
		}
		if !limiter.Allow("k", 3, window, base.Add(61*time.Second)) {
			t.Fatal("окно не сдвинулось")
		}
	})
	t.Run("window boundary is exclusive", func(t *testing.T) {
		limiter := NewSlidingWindowLimiter()
		limiter.Allow("k", 1, time.Second, base)
		if !limiter.Allow("k", 1, time.Second, base.Add(time.Second)) {
			t.Fatal("граница окна должна освобождать слот")
		}
	})
	t.Run("keys are isolated", func(t *testing.T) {
		limiter := NewSlidingWindowLimiter()
		limiter.Allow("a", 1, window, base)
		if !limiter.Allow("b", 1, window, base) {
			t.Fatal("ключи делят одно окно")
		}
	})
	t.Run("sweeps stale keys", func(t *testing.T) {
		limiter := NewSlidingWindowLimiter()
		limiter.Allow("stale", 3, window, base)
		limiter.Allow("fresh", 3, window, base.Add(30*time.Second))
		limiter.Sweep(base.Add(62*time.Second), window)
		if limiter.Size() != 1 {
			t.Fatalf("ключей = %d, want 1", limiter.Size())
		}
	})
	t.Run("does not grow unbounded", func(t *testing.T) {
		limiter := NewSlidingWindowLimiter()
		for i := 0; i < 5000; i++ {
			limiter.Allow("key-"+time.Duration(i).String(), 3, time.Second, base.Add(time.Duration(i)*2*time.Second))
		}
		limiter.Sweep(base.Add(20000*time.Second), time.Second)
		if limiter.Size() >= 5000 {
			t.Fatalf("карта не чистится: %d", limiter.Size())
		}
	})
}

func TestLimitsByChatAndUser(t *testing.T) {
	t.Parallel()

	limiter := NewSlidingWindowLimiter()
	limits := Limits{ChatPerMinute: 2, UserPerMinute: 1, Window: time.Minute}
	now := time.Unix(1_700_000_000, 0)
	if !limiter.AllowChat("-1:", limits, now) || !limiter.AllowChat("-1:", limits, now) {
		t.Fatal("чат-лимит сработал раньше времени")
	}
	if limiter.AllowChat("-1:", limits, now) {
		t.Fatal("чат-лимит не сработал")
	}
	// Другой чат не страдает; у пользователя свой лимит.
	if !limiter.AllowChat("-2:", limits, now) {
		t.Fatal("чужой чат заблокирован")
	}
	if !limiter.AllowUser("42", limits, now) {
		t.Fatal("первый запрос пользователя отклонён")
	}
	if limiter.AllowUser("42", limits, now) {
		t.Fatal("пользовательский лимит не сработал")
	}
	if !limiter.AllowUser("", limits, now) {
		t.Fatal("пустой автор должен пропускаться")
	}
}
