package store

import (
	"database/sql"
	"encoding/json"
	"github.com/luckystriker/ai-dnd-system/internal/engine"
)

// SerializedEnemy — снимок активного врага.
type SerializedEnemy struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	HP   int    `json:"hp"`
	AC   int    `json:"ac"`
}

// CombatSnapshot — сохранённый бой.
type CombatSnapshot struct {
	Combat  engine.CombatOrder
	Enemies []SerializedEnemy
}

// SaveCombatState сохраняет снимок активного боя (перезапись строки кампании).
func (s *Store) SaveCombatState(campaignSlug string, snapshot CombatSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	_, err = s.db.Exec(
		"INSERT OR REPLACE INTO combat_snapshot (campaign_id, combat, enemies, saved_at) VALUES (?, ?, ?, ?)",
		id, marshalJSON(engine.SerializeCombatOrder(snapshot.Combat)), marshalJSON(snapshot.Enemies), nowISO(),
	)
	return err
}

// LoadCombatState читает снимок; nil — записи нет или данные невалидны.
func (s *Store) LoadCombatState(campaignSlug string) (*CombatSnapshot, error) {
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return nil, err
	}
	var combatText, enemiesText string
	err = s.db.QueryRow(
		"SELECT combat, enemies FROM combat_snapshot WHERE campaign_id = ?", id,
	).Scan(&combatText, &enemiesText)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var combatRaw any
	if err := json.Unmarshal([]byte(combatText), &combatRaw); err != nil {
		return nil, nil
	}
	combat, ok := engine.DeserializeCombatOrder(combatRaw)
	if !ok || !combat.Started || len(combat.Order) == 0 {
		return nil, nil
	}
	var enemiesRaw any
	if err := json.Unmarshal([]byte(enemiesText), &enemiesRaw); err != nil {
		return nil, nil
	}
	enemies := []SerializedEnemy{}
	items, ok := enemiesRaw.([]any)
	if !ok {
		return nil, nil
	}
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		enemyID, idOK := record["id"].(string)
		name, nameOK := record["name"].(string)
		hp, hpOK := jsonInt(record["hp"])
		ac, acOK := jsonInt(record["ac"])
		if !idOK || !nameOK || !hpOK || !acOK {
			continue
		}
		enemies = append(enemies, SerializedEnemy{ID: enemyID, Name: name, HP: hp, AC: ac})
	}
	if len(enemies) == 0 {
		return nil, nil
	}
	return &CombatSnapshot{Combat: combat, Enemies: enemies}, nil
}

// ClearCombatState удаляет снимок боя; идемпотентно.
func (s *Store) ClearCombatState(campaignSlug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok, err := s.campaignIDOf(campaignSlug)
	if err != nil || !ok {
		return err
	}
	_, err = s.db.Exec("DELETE FROM combat_snapshot WHERE campaign_id = ?", id)
	return err
}

func jsonInt(value any) (int, bool) {
	if number, ok := value.(float64); ok {
		return int(number), true
	}
	return 0, false
}
