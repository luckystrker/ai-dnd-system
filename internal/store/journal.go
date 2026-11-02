package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// TranscriptEntry — одна запись транскрипта дня.
type TranscriptEntry struct {
	Kind    string // "player" | "dm" | "action"
	Author  *string
	Text    string
	EventID *string
	At      *string
}

// DayRecord — день целиком.
type DayRecord struct {
	Day      int
	Date     *string
	Note     *string
	Summary  *string
	Headline *string
	Entries  []string
}

// DayHeadline — шапка дня.
type DayHeadline struct {
	Day      int
	Headline string
}

// LedgerFilter — фильтр журнала экономики.
type LedgerFilter struct {
	Day  *int
	Type *LedgerType
}

// AutoDigestMark — маркер авто-дайджеста в поле summary.
const AutoDigestMark = "*(авто-дайджест)*"

// campaignIDOf возвращает id кампании по slug; "", false — кампании нет.
func (s *Store) campaignIDOf(campaignSlug string) (string, bool, error) {
	var id string
	err := s.db.QueryRow("SELECT id FROM campaigns WHERE slug = ?", campaignSlug).Scan(&id)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// EnsureDay создаёт метаданные дня, если их ещё нет.
func (s *Store) ensureDay(campaignSlug string, day int, note, date *string) error {
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	dayDate := todayISO()
	if date != nil {
		dayDate = *date
	}
	var dayNote any
	if note != nil {
		dayNote = *note
	}
	_, err = s.db.Exec(
		"INSERT OR IGNORE INTO campaign_days (campaign_id, day, date, note) VALUES (?, ?, ?, ?)",
		id, day, dayDate, dayNote,
	)
	return err
}

// EnsureDay создаёт метаданные дня, если их ещё нет.
func (s *Store) EnsureDay(campaignSlug string, day int, note, date *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureDay(campaignSlug, day, note, date)
}

// ListDays — номера сохранённых дней по возрастанию.
func (s *Store) ListDays(campaignSlug string) ([]int, error) {
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.db.Query("SELECT day FROM campaign_days WHERE campaign_id = ? ORDER BY day", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var days []int
	for rows.Next() {
		var day int
		if err := rows.Scan(&day); err != nil {
			return nil, err
		}
		days = append(days, day)
	}
	return days, rows.Err()
}

// AppendTranscriptEntry добавляет строку в транскрипт дня (append-only,
// дедуп по eventId через UNIQUE(campaign_id, event_id)).
func (s *Store) AppendTranscriptEntry(campaignSlug string, day int, entry TranscriptEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDay(campaignSlug, day, nil, nil); err != nil {
		return err
	}
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	text := jsTrim(collapseNewlines(entry.Text))
	if text == "" {
		return nil
	}
	time := nowISO()
	if entry.At != nil {
		time = *entry.At
	}
	clock := ""
	if len(time) >= 16 {
		clock = time[11:16]
	}
	var line string
	switch entry.Kind {
	case "player":
		author := "?"
		if entry.Author != nil {
			author = *entry.Author
		}
		line = fmt.Sprintf("- [%s] **Игрок @%s**: %s", clock, author, text)
	case "dm":
		line = fmt.Sprintf("- [%s] **DM**: %s", clock, text)
	default:
		line = fmt.Sprintf("- [%s] *%s*", clock, text)
	}
	var marker any
	if entry.EventID != nil && *entry.EventID != "" {
		marker = "evt:" + *entry.EventID
		line += fmt.Sprintf(" <!-- evt:%s -->", *entry.EventID)
	}
	_, err = s.db.Exec(
		"INSERT OR IGNORE INTO transcript_entries (campaign_id, day, line, event_id) VALUES (?, ?, ?, ?)",
		id, day, line, marker,
	)
	return err
}

// ReadDay читает день целиком; nil, если дня нет.
func (s *Store) ReadDay(campaignSlug string, day int) (*DayRecord, error) {
	return s.ReadDayTail(campaignSlug, day, 0)
}

// ReadDayTail читает последние maxLines записей дня; maxLines <= 0 — все
// (JS slice(-0) тоже отдаёт весь массив).
func (s *Store) ReadDayTail(campaignSlug string, day, maxLines int) (*DayRecord, error) {
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return nil, err
	}
	var (
		record   DayRecord
		date     sql.NullString
		note     sql.NullString
		summary  sql.NullString
		headline sql.NullString
	)
	err = s.db.QueryRow(
		"SELECT day, date, note, summary, headline FROM campaign_days WHERE campaign_id = ? AND day = ?",
		id, day,
	).Scan(&record.Day, &date, &note, &summary, &headline)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record.Date = nullString(date)
	record.Note = nullString(note)
	record.Summary = nullString(summary)
	record.Headline = nullString(headline)

	rows, err := s.db.Query(
		"SELECT line FROM transcript_entries WHERE campaign_id = ? AND day = ? ORDER BY id",
		id, day,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		record.Entries = append(record.Entries, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if maxLines > 0 && len(record.Entries) > maxLines {
		record.Entries = record.Entries[len(record.Entries)-maxLines:]
	}
	return &record, nil
}

// SetDaySummary записывает саммари дня.
func (s *Store) SetDaySummary(campaignSlug string, day int, summary string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDay(campaignSlug, day, nil, nil); err != nil {
		return err
	}
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	_, err = s.db.Exec(
		"UPDATE campaign_days SET summary = ? WHERE campaign_id = ? AND day = ?",
		jsTrim(summary), id, day,
	)
	return err
}

// SetDayHeadline пишет короткую (≤140) шапку дня.
func (s *Store) SetDayHeadline(campaignSlug string, day int, headline string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDay(campaignSlug, day, nil, nil); err != nil {
		return err
	}
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	_, err = s.db.Exec(
		"UPDATE campaign_days SET headline = ? WHERE campaign_id = ? AND day = ?",
		truncateRunes(jsTrim(headline), 140), id, day,
	)
	return err
}

// ListDayHeadlines — шапки всех дней по возрастанию, дни без шапки пропускаются.
func (s *Store) ListDayHeadlines(campaignSlug string) ([]DayHeadline, error) {
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT day, headline FROM campaign_days
         WHERE campaign_id = ? AND headline IS NOT NULL AND headline != '' ORDER BY day`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var headlines []DayHeadline
	for rows.Next() {
		var item DayHeadline
		if err := rows.Scan(&item.Day, &item.Headline); err != nil {
			return nil, err
		}
		headlines = append(headlines, item)
	}
	return headlines, rows.Err()
}

// UpsertCampaignSummaryDay обновляет/добавляет секцию дня накопительного саммари.
func (s *Store) UpsertCampaignSummaryDay(campaignSlug string, day int, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO campaign_summary (campaign_id, day, text) VALUES (?, ?, ?)
         ON CONFLICT(campaign_id, day) DO UPDATE SET text = excluded.text`,
		id, day, jsTrim(text),
	)
	return err
}

// ReadCampaignSummary читает накопительное саммари с секциями «## День N».
func (s *Store) ReadCampaignSummary(campaignSlug string) (string, error) {
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return "", err
	}
	rows, err := s.db.Query(
		"SELECT day, text FROM campaign_summary WHERE campaign_id = ? ORDER BY day", id,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var day int
		var text string
		if err := rows.Scan(&day, &text); err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("## День %d\n\n%s", day, text))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "", nil
	}
	return jsTrim(strings.Join(parts, "\n\n")), nil
}

