package store

import (
	"database/sql"
	"fmt"
)

// TelegramUpdate — апдейт, сохранённый в durable-inbox до обработки.
type TelegramUpdate struct {
	UpdateID    int64
	ChatKey     string
	Payload     string
	ReceivedAt  string
	ProcessedAt *string
}

// SaveTelegramUpdate пишет апдейт в inbox. inserted=false — этот update_id уже
// записан (повторная доставка после рестарта или гонка с сохранением offset).
func (s *Store) SaveTelegramUpdate(updateID int64, chatKey, payload string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(
		`INSERT INTO telegram_updates (update_id, chat_key, payload, received_at)
         VALUES (?, ?, ?, ?) ON CONFLICT(update_id) DO NOTHING`,
		updateID, chatKey, payload, nowISO(),
	)
	if err != nil {
		return false, fmt.Errorf("store: запись telegram update %d: %w", updateID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// MarkTelegramUpdateProcessed помечает апдейт обработанным. Повторная пометка
// идемпотентна.
func (s *Store) MarkTelegramUpdateProcessed(updateID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(
		"UPDATE telegram_updates SET processed_at = ? WHERE update_id = ?",
		nowISO(), updateID,
	); err != nil {
		return fmt.Errorf("store: пометка telegram update %d: %w", updateID, err)
	}
	return nil
}

// PendingTelegramUpdates возвращает необработанные апдейты в порядке
// update_id: после рестарта их дочитывает канал.
func (s *Store) PendingTelegramUpdates() ([]TelegramUpdate, error) {
	rows, err := s.db.Query(
		`SELECT update_id, chat_key, payload, received_at, processed_at
         FROM telegram_updates WHERE processed_at IS NULL ORDER BY update_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: чтение telegram inbox: %w", err)
	}
	defer rows.Close()
	var updates []TelegramUpdate
	for rows.Next() {
		var item TelegramUpdate
		var processed sql.NullString
		if err := rows.Scan(&item.UpdateID, &item.ChatKey, &item.Payload, &item.ReceivedAt, &processed); err != nil {
			return nil, err
		}
		item.ProcessedAt = nullString(processed)
		updates = append(updates, item)
	}
	return updates, rows.Err()
}

// SaveTelegramOffset сохраняет offset long-poll (update_id + 1).
func (s *Store) SaveTelegramOffset(offset int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(
		`INSERT INTO telegram_poll_state (id, value) VALUES ('offset', ?)
         ON CONFLICT(id) DO UPDATE SET value = excluded.value`,
		offset,
	); err != nil {
		return fmt.Errorf("store: сохранение telegram offset: %w", err)
	}
	return nil
}

// TelegramOffset читает сохранённый offset; 0 — бот ещё не опрашивал Telegram.
func (s *Store) TelegramOffset() (int64, error) {
	var value int64
	err := s.db.QueryRow("SELECT value FROM telegram_poll_state WHERE id = 'offset'").Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: чтение telegram offset: %w", err)
	}
	return value, nil
}
