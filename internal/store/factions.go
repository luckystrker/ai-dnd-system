package store

import (
	"database/sql"
	"fmt"
	"strings"
)

const factionColumns = `id, campaign_id, slug, name, description, standing, created_at, updated_at`

// ClampStanding зажимает репутацию в -5..+5 (trunc как Math.trunc).
func ClampStanding(value int) int {
	return max(-5, min(5, value))
}

// UpsertFaction создаёт или обновляет фракцию (поиск по имени без регистра).
func (s *Store) UpsertFaction(campaignIDOrSlug string, input UpsertFactionInput) (*Faction, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	existing, err := s.findFactionByName(campaign.ID, input.Name)
	if err != nil {
		return nil, err
	}
	now := nowISO()
	profile := Faction{
		ID:         newUUID(),
		CampaignID: campaign.ID,
		Name:       input.Name,
		Standing:   0,
		CreatedAt:  now,
	}
	if existing != nil {
		profile = *existing
	}
	if existing == nil {
		slug, err := s.uniqueFactionSlug(campaign.ID, Slugify(input.Name))
		if err != nil {
			return nil, err
		}
		profile.Slug = slug
	}
	if input.Description != nil {
		profile.Description = input.Description
	}
	if input.Standing != nil {
		profile.Standing = ClampStanding(*input.Standing)
	}
	profile.UpdatedAt = ptr(now)
	if err := s.writeFaction(profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

// AdjustStanding меняет репутацию на delta с клампингом.
func (s *Store) AdjustStanding(campaignIDOrSlug, nameOrSlug string, delta int) (*Faction, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	faction, err := s.GetFaction(campaign.ID, nameOrSlug)
	if err != nil {
		return nil, err
	}
	if faction == nil {
		return nil, storeError(fmt.Sprintf("Фракция «%s» не найдена.", nameOrSlug), ErrNotFound)
	}
	faction.Standing = ClampStanding(faction.Standing + delta)
	faction.UpdatedAt = ptr(nowISO())
	if err := s.writeFaction(*faction); err != nil {
		return nil, err
	}
	return faction, nil
}

// GetFaction находит фракцию по id, slug или имени.
func (s *Store) GetFaction(campaignIDOrSlug, nameOrSlug string) (*Faction, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	row, err := s.findFactionRow(campaign.ID, nameOrSlug)
	if err != nil || row == nil {
		return nil, err
	}
	return factionFromRow(*row), nil
}

// ListFactions — фракции кампании по времени создания.
func (s *Store) ListFactions(campaignIDOrSlug string) ([]Faction, error) {
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		"SELECT "+factionColumns+" FROM factions WHERE campaign_id = ? ORDER BY created_at",
		campaign.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var factions []Faction
	for rows.Next() {
		row, err := scanFaction(rows)
		if err != nil {
			return nil, err
		}
		factions = append(factions, *factionFromRow(*row))
	}
	return factions, rows.Err()
}

type factionRow struct {
	ID          string
	CampaignID  string
	Slug        string
	Name        string
	Description sql.NullString
	Standing    int
	CreatedAt   string
	UpdatedAt   sql.NullString
}

func scanFaction(scanner interface{ Scan(...any) error }) (*factionRow, error) {
	var row factionRow
	if err := scanner.Scan(
		&row.ID, &row.CampaignID, &row.Slug, &row.Name, &row.Description,
		&row.Standing, &row.CreatedAt, &row.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &row, nil
}

func factionFromRow(row factionRow) *Faction {
	return &Faction{
		ID:          row.ID,
		CampaignID:  row.CampaignID,
		Name:        row.Name,
		Slug:        row.Slug,
		Description: nullString(row.Description),
		Standing:    ClampStanding(row.Standing),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   nullString(row.UpdatedAt),
	}
}

func (s *Store) findFactionByName(campaignID, name string) (*Faction, error) {
	rows, err := s.db.Query("SELECT "+factionColumns+" FROM factions WHERE campaign_id = ?", campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(name)
	for rows.Next() {
		row, err := scanFaction(rows)
		if err != nil {
			return nil, err
		}
		if strings.ToLower(row.Name) == needle {
			return factionFromRow(*row), nil
		}
	}
	return nil, rows.Err()
}

func (s *Store) findFactionRow(campaignID, nameOrSlug string) (*factionRow, error) {
	rows, err := s.db.Query("SELECT "+factionColumns+" FROM factions WHERE campaign_id = ?", campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(nameOrSlug)
	for rows.Next() {
		row, err := scanFaction(rows)
		if err != nil {
			return nil, err
		}
		if row.ID == nameOrSlug || strings.ToLower(row.Slug) == needle || strings.ToLower(row.Name) == needle {
			return row, nil
		}
	}
	return nil, rows.Err()
}

func (s *Store) uniqueFactionSlug(campaignID, base string) (string, error) {
	return s.uniqueSlug("factions", campaignID, "slug", base)
}

func (s *Store) writeFaction(profile Faction) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO factions (
           id, campaign_id, slug, name, description, standing, created_at, updated_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		profile.ID, profile.CampaignID, profile.Slug, profile.Name,
		nullableStringPtr(profile.Description), profile.Standing,
		profile.CreatedAt, nullableStringPtr(profile.UpdatedAt),
	)
	return err
}
