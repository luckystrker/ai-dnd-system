package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // чистый Go-драйвер SQLite (FTS5 включён)
)

// schema — DDL всех таблиц, дословно перенесённый из TS-модулей
// (store-sqlite.ts, journal.ts, npc.ts, locations.ts, factions.ts,
// world-state.ts, combat-store.ts). Порядок важен из-за внешних ключей.
var schema = []string{
	`CREATE TABLE IF NOT EXISTS campaigns (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'setup',
  owner_user_id TEXT NOT NULL,
  length TEXT NOT NULL DEFAULT 'medium',
  setting TEXT NOT NULL DEFAULT '',
  theme TEXT NOT NULL DEFAULT '',
  goal TEXT,
  tone TEXT,
  opening_scene TEXT,
  description TEXT NOT NULL DEFAULT '',
  bound_chat_id TEXT,
  bound_thread_id INTEGER,
  current_day INTEGER,
  time_of_day TEXT,
  in_game_date TEXT,
  weather TEXT,
  created_at TEXT NOT NULL
)`,
	`CREATE INDEX IF NOT EXISTS idx_campaigns_bound_chat
  ON campaigns (bound_chat_id, bound_thread_id)`,
	`CREATE TABLE IF NOT EXISTS campaign_members (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  name TEXT,
  username TEXT,
  role TEXT NOT NULL DEFAULT 'player',
  PRIMARY KEY (campaign_id, user_id)
)`,
	`CREATE TABLE IF NOT EXISTS characters (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  owner_user_id TEXT NOT NULL,
  class TEXT NOT NULL,
  race TEXT NOT NULL,
  level INTEGER NOT NULL DEFAULT 1,
  stats TEXT NOT NULL DEFAULT '{}',
  background TEXT,
  motivation TEXT,
  appearance TEXT,
  hp INTEGER,
  max_hp INTEGER,
  conditions TEXT,
  inventory TEXT,
  abilities TEXT,
  gold INTEGER,
  xp INTEGER,
  location TEXT,
  updated_at TEXT,
  created_at TEXT NOT NULL,
  UNIQUE (campaign_id, slug)
)`,
	`CREATE TABLE IF NOT EXISTS quests (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  title TEXT NOT NULL,
  giver_npc_slug TEXT,
  objective TEXT NOT NULL,
  difficulty TEXT NOT NULL DEFAULT 'medium',
  reward_plan TEXT,
  status TEXT NOT NULL DEFAULT 'offered',
  deadline_day INTEGER,
  created_day INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
)`,
	`CREATE INDEX IF NOT EXISTS idx_quests_status
  ON quests (campaign_id, status)`,
	`CREATE TABLE IF NOT EXISTS open_threads (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  text TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'unresolved',
  status TEXT NOT NULL DEFAULT 'open',
  linked_quest_id TEXT,
  day_opened INTEGER NOT NULL DEFAULT 1,
  day_closed INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT
)`,
	`CREATE INDEX IF NOT EXISTS idx_open_threads_status
  ON open_threads (campaign_id, status)`,
	`CREATE TABLE IF NOT EXISTS campaign_days (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  date TEXT,
  note TEXT,
  summary TEXT,
  headline TEXT,
  PRIMARY KEY (campaign_id, day)
)`,
	`CREATE TABLE IF NOT EXISTS transcript_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  line TEXT NOT NULL,
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
)`,
	`CREATE TABLE IF NOT EXISTS key_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  line TEXT NOT NULL,
  permanent INTEGER NOT NULL DEFAULT 0,
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
)`,
	`CREATE TABLE IF NOT EXISTS ledger_rows (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  type TEXT NOT NULL,
  line TEXT NOT NULL,
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
)`,
	`CREATE TABLE IF NOT EXISTS campaign_summary (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  text TEXT NOT NULL,
  PRIMARY KEY (campaign_id, day)
)`,
	`CREATE TABLE IF NOT EXISTS npcs (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  role TEXT,
  status TEXT NOT NULL DEFAULT 'alive',
  location TEXT,
  relationships TEXT NOT NULL DEFAULT '{}',
  first_seen_day INTEGER,
  last_seen_day INTEGER,
  memory TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
)`,
	`CREATE TABLE IF NOT EXISTS locations (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  connections TEXT NOT NULL DEFAULT '[]',
  discovered_day INTEGER,
  visited_days TEXT NOT NULL DEFAULT '[]',
  current INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
)`,
	`CREATE TABLE IF NOT EXISTS factions (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  standing INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
)`,
	`CREATE TABLE IF NOT EXISTS world_changes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  category TEXT NOT NULL,
  text TEXT NOT NULL,
  day INTEGER,
  text_norm TEXT NOT NULL,
  UNIQUE (campaign_id, category, text_norm)
)`,
	`CREATE TABLE IF NOT EXISTS combat_snapshot (
  campaign_id TEXT PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE,
  combat TEXT NOT NULL,
  enemies TEXT NOT NULL,
  saved_at TEXT NOT NULL
)`,

	// --- durable Telegram-канал (слайс 6) -----------------------------------
	// Апдейт попадает сюда до обработки. Первичный ключ по update_id — дедуп
	// при повторной доставке (offset сохраняется отдельно и может отставать
	// на один апдейт после падения). processed_at ставится после обработки:
	// после рестарта необработанные апдейты дочитываются.
	`CREATE TABLE IF NOT EXISTS telegram_updates (
  update_id INTEGER PRIMARY KEY,
  chat_key TEXT NOT NULL,
  payload TEXT NOT NULL,
  received_at TEXT NOT NULL,
  processed_at TEXT
)`,
	`CREATE INDEX IF NOT EXISTS idx_telegram_updates_pending
  ON telegram_updates (processed_at, update_id)`,
	// Offset long-poll. Одна строка: следующее значение, которое просим у
	// Telegram. Пишется после записи апдейтов в inbox.
	`CREATE TABLE IF NOT EXISTS telegram_poll_state (
  id TEXT PRIMARY KEY,
  value INTEGER NOT NULL
)`,
	// Durable-история ходов чата: stream.Storage поверх SQLite. seq задаёт
	// порядок событий в потоке; компакция переписывает поток через SetHistory.
	`CREATE TABLE IF NOT EXISTS event_streams (
  stream_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  event TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (stream_id, seq)
)`,
}

