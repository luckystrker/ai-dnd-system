package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// LevelUpInput — атомарное повышение уровня: лист, новая способность,
// ключевое событие и строка транскрипта пишутся одной транзакцией.
type LevelUpInput struct {
	NewLevel       int
	MaxHP          *int
	Ability        *CharacterAbility
	KeyEventText   string
	KeyEventID     string
	TranscriptText string
}

// GrantLedgerRow — запись журнала, которую надо записать вместе с выдачей.
type GrantLedgerRow struct {
	Row     LedgerRow
	EventID *string
}

// LevelUpCharacter повышает уровень персонажа одной транзакцией. Сбой на
// любом шаге откатывает всё: повтор после ошибки не оставляет лист на новом
// уровне без способности или ключевого события.
func (s *Store) LevelUpCharacter(campaignIDOrSlug, nameOrSlug string, day int, input LevelUpInput) (*CharacterSheet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	row, err := s.findCharacterRow(campaign.ID, nameOrSlug)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	maxHP := nullInt(row.MaxHP)
	if input.MaxHP != nil {
		maxHP = input.MaxHP
	}
	if _, err := tx.Exec(
		"UPDATE characters SET level = ?, max_hp = ?, updated_at = ? WHERE id = ?",
		input.NewLevel, nullableInt(maxHP), nowISO(), row.ID,
	); err != nil {
		return nil, err
	}
	if input.Ability != nil {
		abilities := parseAbilityArray(row.Abilities)
		known := map[string]bool{}
		for _, ability := range abilities {
			known[strings.ToLower(ability.Name)] = true
		}
		if key := strings.ToLower(input.Ability.Name); !known[key] {
			abilities = append(abilities, *input.Ability)
		}
		if _, err := tx.Exec(
			"UPDATE characters SET abilities = ?, updated_at = ? WHERE id = ?",
			marshalJSON(abilities), nowISO(), row.ID,
		); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(input.KeyEventText) != "" {
		if err := appendKeyEventTx(tx, campaign.ID, day, input.KeyEventText, &input.KeyEventID, false); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(input.TranscriptText) != "" {
		eventID := input.KeyEventID
		if err := appendTranscriptTx(tx, campaign.ID, day, TranscriptEntry{
			Kind: "action", Text: input.TranscriptText, EventID: &eventID,
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return s.mustGetCharacter(row.ID)
}

// GrantCharacterWithLedger аддитивно выдаёт предметы/способности/золото/XP и
// пишет журнал экономики одной транзакцией с листом.
func (s *Store) GrantCharacterWithLedger(campaignIDOrSlug, nameOrSlug string, patch CharacterGrantPatch, ledger []GrantLedgerRow) (*CharacterSheet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	row, err := s.findCharacterRow(campaign.ID, nameOrSlug)
	if err != nil {
		return nil, err
	}
	inventory := parseStringArray(row.Inventory)
	if inventory == nil {
		inventory = []string{}
	}
	abilities := parseAbilityArray(row.Abilities)
	if abilities == nil {
		abilities = []CharacterAbility{}
	}
	conditions := parseStringArray(row.Conditions)
	if conditions == nil {
		conditions = []string{}
	}
	inventory = append(inventory, patch.Inventory...)
	known := map[string]bool{}
	for _, ability := range abilities {
		known[strings.ToLower(ability.Name)] = true
	}
	for _, ability := range patch.Abilities {
		key := strings.ToLower(ability.Name)
		if known[key] {
			continue
		}
		abilities = append(abilities, ability)
		known[key] = true
	}
	for _, condition := range patch.Conditions {
		if !containsString(conditions, condition) {
			conditions = append(conditions, condition)
		}
	}
	gold := parseNullableInt(row.Gold) + patch.Gold
	xp := parseNullableInt(row.XP) + patch.XP

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec(
		`UPDATE characters SET
           inventory = ?, abilities = ?, conditions = ?, gold = ?, xp = ?, updated_at = ?
         WHERE id = ?`,
		marshalJSON(inventory), marshalJSON(abilities), marshalJSON(conditions),
		gold, xp, nowISO(), row.ID,
	); err != nil {
		return nil, err
	}
	for _, entry := range ledger {
		if err := appendLedgerTx(tx, campaign.ID, entry.Row, entry.EventID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return s.mustGetCharacter(row.ID)
}

// appendKeyEventTx — та же строка, что AppendKeyEvent, но в транзакции.
func appendKeyEventTx(ex interface {
	Exec(query string, args ...any) (sql.Result, error)
}, campaignID string, day int, text string, eventID *string, permanent bool) error {
	clean := jsTrim(collapseNewlines(text))
	if clean == "" {
		return nil
	}
	prefix := ""
	if permanent {
		prefix = "[важно] "
	}
	line := fmt.Sprintf("- %s**День %d**: %s", prefix, day, clean)
	dedupID := contentEventID(campaignID, day, clean, permanent)
	if eventID != nil && *eventID != "" {
		dedupID = *eventID
		line += fmt.Sprintf(" <!-- evt:%s -->", *eventID)
	}
	_, err := ex.Exec(
		"INSERT OR IGNORE INTO key_events (campaign_id, day, line, permanent, event_id) VALUES (?, ?, ?, ?, ?)",
		campaignID, day, line, boolToInt(permanent), "evt:"+dedupID,
	)
	return err
}

// appendTranscriptTx — та же строка, что AppendTranscriptEntry, но в транзакции.
func appendTranscriptTx(ex interface {
	Exec(query string, args ...any) (sql.Result, error)
}, campaignID string, day int, entry TranscriptEntry) error {
	if _, err := ex.Exec(
		"INSERT OR IGNORE INTO campaign_days (campaign_id, day, date) VALUES (?, ?, ?)",
		campaignID, day, todayISO(),
	); err != nil {
		return err
	}
	text := jsTrim(collapseNewlines(entry.Text))
	if text == "" {
		return nil
	}
	timeValue := nowISO()
	if entry.At != nil {
		timeValue = *entry.At
	}
	clock := ""
	if len(timeValue) >= 16 {
		clock = timeValue[11:16]
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
	_, err := ex.Exec(
		"INSERT OR IGNORE INTO transcript_entries (campaign_id, day, line, event_id) VALUES (?, ?, ?, ?)",
		campaignID, day, line, marker,
	)
	return err
}

// appendLedgerTx — та же строка, что AppendLedgerRow, но в транзакции.
func appendLedgerTx(ex interface {
	Exec(query string, args ...any) (sql.Result, error)
}, campaignID string, row LedgerRow, eventID *string) error {
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
	_, err := ex.Exec(
		"INSERT OR IGNORE INTO ledger_rows (campaign_id, day, type, line, event_id) VALUES (?, ?, ?, ?, ?)",
		campaignID, row.Day, string(row.Type), line, marker,
	)
	return err
}
