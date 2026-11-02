package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const npcColumns = `id, campaign_id, slug, name, role, status, location, relationships,
  first_seen_day, last_seen_day, memory, created_at, updated_at`

// UpsertNpc создаёт или обновляет NPC (поиск по имени без учёта регистра).
func (s *Store) UpsertNpc(campaignIDOrSlug string, input NpcUpsertInput) (*NpcWithMemory, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	existing, err := s.findNpcByName(campaign.ID, input.Name)
	if err != nil {
		return nil, err
	}
	now := nowISO()
	profile := NpcProfile{
		ID:            newUUID(),
		CampaignID:    campaign.ID,
		Name:          input.Name,
		Status:        NpcAlive,
		Relationships: map[string]NpcRelationship{},
		CreatedAt:     now,
	}
	memory := ""
	if existing != nil {
		profile = existing.NpcProfile
		memory = existing.Memory
	}
	if existing == nil {
		slug, err := s.uniqueNpcSlug(campaign.ID, Slugify(input.Name))
		if err != nil {
			return nil, err
		}
		profile.Slug = slug
	}
	if input.Role != nil {
		profile.Role = input.Role
	}
	if input.Status != nil {
		profile.Status = *input.Status
	}
	if input.Location != nil {
		profile.Location = input.Location
	}
	if input.FirstSeenDay != nil {
		profile.FirstSeenDay = input.FirstSeenDay
	}
	if input.LastSeenDay != nil {
		profile.LastSeenDay = input.LastSeenDay
	}
	for name, relation := range input.Relationships {
		profile.Relationships[name] = relation
	}
	profile.UpdatedAt = ptr(now)

	if input.MemoryAppend != nil {
		appendText := strings.TrimSpace(*input.MemoryAppend)
		if appendText != "" {
			dayRun := inlineDayRun.FindString(appendText)
			var inlineDay *int
			if dayRun != "" {
				if match := inlineDayValue.FindStringSubmatch(dayRun); match != nil {
					value, convErr := strconv.Atoi(match[1])
					if convErr == nil {
						inlineDay = &value
					}
				}
				appendText = strings.TrimSpace(appendText[len(dayRun):])
			}
			day := input.MemoryAppendDay
			if day == nil {
				day = inlineDay
			}
			dayMark := ""
			if day != nil {
				dayMark = fmt.Sprintf(" [День %d]", *day)
			}
			line := fmt.Sprintf("-%s %s", dayMark, collapseNewlines(appendText))
			if memory != "" {
				memory += "\n" + line
			} else {
				memory = line
			}
		}
	}
	if err := s.writeNpc(profile, memory); err != nil {
		return nil, err
	}
	return &NpcWithMemory{NpcProfile: profile, Memory: memory}, nil
}

// GetNpc возвращает карточку NPC вместе с памятью.
func (s *Store) GetNpc(campaignIDOrSlug, nameOrSlug string) (*NpcWithMemory, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	row, err := s.findNpcRow(campaign.ID, nameOrSlug)
	if err != nil || row == nil {
		return nil, err
	}
	profile := npcFromRow(*row)
	return &NpcWithMemory{NpcProfile: *profile, Memory: row.Memory}, nil
}

// ListNpcs — NPC кампании по времени создания.
func (s *Store) ListNpcs(campaignIDOrSlug string) ([]NpcProfile, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		"SELECT "+npcColumns+" FROM npcs WHERE campaign_id = ? ORDER BY created_at", campaign.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var npcs []NpcProfile
	for rows.Next() {
		row, err := scanNpc(rows)
		if err != nil {
			return nil, err
		}
		npcs = append(npcs, *npcFromRow(*row))
	}
	return npcs, rows.Err()
}