// Store — соединение с базой кампаний и все операции памяти.
type Store struct {
	db *sql.DB
	// mu сериализует операции «прочитал → решил → записал»: в TS они были
	// атомарны из-за синхронности, в Go между операторами вклиниваются
	// горутины (обработчики чатов, субагент, тулы).
	mu sync.Mutex
	// ftsMu защищает ensureFTSIndex, ftsReady и запись индекса: catch-up
	// читает last и вставляет строки транзакцией, иначе параллельные поиски
	// дублируют fts_journal.
	ftsMu    sync.Mutex
	ftsReady bool
	// log принимает проглоченные ошибки хранилища (FTS DDL и catch-up):
	// поиск продолжает работать сканом, но слепого пятна в логах нет.
	log *slog.Logger
}

// SetLogger задаёт логгер хранилища; nil — slog.Default().
func (s *Store) SetLogger(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	s.log = logger
}

// Open открывает базу кампаний: WAL, foreign_keys=ON, DDL и миграции старых
// баз. Путь создаётся вместе с каталогом (как mkdirSync в TS).
func Open(path string) (*Store, error) {
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("store: создать каталог базы: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dsnFor(path))
	if err != nil {
		return nil, fmt.Errorf("store: открыть %s: %w", path, err)
	}
	// Один писатель: modernc/SQLite не любит конкурентные записи в одном
	// процессе, а WAL обслуживает внешних читателей (второй Store).
	db.SetMaxOpenConns(1)
	store := &Store{db: db, log: slog.Default()}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// dsnFor собирает DSN так, чтобы pragma применялись к КАЖДОМУ новому
// соединению пула (Exec на пуле действует только на одно соединение и теряется
// при его пересоздании). busy_timeout идёт до journal_mode: переключение WAL
// при открытом втором процессе иначе сразу вернёт BUSY.
func dsnFor(path string) string {
	params := "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	if path == ":memory:" {
		return "file::memory:?" + params
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return "file:" + path + "?" + params
}

func (s *Store) initialize() error {
	for _, statement := range schema {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("store: схема: %w", err)
		}
	}
	if err := s.migrate(); err != nil {
		return err
	}
	if err := s.repairKeyEventDedup(); err != nil {
		return err
	}
	return s.repairNpcDoubleDayMarkers()
}

// Close закрывает соединение.
func (s *Store) Close() error { return s.db.Close() }

// Ping проверяет соединение (health-check systemd-обёртки).
func (s *Store) Ping() error { return s.db.Ping() }

// migrate добавляет колонки, появившиеся после первых версий схемы.
func (s *Store) migrate() error {
	hasColumn := func(table, column string) (bool, error) {
		rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, columnType string
			var notNull, pk int
			var defaultValue any
			if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
				return false, err
			}
			if name == column {
				return true, nil
			}
		}
		return false, rows.Err()
	}

	characters, err := hasColumn("characters", "abilities")
	if err != nil {
		return fmt.Errorf("store: миграция characters: %w", err)
	}
	if !characters {
		if err := s.addColumn("characters", "abilities"); err != nil {
			return err
		}
	}

	for _, column := range []string{"time_of_day", "in_game_date", "weather"} {
		exists, err := hasColumn("campaigns", column)
		if err != nil {
			return fmt.Errorf("store: миграция campaigns: %w", err)
		}
		if exists {
			continue
		}
		if err := s.addColumn("campaigns", column); err != nil {
			return err
		}
	}
	return nil
}

// addColumn добавляет колонку; гонку с параллельным стартом (TS или второй
// процесс Go) считает успехом — колонка уже появилась.
func (s *Store) addColumn(table, column string) error {
	if _, err := s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " TEXT"); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return nil
		}
		return fmt.Errorf("store: ALTER %s.%s: %w", table, column, err)
	}
	return nil
}

