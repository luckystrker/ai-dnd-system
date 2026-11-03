package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// legacySchema — схема, какой её создавал TS до C2/C-способностей: кампании без
// time_of_day/in_game_date/weather, персонажи без abilities.
const legacySchema = `
CREATE TABLE campaigns (
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
  created_at TEXT NOT NULL
);
CREATE TABLE campaign_members (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  name TEXT,
  username TEXT,
  role TEXT NOT NULL DEFAULT 'player',
  PRIMARY KEY (campaign_id, user_id)
);
CREATE TABLE characters (
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
  gold INTEGER,
  xp INTEGER,
  location TEXT,
  updated_at TEXT,
  created_at TEXT NOT NULL,
  UNIQUE (campaign_id, slug)
);
CREATE TABLE key_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  line TEXT NOT NULL,
  permanent INTEGER NOT NULL DEFAULT 0,
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
);
CREATE TABLE npcs (
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
);
`

func TestOpenLegacyTSDBSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO campaigns (id, slug, title, status, owner_user_id, length, setting, theme, description, current_day, created_at)
          VALUES ('c1', 'old-campaign', 'Старая кампания', 'active', 'u-dm', 'medium', 's', 't', 'Описание', 3, '2026-01-01T00:00:00.000Z')`, nil},
		{`INSERT INTO campaign_members (campaign_id, user_id, name, role) VALUES ('c1', 'u-dm', 'Ведущий', 'dm')`, nil},
		{`INSERT INTO characters (id, campaign_id, slug, name, owner_user_id, class, race, level, stats, hp, max_hp, created_at)
          VALUES ('ch1', 'c1', 'ariya', 'Ария', 'u-dm', 'плут', 'эльф', 2, '{"dex":16,"str":9}', 12, 14, '2026-01-01T00:00:00.000Z')`, nil},
		{`INSERT INTO key_events (campaign_id, day, line, permanent) VALUES ('c1', 1, '- **День 1**: Старое событие.', 0)`, nil},
		{`INSERT INTO key_events (campaign_id, day, line, permanent) VALUES ('c1', 1, '- **День 1**: Старое событие.', 0)`, nil},
		{`INSERT INTO npcs (id, campaign_id, slug, name, status, memory, created_at)
          VALUES ('n1', 'c1', 'born', 'Борн', 'alive', '- [День 1] [День 1] Старая память.', '2026-01-01T00:00:00.000Z')`, nil},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy: %v", err)
	}
	defer store.Close()

	// Миграции колонок.
	for _, column := range []string{"time_of_day", "in_game_date", "weather"} {
		if ok, err := store.hasColumn("campaigns", column); err != nil || !ok {
			t.Fatalf("колонка campaigns.%s: %v, %v", column, ok, err)
		}
	}
	if ok, err := store.hasColumn("characters", "abilities"); err != nil || !ok {
		t.Fatalf("колонка characters.abilities: %v, %v", ok, err)
	}

	campaign, err := store.GetCampaign("old-campaign")
	if err != nil || campaign == nil {
		t.Fatalf("GetCampaign = %v, %v", campaign, err)
	}
	if campaign.CurrentDay == nil || *campaign.CurrentDay != 3 || campaign.TimeOfDay != nil {
		t.Fatalf("campaign = %+v", campaign)
	}
	sheets, err := store.ListCharacters("c1")
	if err != nil || len(sheets) != 1 {
		t.Fatalf("ListCharacters = %v, %v", sheets, err)
	}
	if sheets[0].Stats["dex"] != 16 || sheets[0].Abilities != nil || sheets[0].HP == nil || *sheets[0].HP != 12 {
		t.Fatalf("sheet = %+v", sheets[0])
	}

	// Бэкфилл event_id и дедуп старых key_events.
	events, err := store.ReadKeyEvents("old-campaign")
	if err != nil {
		t.Fatal(err)
	}
	if events != "- **День 1**: Старое событие." {
		t.Fatalf("events = %q", events)
	}
	var eventID *string
	if err := store.db.QueryRow("SELECT event_id FROM key_events LIMIT 1").Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if eventID == nil || *eventID == "" {
		t.Fatal("event_id не забэкфиллен")
	}

	// Починка двойного маркера памяти NPC.
	npc, err := store.GetNpc("old-campaign", "Борн")
	if err != nil || npc == nil {
		t.Fatalf("GetNpc = %v, %v", npc, err)
	}
	if npc.Memory != "- [День 1] Старая память." {
		t.Fatalf("memory = %q", npc.Memory)
	}

	// Запись в старую базу работает после миграций.
	updated, err := store.SetEnvironment("old-campaign", EnvironmentPatch{
		TimeOfDay: ptr(TimeEvening), Weather: ptr("туман"), InGameDate: ptr("3 день Туманов"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TimeOfDay == nil || *updated.TimeOfDay != TimeEvening || updated.Weather == nil || *updated.Weather != "туман" {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := store.UpdateCharacter("old-campaign", "Ария", CharacterStatePatch{HP: ptr(5)}); err != nil {
		t.Fatalf("UpdateCharacter: %v", err)
	}
}

// hasColumn — тестовый доступ к проверке колонок (та же логика, что в migrate).
func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull, pk  int
			defaultValue any
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func TestKeyEventBackfillHandlesExistingHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collision.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO campaigns (id, slug, title, status, owner_user_id, length, setting, theme, description, created_at)
         VALUES ('c1', 'old-campaign', 'Старая кампания', 'active', 'u-dm', 'medium', 's', 't', '', '2026-01-01T00:00:00.000Z')`,
	); err != nil {
		t.Fatal(err)
	}
	clean := "Старое событие."
	hash := "evt:" + contentEventID("c1", 1, clean, false)
	// Строка новой версии уже имеет вычисленный хеш; рядом лежит NULL-строка
	// с тем же содержимым — бэкфилл обязан удалить её, а не упасть на UNIQUE.
	if _, err := db.Exec(
		`INSERT INTO key_events (campaign_id, day, line, permanent, event_id) VALUES ('c1', 1, ?, 0, ?)`,
		"- **День 1**: "+clean, hash,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO key_events (campaign_id, day, line, permanent, event_id) VALUES ('c1', 1, ?, 0, NULL)`,
		"- **День 1**: "+clean,
	); err != nil {
		t.Fatal(err)
	}
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open не должен падать на коллизии бэкфилла: %v", err)
	}
	defer store.Close()
	var rows, nulls int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM key_events").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM key_events WHERE event_id IS NULL").Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || nulls != 0 {
		t.Fatalf("rows=%d nulls=%d, want 1/0", rows, nulls)
	}
}