// LastMemoryLine — последняя осмысленная строка памяти NPC, обрезанная.
func (s *Store) LastMemoryLine(campaignIDOrSlug, npcSlug string, maxChars int) (string, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return "", err
	}
	rows, err := s.db.Query("SELECT slug, memory FROM npcs WHERE campaign_id = ?", campaign.ID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	needle := strings.ToLower(npcSlug)
	memory := ""
	found := false
	for rows.Next() {
		var slug, value string
		if err := rows.Scan(&slug, &value); err != nil {
			return "", err
		}
		if strings.ToLower(slug) == needle {
			memory = value
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	var lines []string
	for _, line := range strings.Split(memory, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) == 0 {
		return "", nil
	}
	last := lines[len(lines)-1]
	if maxChars <= 0 {
		maxChars = 100
	}
	if len([]rune(last)) > maxChars {
		return string([]rune(last)[:maxChars]) + "…", nil
	}
	return last, nil
}

type npcRow struct {
	ID            string
	CampaignID    string
	Slug          string
	Name          string
	Role          sql.NullString
	Status        string
	Location      sql.NullString
	Relationships string
	FirstSeenDay  sql.NullInt64
	LastSeenDay   sql.NullInt64
	Memory        string
	CreatedAt     string
	UpdatedAt     sql.NullString
}

func scanNpc(scanner interface{ Scan(...any) error }) (*npcRow, error) {
	var row npcRow
	if err := scanner.Scan(
		&row.ID, &row.CampaignID, &row.Slug, &row.Name, &row.Role, &row.Status,
		&row.Location, &row.Relationships, &row.FirstSeenDay, &row.LastSeenDay,
		&row.Memory, &row.CreatedAt, &row.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &row, nil
}

func npcFromRow(row npcRow) *NpcProfile {
	status := NpcAlive
	if row.Status == string(NpcDead) || row.Status == string(NpcUnknown) {
		status = NpcStatus(row.Status)
	}
	return &NpcProfile{
		ID:            row.ID,
		CampaignID:    row.CampaignID,
		Name:          row.Name,
		Slug:          row.Slug,
		Role:          nullString(row.Role),
		Status:        status,
		Location:      nullString(row.Location),
		Relationships: parseNpcRelationships(row.Relationships),
		FirstSeenDay:  nullInt(row.FirstSeenDay),
		LastSeenDay:   nullInt(row.LastSeenDay),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     nullString(row.UpdatedAt),
	}
}

func parseNpcRelationships(value string) map[string]NpcRelationship {
	relationships := map[string]NpcRelationship{}
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return relationships
	}
	record, ok := parsed.(map[string]any)
	if !ok {
		return relationships
	}
	for name, item := range record {
		relation, ok := item.(map[string]any)
		if !ok {
			continue
		}
		attitude := 0
		if number, ok := relation["attitude"].(float64); ok {
			attitude = int(number)
		}
		entry := NpcRelationship{Attitude: attitude}
		if notes, ok := relation["notes"].(string); ok && notes != "" {
			entry.Notes = &notes
		}
		relationships[name] = entry
	}
	return relationships
}

func serializeNpcRelationships(relationships map[string]NpcRelationship) string {
	record := map[string]any{}
	for name, relation := range relationships {
		item := map[string]any{"attitude": relation.Attitude}
		if relation.Notes != nil && *relation.Notes != "" {
			item["notes"] = *relation.Notes
		}
		record[name] = item
	}
	return marshalJSON(record)
}

func (s *Store) findNpcByName(campaignID, name string) (*NpcWithMemory, error) {
	rows, err := s.db.Query("SELECT "+npcColumns+" FROM npcs WHERE campaign_id = ?", campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(name)
	for rows.Next() {
		row, err := scanNpc(rows)
		if err != nil {
			return nil, err
		}
		if strings.ToLower(row.Name) == needle {
			return &NpcWithMemory{NpcProfile: *npcFromRow(*row), Memory: row.Memory}, nil
		}
	}
	return nil, rows.Err()
}

func (s *Store) findNpcRow(campaignID, nameOrSlug string) (*npcRow, error) {
	rows, err := s.db.Query("SELECT "+npcColumns+" FROM npcs WHERE campaign_id = ?", campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(nameOrSlug)
	for rows.Next() {
		row, err := scanNpc(rows)
		if err != nil {
			return nil, err
		}
		if row.ID == nameOrSlug || strings.ToLower(row.Slug) == needle || strings.ToLower(row.Name) == needle {
			return row, nil
		}
	}
	return nil, rows.Err()
}

func (s *Store) uniqueNpcSlug(campaignID, base string) (string, error) {
	return s.uniqueSlug("npcs", campaignID, "slug", base)
}

func (s *Store) writeNpc(profile NpcProfile, memory string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO npcs (
           id, campaign_id, slug, name, role, status, location, relationships,
           first_seen_day, last_seen_day, memory, created_at, updated_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		profile.ID, profile.CampaignID, profile.Slug, profile.Name,
		nullableStringPtr(profile.Role), string(profile.Status),
		nullableStringPtr(profile.Location), serializeNpcRelationships(profile.Relationships),
		nullableInt(profile.FirstSeenDay), nullableInt(profile.LastSeenDay),
		memory, profile.CreatedAt, nullableStringPtr(profile.UpdatedAt),
	)
	return err
}
