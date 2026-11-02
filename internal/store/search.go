package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// MemoryHit — один результат поиска по памяти кампании.
type MemoryHit struct {
	Source  string
	Day     *int
	Snippet string
}

// SearchOptions — параметры поиска.
type SearchOptions struct {
	Limit     int
	Context   int
	PerSource int
}

// Значения по умолчанию как в TS.
const (
	DefaultLimit   = 20
	DefaultContext = 120
	defaultPerSrc  = 3
)

var queryTermPattern = regexp.MustCompile(`[\p{L}\p{N}]{2,}`)
var whitespaceRuns = regexp.MustCompile(`[\s\p{Z}]+`)

// QueryTerms разбивает запрос на нормализованные слова длиной ≥ 2.
func QueryTerms(query string) []string {
	matches := queryTermPattern.FindAllString(strings.ToLower(query), -1)
	seen := map[string]bool{}
	var terms []string
	for _, match := range matches {
		if seen[match] {
			continue
		}
		seen[match] = true
		terms = append(terms, match)
	}
	return terms
}

// Normalize — нижний регистр и единые пробелы.
func Normalize(text string) string {
	return strings.TrimSpace(whitespaceRuns.ReplaceAllString(strings.ToLower(text), " "))
}

// SnippetAround строит сниппет вокруг первого совпадения любого терма.
func SnippetAround(text string, terms []string, context int) *string {
	lower := Normalize(text)
	if lower == "" {
		return nil
	}
	earliest := -1
	for _, term := range terms {
		byteIndex := strings.Index(lower, term)
		if byteIndex < 0 {
			continue
		}
		runeIndex := len([]rune(lower[:byteIndex]))
		if earliest == -1 || runeIndex < earliest {
			earliest = runeIndex
		}
	}
	if earliest == -1 {
		return nil
	}
	original := []rune(text)
	start := earliest - context/2
	if start < 0 {
		start = 0
	}
	end := earliest + context
	if end > len(original) {
		end = len(original)
	}
	if start > end {
		start = end
	}
	prefix := ""
	if start > 0 {
		prefix = "…"
	}
	suffix := ""
	if end < len(original) {
		suffix = "…"
	}
	return ptr(prefix + string(original[start:end]) + suffix)
}

// BuildMatchQuery — FTS5 MATCH: кавычки + префикс-звёздочка снаружи, AND.
func BuildMatchQuery(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		parts = append(parts, `"`+term+`"*`)
	}
	return strings.Join(parts, " AND ")
}

var ftsSchema = []string{
	`CREATE VIRTUAL TABLE IF NOT EXISTS fts_journal USING fts5(
  source UNINDEXED, source_rowid UNINDEXED, campaign_id UNINDEXED, day UNINDEXED, line,
  tokenize = 'unicode61'
)`,
	`CREATE TABLE IF NOT EXISTS fts_meta (
  source TEXT PRIMARY KEY,
  last_source_id INTEGER NOT NULL DEFAULT 0
)`,
	`CREATE TRIGGER IF NOT EXISTS fts_journal_delete_transcript AFTER DELETE ON transcript_entries BEGIN
  DELETE FROM fts_journal WHERE source = 'transcript_entries' AND source_rowid = OLD.id;
END`,
	`CREATE TRIGGER IF NOT EXISTS fts_journal_delete_key_events AFTER DELETE ON key_events BEGIN
  DELETE FROM fts_journal WHERE source = 'key_events' AND source_rowid = OLD.id;
END`,
	`CREATE TRIGGER IF NOT EXISTS fts_journal_delete_ledger_rows AFTER DELETE ON ledger_rows BEGIN
  DELETE FROM fts_journal WHERE source = 'ledger_rows' AND source_rowid = OLD.id;
END`,
}

