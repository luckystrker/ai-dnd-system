package dm

import (
	"log/slog"
	"time"

	agocontext "github.com/luckystrker/ago/context"
	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/middleware"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// sessionStartDepKey — маркер начала сессии в контексте хода. Решение
// принимается один раз за ход, а блок памяти пересобирается на каждый
// LLM-вызов (PLAN 19), поэтому маркер должен жить в контексте хода: иначе
// второй раунд тулов прочитал бы уже обновлённое время активности.
const sessionStartDepKey = "ttrpg.session_start"

// DefaultSessionGap — пауза в чате, после которой начинается новая сессия
// (PLAN 39). 20 часов отделяют вечернюю игру от следующей.
const DefaultSessionGap = 20 * time.Hour

// sessionBoundaryMW определяет границу игровой сессии по durable-времени
// последнего сообщения в чате (store.chat_activity), а не по счётчику ходов
// процесса: рестарт без паузы не должен давать ложный recap, а живой чат
// после недельного перерыва — оставаться без маркера.
type sessionBoundaryMW struct {
	middleware.Base
	store *store.Store
	gap   time.Duration
	now   func() time.Time
	log   *slog.Logger
}

func (m *sessionBoundaryMW) OnTurn(next middleware.TurnFunc, ev *events.ModelRequest, cx *agocontext.Context) (*events.ModelResponse, error) {
	if cx != nil {
		// Перезаписываем каждый ход: dependency живёт в контексте агента,
		// а маркер относится только к текущему ходу.
		cx.SetDependency(sessionStartDepKey, m.marker(cx))
	}
	return next(ev, cx)
}

// marker решает границу и записывает новое время активности. Ошибка хранилища
// не ломает ход: память и игра важнее маркера.
func (m *sessionBoundaryMW) marker(cx *agocontext.Context) string {
	if m.store == nil {
		return ""
	}
	session := sessionFrom(cx)
	if session == nil {
		return ""
	}
	key := session.CampaignID
	if key == "" {
		key = session.CampaignSlug
	}
	if key == "" {
		return ""
	}
	now := time.Now()
	if m.now != nil {
		now = m.now()
	}
	previous, seen, err := m.store.TouchChatActivity(key, now)
	if err != nil {
		if m.log != nil {
			m.log.Warn("dm: chat activity failed", "campaign", key, "error", err)
		}
		return ""
	}
	if !seen {
		return sessionStartMarker(0)
	}
	gap := now.Sub(previous)
	if gap < m.gap {
		return ""
	}
	return sessionStartMarker(gap)
}

// sessionStartFrom достаёт маркер текущего хода (пусто — обычный ход).
func sessionStartFrom(cx *agocontext.Context) string {
	if cx == nil {
		return ""
	}
	value, ok := cx.Dependency(sessionStartDepKey)
	if !ok {
		return ""
	}
	marker, _ := value.(string)
	return marker
}
