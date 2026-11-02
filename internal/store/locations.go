package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const locationColumns = `id, campaign_id, slug, name, description, connections,
  discovered_day, visited_days, current, created_at, updated_at`

// UpsertLocation создаёт или обновляет локацию (поиск по имени без регистра).
func (s *Store) UpsertLocation(campaignIDOrSlug string, input UpsertLocationInput) (*Location, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	existing, err := s.findLocationByName(campaign.ID, input.Name)
	if err != nil {
		return nil, err
	}
	now := nowISO()
	profile := Location{
		ID:          newUUID(),
		CampaignID:  campaign.ID,
		Name:        input.Name,
		Connections: []LocationConnection{},
		VisitedDays: []int{},
		CreatedAt:   now,
	}
	if existing != nil {
		profile = *existing
	}
	if existing == nil {
		slug, err := s.uniqueLocationSlug(campaign.ID, Slugify(input.Name))
		if err != nil {
			return nil, err
		}
		profile.Slug = slug
	}
	if input.Description != nil {
		profile.Description = input.Description
	}
	if input.DiscoveredDay != nil {
		profile.DiscoveredDay = input.DiscoveredDay
	}
	if input.Connections != nil {
		profile.Connections = *input.Connections
	}
	profile.UpdatedAt = ptr(now)
	if err := s.writeLocation(profile); err != nil {
		return nil, err
	}
	if input.Current != nil {
		if *input.Current {
			if err := s.SetCurrent(campaign.ID, profile.Slug); err != nil {
				return nil, err
			}
			profile.Current = ptr(true)
		} else {
			profile.Current = ptr(false)
			if err := s.writeLocation(profile); err != nil {
				return nil, err
			}
		}
	}
	return &profile, nil
}

// SetCurrent делает локацию текущей, снимая флаг с остальных.
func (s *Store) SetCurrent(campaignIDOrSlug, locationSlug string) error {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return err
	}
	target, err := s.GetLocation(campaign.ID, locationSlug)
	if err != nil {
		return err
	}
	if target == nil {
		return storeError(fmt.Sprintf("Локация «%s» не найдена.", locationSlug), ErrNotFound)
	}
	now := nowISO()
	if _, err := s.db.Exec(
		"UPDATE locations SET current = 0, updated_at = ? WHERE campaign_id = ?", now, campaign.ID,
	); err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE locations SET current = 1, updated_at = ? WHERE id = ?", now, target.ID)
	return err
}

// MarkVisited отмечает день посещения без дубликатов (с сортировкой).
func (s *Store) MarkVisited(campaignIDOrSlug, locationSlug string, day int) (*Location, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	location, err := s.GetLocation(campaign.ID, locationSlug)
	if err != nil || location == nil {
		return nil, err
	}
	if !containsInt(location.VisitedDays, day) {
		location.VisitedDays = append(location.VisitedDays, day)
		sort.Ints(location.VisitedDays)
	}
	location.UpdatedAt = ptr(nowISO())
	if err := s.writeLocation(*location); err != nil {
		return nil, err
	}
	return location, nil
}

// GetLocation находит локацию по id, slug или имени; nil — не найдена.
func (s *Store) GetLocation(campaignIDOrSlug, nameOrSlug string) (*Location, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	row, err := s.findLocationRow(campaign.ID, nameOrSlug)
	if err != nil || row == nil {
		return nil, err
	}
	return locationFromRow(*row), nil
}

// CurrentLocation — локация с current=1.
func (s *Store) CurrentLocation(campaignIDOrSlug string) (*Location, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		"SELECT "+locationColumns+" FROM locations WHERE campaign_id = ? AND current = 1 LIMIT 1",
		campaign.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	row, err := scanLocation(rows)
	if err != nil {
		return nil, err
	}
	return locationFromRow(*row), nil
}

// ListLocations — локации кампании по времени создания.
func (s *Store) ListLocations(campaignIDOrSlug string) ([]Location, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		"SELECT "+locationColumns+" FROM locations WHERE campaign_id = ? ORDER BY created_at",
		campaign.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var locations []Location
	for rows.Next() {
		row, err := scanLocation(rows)
		if err != nil {
			return nil, err
		}
		locations = append(locations, *locationFromRow(*row))
	}
	return locations, rows.Err()
}