// ensureFTSIndex создаёт FTS5-индекс и доиндексирует строки, добавленные с
// прошлого поиска. false — FTS недоступен, все источники идут JS-сканом.
func (s *Store) ensureFTSIndex() bool {
	for _, statement := range ftsSchema {
		if _, err := s.db.Exec(statement); err != nil {
			s.ftsEnabled = false
			return false
		}
	}
	s.ftsEnabled = true
	for _, source := range memorySources {
		if !source.FTS {
			continue
		}
		var last int
		err := s.db.QueryRow("SELECT last_source_id FROM fts_meta WHERE source = ?", source.Table).Scan(&last)
		if err != nil && err != sql.ErrNoRows {
			continue
		}
		rows, err := s.db.Query(
			fmt.Sprintf("SELECT id, campaign_id, day, line FROM %s WHERE id > ? ORDER BY id", source.Table), last,
		)
		if err != nil {
			continue
		}
		type ftsRow struct {
			id         int
			campaignID string
			day        int
			line       string
		}
		var batch []ftsRow
		for rows.Next() {
			var item ftsRow
			if err := rows.Scan(&item.id, &item.campaignID, &item.day, &item.line); err != nil {
				batch = nil
				break
			}
			batch = append(batch, item)
		}
		rows.Close()
		if len(batch) == 0 {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			continue
		}
		for _, item := range batch {
			if _, err := tx.Exec(
				"INSERT INTO fts_journal(source, source_rowid, campaign_id, day, line) VALUES (?, ?, ?, ?, ?)",
				source.Table, item.id, item.campaignID, item.day, item.line,
			); err != nil {
				tx.Rollback()
				tx = nil
				break
			}
		}
		if tx == nil {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO fts_meta(source, last_source_id) VALUES (?, ?)
             ON CONFLICT(source) DO UPDATE SET last_source_id = excluded.last_source_id`,
			source.Table, batch[len(batch)-1].id,
		); err != nil {
			tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			continue
		}
	}
	return true
}

// memorySource — описание источника памяти (SQL + форматирование хита).
type memorySource struct {
	Table    string
	IDColumn string
	Select   string
	OrderBy  string
	Source   func(row map[string]any) string
	Day      func(row map[string]any) *int
	Snippet  func(row map[string]any) string
	FTS      bool
}

var memorySources = []memorySource{
	{
		Table: "transcript_entries", Select: "line, day", OrderBy: "day DESC, id DESC",
		Source: func(row map[string]any) string {
			return fmt.Sprintf("транскрипт, день %d", numberOrZero(row["day"]))
		},
		Day: func(row map[string]any) *int { return numberPtr(row["day"]) },
		Snippet: func(row map[string]any) string {
			return asString(row["line"])
		},
		FTS: true,
	},
	{
		Table: "key_events", Select: "line, day", OrderBy: "day DESC, id DESC",
		Source: func(row map[string]any) string {
			return fmt.Sprintf("ключевое событие, день %d", numberOrZero(row["day"]))
		},
		Day: func(row map[string]any) *int { return numberPtr(row["day"]) },
		Snippet: func(row map[string]any) string {
			return asString(row["line"])
		},
		FTS: true,
	},
	{
		Table: "ledger_rows", Select: "line, day", OrderBy: "day DESC, id DESC",
		Source: func(row map[string]any) string {
			return fmt.Sprintf("журнал лута, день %d", numberOrZero(row["day"]))
		},
		Day: func(row map[string]any) *int { return numberPtr(row["day"]) },
		Snippet: func(row map[string]any) string {
			return asString(row["line"])
		},
		FTS: true,
	},
	{
		Table: "world_changes", Select: "text, category, day", OrderBy: "day IS NULL, day DESC, id DESC",
		Source: func(map[string]any) string { return "состояние мира" },
		Day:    func(row map[string]any) *int { return numberPtr(row["day"]) },
		Snippet: func(row map[string]any) string {
			category := asString(row["category"])
			if category != "" {
				return category + ": " + asString(row["text"])
			}
			return asString(row["text"])
		},
	},
	{
		Table: "npcs", Select: "name, role, memory, last_seen_day",
		OrderBy: "last_seen_day IS NULL, last_seen_day DESC, id DESC",
		Source:  func(row map[string]any) string { return "NPC " + asString(row["name"]) },
		Day:     func(row map[string]any) *int { return numberPtr(row["last_seen_day"]) },
		Snippet: func(row map[string]any) string {
			return joinFields([]string{asString(row["name"]), asString(row["role"]), asString(row["memory"])})
		},
	},
	{
		Table: "locations", Select: "name, description, discovered_day",
		OrderBy: "discovered_day IS NULL, discovered_day DESC, id DESC",
		Source:  func(row map[string]any) string { return "локация " + asString(row["name"]) },
		Day:     func(row map[string]any) *int { return numberPtr(row["discovered_day"]) },
		Snippet: func(row map[string]any) string {
			return joinFields([]string{asString(row["name"]), asString(row["description"])})
		},
	},
	{
		Table: "factions", Select: "name, description", OrderBy: "id DESC",
		Source: func(row map[string]any) string { return "фракция " + asString(row["name"]) },
		Day:    func(map[string]any) *int { return nil },
		Snippet: func(row map[string]any) string {
			return joinFields([]string{asString(row["name"]), asString(row["description"])})
		},
	},
	{
		Table: "characters", Select: "name, background, motivation", OrderBy: "id DESC",
		Source: func(row map[string]any) string { return "персонаж " + asString(row["name"]) },
		Day:    func(map[string]any) *int { return nil },
		Snippet: func(row map[string]any) string {
			return joinFields([]string{asString(row["name"]), asString(row["background"]), asString(row["motivation"])})
		},
	},
	{
		Table: "quests", Select: "title, objective, created_day", OrderBy: "created_day DESC, id DESC",
		Source: func(row map[string]any) string { return "квест " + asString(row["title"]) },
		Day:    func(row map[string]any) *int { return numberPtr(row["created_day"]) },
		Snippet: func(row map[string]any) string {
			return joinFields([]string{asString(row["title"]), asString(row["objective"])})
		},
	},
	{
		Table: "campaigns", IDColumn: "id", Select: "title, description, setting", OrderBy: "id DESC",
		Source: func(map[string]any) string { return "кампания" },
		Day:    func(map[string]any) *int { return nil },
		Snippet: func(row map[string]any) string {
			return joinFields([]string{asString(row["title"]), asString(row["description"]), asString(row["setting"])})
		},
	},
}

// SearchCampaignMemory ищет совпадения по всем таблицам памяти кампании.
func (s *Store) SearchCampaignMemory(slug, query string, options SearchOptions) ([]MemoryHit, error) {
	terms := QueryTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	limit := options.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	context := options.Context
	if context <= 0 {
		context = DefaultContext
	}
	perSource := options.PerSource
	if perSource <= 0 {
		perSource = defaultPerSrc
	}

	campaignID, ok, err := s.campaignIDOf(slug)
	if err != nil || !ok {
		return nil, err
	}
	s.ensureFTSIndex()

	var hits []MemoryHit
	for _, source := range memorySources {
		found, searchErr := s.searchSource(campaignID, source, terms, context, perSource)
		if searchErr != nil {
			// Таблицы-источника могло не быть — пропускаем, остальные ищут.
			continue
		}
		hits = append(hits, found...)
	}
	sort.SliceStable(hits, func(a, b int) bool {
		dayA, dayB := -1, -1
		if hits[a].Day != nil {
			dayA = *hits[a].Day
		}
		if hits[b].Day != nil {
			dayB = *hits[b].Day
		}
		if dayA != dayB {
			return dayB < dayA
		}
		return hits[a].Source < hits[b].Source
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (s *Store) searchSource(campaignID string, source memorySource, terms []string, context, perSource int) ([]MemoryHit, error) {
	if source.FTS && s.ftsEnabled {
		hits, err := s.searchSourceFTS(campaignID, source, terms, context, perSource)
		if err == nil && len(hits) > 0 {
			return hits, nil
		}
	}
	idColumn := source.IDColumn
	if idColumn == "" {
		idColumn = "campaign_id"
	}
	rows, err := s.db.Query(
		fmt.Sprintf("SELECT %s FROM %s WHERE %s = ? ORDER BY %s", source.Select, source.Table, idColumn, source.OrderBy),
		campaignID,
	)
	if err != nil {
		return nil, err
	}
	items, err := readRowsAsMaps(rows)
	if err != nil {
		return nil, err
	}
	var hits []MemoryHit
	for _, row := range items {
		text := source.Snippet(row)
		if !matchesAllTerms(text, terms) {
			continue
		}
		snippet := SnippetAround(text, terms, context)
		if snippet == nil {
			continue
		}
		hits = append(hits, MemoryHit{Source: source.Source(row), Day: source.Day(row), Snippet: *snippet})
		if len(hits) >= perSource {
			break
		}
	}
	return hits, nil
}

func (s *Store) searchSourceFTS(campaignID string, source memorySource, terms []string, context, perSource int) ([]MemoryHit, error) {
	fetch := perSource*3 + 4
	rows, err := s.db.Query(
		`SELECT day, line FROM fts_journal
         WHERE fts_journal MATCH ? AND campaign_id = ? AND source = ?
         ORDER BY day DESC, rowid DESC LIMIT ?`,
		BuildMatchQuery(terms), campaignID, source.Table, fetch,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []MemoryHit
	for rows.Next() {
		var day int
		var line string
		if err := rows.Scan(&day, &line); err != nil {
			return nil, err
		}
		snippet := SnippetAround(line, terms, context)
		if snippet == nil {
			continue
		}
		hits = append(hits, MemoryHit{
			Source:  source.Source(map[string]any{"day": int64(day)}),
			Day:     &day,
			Snippet: *snippet,
		})
		if len(hits) >= perSource {
			break
		}
	}
	return hits, rows.Err()
}

func matchesAllTerms(text string, terms []string) bool {
	lower := Normalize(text)
	for _, term := range terms {
		if !strings.Contains(lower, term) {
			return false
		}
	}
	return true
}

func joinFields(fields []string) string {
	cleaned := make([]string, 0, len(fields))
	for _, field := range fields {
		value := strings.TrimSpace(whitespaceRuns.ReplaceAllString(field, " "))
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return strings.Join(cleaned, " ")
}

func readRowsAsMaps(rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for index, column := range columns {
			row[column] = values[index]
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func numberOrZero(value any) int {
	if number := numberPtr(value); number != nil {
		return *number
	}
	return 0
}

func numberPtr(value any) *int {
	switch number := value.(type) {
	case int64:
		parsed := int(number)
		return &parsed
	case int:
		parsed := number
		return &parsed
	case float64:
		parsed := int(number)
		return &parsed
	}
	return nil
}
