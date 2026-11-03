package telegram

import (
	"sync"
	"time"
)

// SlidingWindowLimiter — in-memory rate limiter (порт TS-версии; до слайса 8 —
// `agent/lib/rate-limit.ts`).
// Один экземпляр на процесс: бот работает одиночным инстансом.
type SlidingWindowLimiter struct {
	mu      sync.Mutex
	buckets map[string]bucket
}

type bucket struct {
	hits         []time.Time
	lastAccessed time.Time
}

// NewSlidingWindowLimiter создаёт пустой лимитер.
func NewSlidingWindowLimiter() *SlidingWindowLimiter {
	return &SlidingWindowLimiter{buckets: map[string]bucket{}}
}

// Allow возвращает true, если запрос укладывается в лимит (и учитывает его).
func (l *SlidingWindowLimiter) Allow(key string, limit int, window time.Duration, now time.Time) bool {
	if limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	windowStart := now.Add(-window)
	existing := l.buckets[key]
	hits := make([]time.Time, 0, len(existing.hits)+1)
	for _, hit := range existing.hits {
		if hit.After(windowStart) {
			hits = append(hits, hit)
		}
	}
	allowed := len(hits) < limit
	if allowed {
		hits = append(hits, now)
	}
	l.buckets[key] = bucket{hits: hits, lastAccessed: now}
	if len(l.buckets) > 1000 {
		l.sweepLocked(now, window)
	}
	return allowed
}

// Sweep удаляет ключи, к которым не обращались дольше window.
func (l *SlidingWindowLimiter) Sweep(now time.Time, window time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now, window)
}

func (l *SlidingWindowLimiter) sweepLocked(now time.Time, window time.Duration) {
	cutoff := now.Add(-window)
	for key, item := range l.buckets {
		if item.lastAccessed.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

// Size — число отслеживаемых ключей.
func (l *SlidingWindowLimiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Limits — лимиты Telegram-канала (сообщений в окно).
type Limits struct {
	ChatPerMinute int
	UserPerMinute int
	Window        time.Duration
}

// DefaultLimits — те же значения, что TELEGRAM_RATE_LIMITS в TS.
func DefaultLimits() Limits {
	return Limits{ChatPerMinute: 20, UserPerMinute: 10, Window: time.Minute}
}

// AllowChat проверяет лимит чата/топика.
func (l *SlidingWindowLimiter) AllowChat(chatKey string, limits Limits, now time.Time) bool {
	return l.Allow("chat:"+chatKey, limits.ChatPerMinute, limits.Window, now)
}

// AllowUser проверяет лимит пользователя.
func (l *SlidingWindowLimiter) AllowUser(userID string, limits Limits, now time.Time) bool {
	if userID == "" {
		return true
	}
	return l.Allow("user:"+userID, limits.UserPerMinute, limits.Window, now)
}
