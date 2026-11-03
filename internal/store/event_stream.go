package store

import (
	"database/sql"
	"fmt"

	"github.com/luckystrker/ago/events"
	"github.com/luckystrker/ago/stream"
)

// EventStreamStorage — stream.Storage поверх SQLite: durable-история ходов
// чата. Одна таблица event_streams; поток адресуется stream_id (у Telegram —
// "tg:<chatId>:<topic>").
//
// Компакция ago переписывает историю через SetHistory, поэтому размер
// ограничен не журналом, а политикой компакции (и жёстким капом History).
type EventStreamStorage struct {
	store *Store
}

// NewEventStreamStorage создаёт хранилище истории потоков.
func NewEventStreamStorage(st *Store) *EventStreamStorage {
	return &EventStreamStorage{store: st}
}

var _ stream.Storage = (*EventStreamStorage)(nil)

// SaveEvent дописывает событие в конец потока.
func (s *EventStreamStorage) SaveEvent(streamID string, ev events.Event) error {
	payload, err := events.MarshalEvent(ev)
	if err != nil {
		return fmt.Errorf("store: сериализация события %T: %w", ev, err)
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if _, err := s.store.db.Exec(
		`INSERT INTO event_streams (stream_id, seq, event, created_at)
         VALUES (?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM event_streams WHERE stream_id = ?), ?, ?)`,
		streamID, streamID, string(payload), nowISO(),
	); err != nil {
		return fmt.Errorf("store: запись события потока %s: %w", streamID, err)
	}
	return nil
}

// ListStreamIDs возвращает id всех durable-потоков.
func (s *EventStreamStorage) ListStreamIDs() ([]string, error) {
	rows, err := s.store.db.Query("SELECT DISTINCT stream_id FROM event_streams ORDER BY stream_id")
	if err != nil {
		return nil, fmt.Errorf("store: список потоков: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetHistory читает поток в порядке seq.
func (s *EventStreamStorage) GetHistory(streamID string) ([]events.Event, error) {
	rows, err := s.store.db.Query(
		"SELECT event FROM event_streams WHERE stream_id = ? ORDER BY seq", streamID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: чтение потока %s: %w", streamID, err)
	}
	defer rows.Close()
	var history []events.Event
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		ev, err := events.UnmarshalEvent([]byte(payload))
		if err != nil {
			// Одно битое событие не должно ронять весь чат: пропускаем его,
			// остальная история остаётся рабочей.
			continue
		}
		history = append(history, ev)
	}
	return history, rows.Err()
}

// SetHistory переписывает поток целиком (компакция, ремонт после сбоя).
func (s *EventStreamStorage) SetHistory(streamID string, evs []events.Event) error {
	payloads := make([]string, 0, len(evs))
	for _, ev := range evs {
		payload, err := events.MarshalEvent(ev)
		if err != nil {
			return fmt.Errorf("store: сериализация события %T: %w", ev, err)
		}
		payloads = append(payloads, string(payload))
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	tx, err := s.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM event_streams WHERE stream_id = ?", streamID); err != nil {
		return err
	}
	createdAt := nowISO()
	for index, payload := range payloads {
		if _, err := tx.Exec(
			"INSERT INTO event_streams (stream_id, seq, event, created_at) VALUES (?, ?, ?, ?)",
			streamID, index+1, payload, createdAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DropHistory удаляет поток.
func (s *EventStreamStorage) DropHistory(streamID string) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if _, err := s.store.db.Exec("DELETE FROM event_streams WHERE stream_id = ?", streamID); err != nil {
		return fmt.Errorf("store: удаление потока %s: %w", streamID, err)
	}
	return nil
}

// HasStreamEvents — есть ли у потока сохранённая история (тест/диагностика).
func (s *Store) HasStreamEvents(streamID string) (bool, error) {
	var exists int
	err := s.db.QueryRow("SELECT 1 FROM event_streams WHERE stream_id = ? LIMIT 1", streamID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
