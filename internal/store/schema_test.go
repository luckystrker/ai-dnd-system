package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestPragmasAppliedPerConnection(t *testing.T) {
	store := newTestStore(t)

	check := func() {
		t.Helper()
		var foreignKeys, busyTimeout int
		var journalMode string
		if err := store.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := store.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatal(err)
		}
		if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 {
			t.Fatalf("foreign_keys = %d", foreignKeys)
		}
		if busyTimeout != 5000 {
			t.Fatalf("busy_timeout = %d", busyTimeout)
		}
		if journalMode != "wal" {
			t.Fatalf("journal_mode = %q", journalMode)
		}
	}
	check()

	// Пересоздаём соединение: настройки должны прийти из DSN, а не потеряться.
	store.db.SetMaxIdleConns(0)
	check()
}

type schemaObject struct {
	Type    string
	Name    string
	TblName string
	SQL     string
}

var sqlComment = regexp.MustCompile(`(?m)--[^\n]*`)
var sqlSpace = regexp.MustCompile(`\s+`)

func normalizeSchemaSQL(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	text := sqlComment.ReplaceAllString(value.String, " ")
	text = sqlSpace.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func readSQLiteMaster(t *testing.T, db *sql.DB) []schemaObject {
	t.Helper()
	rows, err := db.Query("SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY type, name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var objects []schemaObject
	for rows.Next() {
		var object schemaObject
		var raw sql.NullString
		if err := rows.Scan(&object.Type, &object.Name, &object.TblName, &raw); err != nil {
			t.Fatal(err)
		}
		object.SQL = normalizeSchemaSQL(raw)
		objects = append(objects, object)
	}
	return objects
}

func compareSchemaObjects(t *testing.T, want, got []schemaObject) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("объектов схемы %d, want %d", len(got), len(want))
	}
	for index := range want {
		if want[index] != got[index] {
			t.Fatalf("объект %d расходится:\nwant %+v\n got %+v", index, want[index], got[index])
		}
	}
}

// Golden DDL: схема, извлечённая из TS-исходников, и схема, которую создаёт Go,
// должны совпадать объект в объект (таблицы, индексы, FTS, триггеры).
//
// Слайс 6 добавил в ту же базу таблицы Telegram-канала (inbox, offset,
// durable-история). Прод-схема TS при этом не меняется: из Go-схемы
// вычитаются только новые объекты с префиксами telegram_/event_streams,
// всё остальное сверяется с golden 1:1.
func TestSchemaMatchesTSGoldenDDL(t *testing.T) {
	goldenSQL, err := os.ReadFile(filepath.Join("testdata", "schema_ts.sql"))
	if err != nil {
		t.Fatal(err)
	}

	goldenPath := filepath.Join(t.TempDir(), "golden.db")
	goldenDB, err := sql.Open("sqlite", dsnFor(goldenPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goldenDB.Exec(string(goldenSQL)); err != nil {
		t.Fatalf("golden DDL: %v", err)
	}
	want := readSQLiteMaster(t, goldenDB)
	goldenDB.Close()

	goPath := filepath.Join(t.TempDir(), "go.db")
	goStore, err := Open(goPath)
	if err != nil {
		t.Fatal(err)
	}
	if !goStore.ensureFTSIndex() {
		t.Fatal("FTS5 недоступен")
	}
	goDB := goStore.db
	got := make([]schemaObject, 0, len(want))
	for _, object := range readSQLiteMaster(t, goDB) {
		if isChannelSchemaObject(object) {
			continue
		}
		got = append(got, object)
	}
	compareSchemaObjects(t, want, got)
	goStore.Close()
}

// channelTables — таблицы слайса 6 поверх той же базы (схема TS не тронута).
var channelTables = map[string]bool{
	"telegram_updates":    true,
	"telegram_poll_state": true,
	"event_streams":       true,
}

func isChannelSchemaObject(object schemaObject) bool {
	if channelTables[object.Name] || channelTables[object.TblName] {
		return true
	}
	return strings.HasPrefix(object.Name, "idx_telegram_")
}

// FTS-состояние, оставленное TS: Go должен дописать только хвост и не
// задублировать уже проиндексированные строки.
func TestCatchUpAfterTSIndexedState(t *testing.T) {
	goldenSQL, err := os.ReadFile(filepath.Join("testdata", "schema_ts.sql"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shared.db")
	db, err := sql.Open("sqlite", dsnFor(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(goldenSQL)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO campaigns (id, slug, title, status, owner_user_id, length, setting, theme, created_at)
         VALUES ('c1', 'search', 'Поиск', 'active', 'u-dm', 'medium', 's', 't', '2026-01-01T00:00:00.000Z')`,
	); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 3; index++ {
		if _, err := db.Exec(
			`INSERT INTO key_events (campaign_id, day, line, permanent, event_id)
             VALUES ('c1', 1, ?, 0, ?)`,
			"Событие "+strconv.Itoa(index), "evt:ts-"+strconv.Itoa(index),
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO fts_journal(source, source_rowid, campaign_id, day, line)
         SELECT 'key_events', id, campaign_id, day, line FROM key_events ORDER BY id LIMIT 2`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO fts_meta(source, last_source_id) VALUES ('key_events', 2)`,
	); err != nil {
		t.Fatal(err)
	}
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if !store.ensureFTSIndex() {
		t.Fatal("FTS5 недоступен")
	}
	var count, last int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM fts_journal WHERE source = 'key_events'",
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("fts_journal строк = %d, want 3", count)
	}
	if err := store.db.QueryRow(
		"SELECT last_source_id FROM fts_meta WHERE source = 'key_events'",
	).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if last != 3 {
		t.Fatalf("last_source_id = %d, want 3", last)
	}
	// Повторный catch-up не должен ничего добавить.
	if !store.ensureFTSIndex() {
		t.Fatal("FTS5 недоступен")
	}
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM fts_journal WHERE source = 'key_events'",
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("после повторного catch-up строк = %d, want 3", count)
	}
}

func TestCompareSourcesCaseInsensitive(t *testing.T) {
	t.Parallel()

	cases := [][2]string{
		{"NPC аня", "NPC Борис"},
		{"локация аня", "локация Борис"},
		{"ёж", "яма"},
	}
	for _, pair := range cases {
		if compareSources(pair[0], pair[1]) >= 0 {
			t.Errorf("compareSources(%q, %q) >= 0", pair[0], pair[1])
		}
	}
}