type locationRow struct {
	ID            string
	CampaignID    string
	Slug          string
	Name          string
	Description   sql.NullString
	Connections   string
	DiscoveredDay sql.NullInt64
	VisitedDays   string
	Current       int
	CreatedAt     string
	UpdatedAt     sql.NullString
}

func scanLocation(scanner interface{ Scan(...any) error }) (*locationRow, error) {
	var row locationRow
	if err := scanner.Scan(
		&row.ID, &row.CampaignID, &row.Slug, &row.Name, &row.Description,
		&row.Connections, &row.DiscoveredDay, &row.VisitedDays, &row.Current,
		&row.CreatedAt, &row.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &row, nil
}

func locationFromRow(row locationRow) *Location {
	return &Location{
		ID:            row.ID,
		CampaignID:    row.CampaignID,
		Name:          row.Name,
		Slug:          row.Slug,
		Description:   nullString(row.Description),
		Connections:   parseConnections(row.Connections),
		DiscoveredDay: nullInt(row.DiscoveredDay),
		VisitedDays:   parseVisitedDays(row.VisitedDays),
		Current:       ptr(row.Current == 1),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     nullString(row.UpdatedAt),
	}
}

func parseConnections(value string) []LocationConnection {
	connections := []LocationConnection{}
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return connections
	}
	items, ok := parsed.([]any)
	if !ok {
		return connections
	}
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		to, _ := record["to"].(string)
		if to == "" {
			continue
		}
		connection := LocationConnection{To: to}
		if via, ok := record["via"].(string); ok && via != "" {
			connection.Via = &via
		}
		if day, ok := record["discoveredDay"].(float64); ok {
			value := int(day)
			connection.DiscoveredDay = &value
		}
		connections = append(connections, connection)
	}
	return connections
}

func parseVisitedDays(value string) []int {
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return nil
	}
	items, ok := parsed.([]any)
	if !ok {
		return nil
	}
	result := make([]int, 0, len(items))
	for _, item := range items {
		if number, ok := numericToFloat(item); ok {
			result = append(result, int(number))
		}
	}
	return result
}

func serializeConnections(connections []LocationConnection) string {
	items := make([]any, 0, len(connections))
	for _, connection := range connections {
		item := map[string]any{"to": connection.To}
		if connection.Via != nil && *connection.Via != "" {
			item["via"] = *connection.Via
		}
		if connection.DiscoveredDay != nil {
			item["discoveredDay"] = *connection.DiscoveredDay
		}
		items = append(items, item)
	}
	return marshalJSON(items)
}

func (s *Store) findLocationByName(campaignID, name string) (*Location, error) {
	rows, err := s.db.Query(
		"SELECT "+locationColumns+" FROM locations WHERE campaign_id = ?", campaignID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(name)
	for rows.Next() {
		row, err := scanLocation(rows)
		if err != nil {
			return nil, err
		}
		if strings.ToLower(row.Name) == needle {
			return locationFromRow(*row), nil
		}
	}
	return nil, rows.Err()
}

func (s *Store) findLocationRow(campaignID, nameOrSlug string) (*locationRow, error) {
	rows, err := s.db.Query(
		"SELECT "+locationColumns+" FROM locations WHERE campaign_id = ?", campaignID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(nameOrSlug)
	for rows.Next() {
		row, err := scanLocation(rows)
		if err != nil {
			return nil, err
		}
		if row.ID == nameOrSlug || strings.ToLower(row.Slug) == needle || strings.ToLower(row.Name) == needle {
			return row, nil
		}
	}
	return nil, rows.Err()
}

func (s *Store) uniqueLocationSlug(campaignID, base string) (string, error) {
	return s.uniqueSlug("locations", campaignID, "slug", base)
}

func (s *Store) writeLocation(profile Location) error {
	current := 0
	if profile.Current != nil && *profile.Current {
		current = 1
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO locations (
           id, campaign_id, slug, name, description, connections, discovered_day,
           visited_days, current, created_at, updated_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		profile.ID, profile.CampaignID, profile.Slug, profile.Name,
		nullableStringPtr(profile.Description), serializeConnections(profile.Connections),
		nullableInt(profile.DiscoveredDay), marshalJSON(profile.VisitedDays), current,
		profile.CreatedAt, nullableStringPtr(profile.UpdatedAt),
	)
	return err
}

func containsInt(values []int, needle int) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func numericToFloat(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case int:
		return float64(number), true
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(number, "%f", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}