// repairKeyEventDedup бэкфиллит event_id для старых строк key_events и удаляет
// точные дубли — как migrateKeyEventDedup в journal.ts.
func (s *Store) repairKeyEventDedup() error {
	rows, err := s.db.Query(
		"SELECT id, campaign_id, day, line, permanent FROM key_events WHERE event_id IS NULL ORDER BY id",
	)
	if err != nil {
		return fmt.Errorf("store: key_events backfill: %w", err)
	}
	type row struct {
		id         int
		campaignID string
		day        int
		line       string
		permanent  int
	}
	var pending []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.campaignID, &item.day, &item.line, &item.permanent); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(pending) == 0 {
		return nil
	}

	seen := map[string]bool{}
	var deleteIDs []int
	type update struct {
		key string
		id  int
	}
	var updates []update
	for _, item := range pending {
		clean := cleanKeyEventLine(item.line)
		if clean == "" {
			continue
		}
		key := "evt:" + contentEventID(item.campaignID, item.day, clean, item.permanent == 1)
		if seen[key] {
			deleteIDs = append(deleteIDs, item.id)
			continue
		}
		seen[key] = true
		updates = append(updates, update{key: key, id: item.id})
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range deleteIDs {
		if _, err := tx.Exec("DELETE FROM key_events WHERE id = ?", id); err != nil {
			return err
		}
	}
	for _, item := range updates {
		// Хеш мог уже принадлежать строке, добавленной новой версией бота:
		// бэкфилл такого NULL-дубля должен удалить старую строку, а не упасть
		// на UNIQUE(campaign_id, event_id) и заблокировать старт.
		var existing int
		err := tx.QueryRow(
			"SELECT 1 FROM key_events WHERE event_id = ? AND id != ? LIMIT 1",
			item.key, item.id,
		).Scan(&existing)
		switch {
		case err == nil:
			if _, err := tx.Exec("DELETE FROM key_events WHERE id = ?", item.id); err != nil {
				return err
			}
		case err == sql.ErrNoRows:
			if _, err := tx.Exec("UPDATE key_events SET event_id = ? WHERE id = ?", item.key, item.id); err != nil {
				return err
			}
		default:
			return err
		}
	}
	return tx.Commit()
}

var (
	keyEventPrefix = regexp.MustCompile(`^-\s*(\[важно\]\s*)?\*\*День\s+\d+\*\*:\s*`)
	dayMarkerHead  = regexp.MustCompile(`^(- \[День (\d+)\])(.*)$`)
	inlineDayRun   = regexp.MustCompile(`^\s*(\[\s*День\s+\d+\s*\]\s*)+`)
	inlineDayValue = regexp.MustCompile(`День\s+(\d+)`)
)

func cleanKeyEventLine(line string) string {
	return jsTrim(collapseNewlines(keyEventPrefix.ReplaceAllString(line, "")))
}

// repairNpcDoubleDayMarkers чинит старые строки памяти NPC с двойным маркером
// дня («- [День 1] [День 1] …») — как repairMemoryDoubleDayMarkers в npc.ts.
func (s *Store) repairNpcDoubleDayMarkers() error {
	rows, err := s.db.Query("SELECT id, memory FROM npcs WHERE memory != ''")
	if err != nil {
		return fmt.Errorf("store: NPC memory repair: %w", err)
	}
	type row struct {
		id     string
		memory string
	}
	var pending []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.memory); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	now := nowISO()
	for _, item := range pending {
		fixed := collapseDoubleDayMarkers(item.memory)
		if fixed == item.memory {
			continue
		}
		if _, err := s.db.Exec("UPDATE npcs SET memory = ?, updated_at = ? WHERE id = ?", fixed, now, item.id); err != nil {
			return err
		}
	}
	return nil
}

// collapseDoubleDayMarkers убирает повторные маркеры того же дня после первого.
func collapseDoubleDayMarkers(memory string) string {
	lines := strings.Split(memory, "\n")
	for index, line := range lines {
		match := dayMarkerHead.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		day := match[2]
		rest := match[3]
		repeat := regexp.MustCompile(`^\s*\[День ` + regexp.QuoteMeta(day) + `\]`)
		for {
			trimmed := repeat.ReplaceAllString(rest, "")
			if trimmed == rest {
				break
			}
			rest = trimmed
		}
		if rest != match[3] {
			lines[index] = match[1] + rest
		}
	}
	return strings.Join(lines, "\n")
}

// jsSpaces — набор, который JS String.prototype.trim обрезает (включая
// NBSP и BOM); strings.TrimSpace BOM не трогает.
const jsSpaces = " \t\n\v\f\r\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

func jsTrim(text string) string { return strings.Trim(text, jsSpaces) }

var newlineCollapse = regexp.MustCompile(`[\s\p{Z}\x{FEFF}]*\n[\s\p{Z}\x{FEFF}]*`)

// collapseNewlines повторяет text.replace(/\s*\n\s*/g, " ") с Unicode-
// пробелами, как в JS (NBSP, U+2028/2029, BOM).
func collapseNewlines(text string) string {
	return newlineCollapse.ReplaceAllString(text, " ")
}

func marshalJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(data)
}

// nullString превращает sql.NullString в *string.
func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

// nullInt превращает sql.NullInt64 в *int.
func nullInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}
