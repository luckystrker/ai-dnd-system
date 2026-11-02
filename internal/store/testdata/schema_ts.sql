-- DDL, извлечённый из TS-исходников agent/lib/campaigns/*.ts (store-sqlite,
-- journal, npc, locations, factions, world-state, combat-store, search).
-- Golden-файл для теста: Go-схема должна совпадать с ним объект в объект.
CREATE TABLE IF NOT EXISTS campaigns (
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
);
CREATE INDEX IF NOT EXISTS idx_campaigns_bound_chat
  ON campaigns (bound_chat_id, bound_thread_id);
CREATE TABLE IF NOT EXISTS campaign_members (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  name TEXT,
  username TEXT,
  role TEXT NOT NULL DEFAULT 'player',
  PRIMARY KEY (campaign_id, user_id)
);
CREATE TABLE IF NOT EXISTS characters (
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
);
CREATE TABLE IF NOT EXISTS quests (
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
);
CREATE INDEX IF NOT EXISTS idx_quests_status
  ON quests (campaign_id, status);
CREATE TABLE IF NOT EXISTS open_threads (
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
);
CREATE INDEX IF NOT EXISTS idx_open_threads_status
  ON open_threads (campaign_id, status);
CREATE TABLE IF NOT EXISTS campaign_days (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  date TEXT,
  note TEXT,
  summary TEXT,
  headline TEXT,
  PRIMARY KEY (campaign_id, day)
);
CREATE TABLE IF NOT EXISTS transcript_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  line TEXT NOT NULL,
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
);
CREATE TABLE IF NOT EXISTS key_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  line TEXT NOT NULL,
  permanent INTEGER NOT NULL DEFAULT 0,
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
);
CREATE TABLE IF NOT EXISTS ledger_rows (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  type TEXT NOT NULL,
  line TEXT NOT NULL,
  event_id TEXT,
  UNIQUE (campaign_id, event_id)
);
CREATE TABLE IF NOT EXISTS campaign_summary (
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  day INTEGER NOT NULL,
  text TEXT NOT NULL,
  PRIMARY KEY (campaign_id, day)
);
CREATE TABLE IF NOT EXISTS npcs (
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
CREATE TABLE IF NOT EXISTS locations (
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
);
CREATE TABLE IF NOT EXISTS factions (
  id TEXT PRIMARY KEY,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  standing INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE (campaign_id, slug)
);
CREATE TABLE IF NOT EXISTS world_changes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  category TEXT NOT NULL,
  text TEXT NOT NULL,
  day INTEGER,
  text_norm TEXT NOT NULL,
  UNIQUE (campaign_id, category, text_norm)
);
CREATE TABLE IF NOT EXISTS combat_snapshot (
  campaign_id TEXT PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE,
  combat TEXT NOT NULL,
  enemies TEXT NOT NULL,
  saved_at TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS fts_journal USING fts5(
  source UNINDEXED, source_rowid UNINDEXED, campaign_id UNINDEXED, day UNINDEXED, line,
  tokenize = 'unicode61'
);
CREATE TABLE IF NOT EXISTS fts_meta (
  source TEXT PRIMARY KEY,
  last_source_id INTEGER NOT NULL DEFAULT 0
);
CREATE TRIGGER IF NOT EXISTS fts_journal_delete_transcript AFTER DELETE ON transcript_entries BEGIN
  DELETE FROM fts_journal WHERE source = 'transcript_entries' AND source_rowid = OLD.id;
END;
CREATE TRIGGER IF NOT EXISTS fts_journal_delete_key_events AFTER DELETE ON key_events BEGIN
  DELETE FROM fts_journal WHERE source = 'key_events' AND source_rowid = OLD.id;
END;
CREATE TRIGGER IF NOT EXISTS fts_journal_delete_ledger_rows AFTER DELETE ON ledger_rows BEGIN
  DELETE FROM fts_journal WHERE source = 'ledger_rows' AND source_rowid = OLD.id;
END;
