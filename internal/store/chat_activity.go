package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Активность чата — durable-источник границы игровой сессии (PLAN 39).
//
// Ключ — кампания, а не сырой chat id: кампания привязана ровно к одному чату
// или форум-топику (решение 28/29), поэтому ключ кампании эквивалентен паре
// «чат + топик» и одинаково работает из Telegram, repl и eval, где своего
// Telegram-чата нет. Время пишет код, который знает момент сообщения: ход DM
// (в том числе в eval и repl) и запись сообщения без хода LLM (гейт,
// rate-limit). Отдельная таблица нужна потому, что transcript_entries не
// хранит время, telegram_updates есть только у Telegram, а event_streams
// переписывается компакцией и уже содержит событие текущего хода.

// ChatActivity возвращает время последней активности; ok=false — активности
// ещё не было (первый ход чата).
func (s *Store) ChatActivity(chatKey string) (time.Time, bool, error) {
	var raw string
	err := s.db.QueryRow(
		"SELECT last_message_at FROM chat_activity WHERE chat_key = ?", chatKey,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("store: чтение активности чата: %w", err)
	}
	at, parseErr := time.Parse(time.RFC3339Nano, raw)
	if parseErr != nil {
		return time.Time{}, false, fmt.Errorf("store: разбор активности чата %q: %w", raw, parseErr)
	}
	return at.UTC(), true, nil
}

// SetChatActivity записывает время активности (фикстуры тестов и eval).
func (s *Store) SetChatActivity(chatKey string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO chat_activity (chat_key, last_message_at) VALUES (?, ?)
         ON CONFLICT(chat_key) DO UPDATE SET last_message_at = excluded.last_message_at`,
		chatKey, at.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("store: запись активности чата: %w", err)
	}
	return nil
}

// TouchChatActivity атомарно читает прошлую активность и записывает новую:
// возвращает, сколько прошло с прошлого сообщения к моменту now. Так текущее
// сообщение не обнуляет измеряемую паузу — иначе граница сессии никогда бы не
// срабатывала.
func (s *Store) TouchChatActivity(chatKey string, now time.Time) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	err := s.db.QueryRow(
		"SELECT last_message_at FROM chat_activity WHERE chat_key = ?", chatKey,
	).Scan(&raw)
	previous, found := time.Time{}, false
	if err == nil {
		parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil {
			return time.Time{}, false, fmt.Errorf("store: разбор активности чата %q: %w", raw, parseErr)
		}
		previous, found = parsed.UTC(), true
	} else if err != sql.ErrNoRows {
		return time.Time{}, false, fmt.Errorf("store: чтение активности чата: %w", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO chat_activity (chat_key, last_message_at) VALUES (?, ?)
         ON CONFLICT(chat_key) DO UPDATE SET last_message_at = excluded.last_message_at`,
		chatKey, now.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return time.Time{}, false, fmt.Errorf("store: запись активности чата: %w", err)
	}
	return previous, found, nil
}