// contentEventID — детерминированный id по содержимому (sha256, 40 hex).
func contentEventID(campaignID string, day int, clean string, permanent bool) string {
	kind := "r"
	if permanent {
		kind = "p"
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", campaignID, day, kind, clean)))
	return hex.EncodeToString(sum[:])[:40]
}

// AppendKeyEvent добавляет ключевое событие. permanent помечает [важно].
// Без явного eventID дедуп идёт по контентному хешу в колонке event_id.
func (s *Store) AppendKeyEvent(campaignSlug string, day int, text string, eventID *string, permanent bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	clean := jsTrim(collapseNewlines(text))
	if clean == "" {
		return nil
	}
	prefix := ""
	if permanent {
		prefix = "[важно] "
	}
	line := fmt.Sprintf("- %s**День %d**: %s", prefix, day, clean)
	dedupID := contentEventID(id, day, clean, permanent)
	if eventID != nil && *eventID != "" {
		dedupID = *eventID
		line += fmt.Sprintf(" <!-- evt:%s -->", *eventID)
	}
	_, err = s.db.Exec(
		"INSERT OR IGNORE INTO key_events (campaign_id, day, line, permanent, event_id) VALUES (?, ?, ?, ?, ?)",
		id, day, line, boolToInt(permanent), "evt:"+dedupID,
	)
	return err
}

// SplitKeyEvents разделяет события на permanent и обычные.
func (s *Store) SplitKeyEvents(campaignSlug string) (permanent, regular []string, err error) {
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return nil, nil, err
	}
	rows, err := s.db.Query(
		"SELECT line, permanent FROM key_events WHERE campaign_id = ? ORDER BY id", id,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		var flag int
		if err := rows.Scan(&line, &flag); err != nil {
			return nil, nil, err
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if flag == 1 {
			permanent = append(permanent, line)
		} else {
			regular = append(regular, line)
		}
	}
	return permanent, regular, rows.Err()
}

// ReadKeyEvents читает ключевые события построчно.
func (s *Store) ReadKeyEvents(campaignSlug string) (string, error) {
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return "", err
	}
	rows, err := s.db.Query("SELECT line FROM key_events WHERE campaign_id = ? ORDER BY id", id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return jsTrim(strings.Join(lines, "\n")), nil
}

// BuildDayDigest — детерминированный дайджест из записей транскрипта дня.
func BuildDayDigest(entries []string, maxSentences int) string {
	if len(entries) == 0 {
		return ""
	}
	if maxSentences <= 0 {
		maxSentences = 5
	}
	start := len(entries) - maxSentences*2
	if start < 0 {
		start = 0
	}
	tail := entries[start:]
	cleaned := make([]string, 0, len(tail))
	for _, line := range tail {
		text := clockPrefix.ReplaceAllString(line, "")
		text = eventMarkerSuffix.ReplaceAllString(text, "")
		text = jsTrim(text)
		if text != "" {
			cleaned = append(cleaned, text)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	return AutoDigestMark + " " + strings.Join(cleaned, " ")
}

// AppendLedgerRow добавляет запись журнала экономики (дедуп по eventId).
func (s *Store) AppendLedgerRow(campaignSlug string, row LedgerRow, eventID *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	item := jsTrim(collapseNewlines(row.ItemOrGold))
	if item == "" {
		return nil
	}
	verb := "найдено"
	if row.Type == LedgerSpent {
		verb = "потрачено"
	}
	by := ""
	if row.By != nil && *row.By != "" {
		by = fmt.Sprintf(" (%s)", *row.By)
	}
	note := ""
	if row.Note != nil && jsTrim(*row.Note) != "" {
		note = " — " + jsTrim(collapseNewlines(*row.Note))
	}
	line := fmt.Sprintf("- [День %d] %s: %s%s%s", row.Day, verb, item, by, note)
	var marker any
	if eventID != nil && *eventID != "" {
		marker = "evt:" + *eventID
		line += fmt.Sprintf(" <!-- evt:%s -->", *eventID)
	}
	_, err = s.db.Exec(
		"INSERT OR IGNORE INTO ledger_rows (campaign_id, day, type, line, event_id) VALUES (?, ?, ?, ?, ?)",
		id, row.Day, string(row.Type), line, marker,
	)
	return err
}

// ReadLedgerRaw читает журнал экономики сырым текстом.
func (s *Store) ReadLedgerRaw(campaignSlug string) (string, error) {
	lines, err := s.ReadLedger(campaignSlug, nil, 0)
	if err != nil {
		return "", err
	}
	return jsTrim(strings.Join(lines, "\n")), nil
}

// ReadLedger читает журнал с фильтром по дню/типу; maxLines <= 0 — все.
func (s *Store) ReadLedger(campaignSlug string, filter *LedgerFilter, maxLines int) ([]string, error) {
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return nil, err
	}
	query := "SELECT line FROM ledger_rows WHERE campaign_id = ?"
	var args []any
	args = append(args, id)
	if filter != nil && filter.Day != nil {
		query += " AND day = ?"
		args = append(args, *filter.Day)
	}
	if filter != nil && filter.Type != nil {
		query += " AND type = ?"
		args = append(args, string(*filter.Type))
	}
	query += " ORDER BY id"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, nil
}

var (
	clockPrefix       = regexp.MustCompile(`^-\s*\[\d{2}:\d{2}\]\s*`)
	eventMarkerSuffix = regexp.MustCompile(`<!--\s*evt:[^>]*-->\s*$`)
)

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
