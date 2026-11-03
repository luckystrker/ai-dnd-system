package store

import (
	"database/sql"
	"strconv"
	"strings"
)

// UpsertWorldChange идемпотентно пишет факт в категорию (по категории+тексту).
func (s *Store) UpsertWorldChange(campaignSlug string, change WorldChange) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	category := jsTrim(change.Category)
	if category == "" {
		category = "Изменения"
	}
	text := jsTrim(collapseNewlines(change.Text))
	if text == "" {
		return nil
	}
	var day any
	if change.Day != nil {
		day = *change.Day
	}
	_, err = s.db.Exec(
		`INSERT INTO world_changes (campaign_id, category, text, day, text_norm) VALUES (?, ?, ?, ?, ?)
         ON CONFLICT(campaign_id, category, text_norm) DO UPDATE SET day = excluded.day`,
		id, category, text, day, strings.ToLower(text),
	)
	return err
}

// ReadWorldState — карта категория → отрендеренные строки (порядок появления).
func (s *Store) ReadWorldState(campaignSlug string) (map[string][]string, []string, error) {
	categories := map[string][]string{}
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return categories, nil, err
	}
	rows, err := s.db.Query(
		"SELECT category, text, day FROM world_changes WHERE campaign_id = ? ORDER BY id", id,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var order []string
	for rows.Next() {
		var category, text string
		var day sql.NullInt64
		if err := rows.Scan(&category, &text, &day); err != nil {
			return nil, nil, err
		}
		dayMark := ""
		if day.Valid {
			dayMark = " (день " + strconv.Itoa(int(day.Int64)) + ")"
		}
		line := "- " + text + dayMark
		if _, exists := categories[category]; !exists {
			order = append(order, category)
		}
		categories[category] = append(categories[category], line)
	}
	return categories, order, rows.Err()
}

// RenderWorldState — читаемый рендер состояния мира.
func (s *Store) RenderWorldState(campaignSlug string) (string, error) {
	categories, order, err := s.ReadWorldState(campaignSlug)
	if err != nil {
		return "", err
	}
	return RenderCategories(categories, order), nil
}

// RenderCategories рендерит категории в «## Категория\n- ...».
func RenderCategories(categories map[string][]string, order []string) string {
	if len(categories) == 0 {
		return ""
	}
	var parts []string
	for _, category := range order {
		items := categories[category]
		if len(items) == 0 {
			continue
		}
		parts = append(parts, "## "+category+"\n"+strings.Join(items, "\n"))
	}
	return strings.Join(parts, "\n\n")
}
