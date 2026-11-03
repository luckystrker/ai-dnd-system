package store

// CharacterCard — плоская карточка персонажа для ответа тулов.
type CharacterCard struct {
	ID             string
	Name           string
	Slug           string
	OwnerUserID    string
	CharacterClass string
	Race           string
	Level          int
	Stats          map[string]float64
	HP             *int
	MaxHP          *int
	Conditions     []string
	Inventory      []string
	Abilities      []CharacterAbility
	Gold           *int
	XP             *int
	Location       *string
	Background     *string
	Motivation     *string
	Appearance     *string
	CreatedAt      string
	UpdatedAt      *string
}

// CharacterCard строит плоскую карточку (nil-поля как null в TS → nil).
func CharacterCardOf(sheet CharacterSheet) CharacterCard {
	conditions := sheet.Conditions
	if conditions == nil {
		conditions = []string{}
	}
	inventory := sheet.Inventory
	if inventory == nil {
		inventory = []string{}
	}
	abilities := sheet.Abilities
	if abilities == nil {
		abilities = []CharacterAbility{}
	}
	return CharacterCard{
		ID:             sheet.ID,
		Name:           sheet.Name,
		Slug:           sheet.Slug,
		OwnerUserID:    sheet.OwnerUserID,
		CharacterClass: sheet.CharacterClass,
		Race:           sheet.Race,
		Level:          sheet.Level,
		Stats:          sheet.Stats,
		HP:             sheet.HP,
		MaxHP:          sheet.MaxHP,
		Conditions:     conditions,
		Inventory:      inventory,
		Abilities:      abilities,
		Gold:           sheet.Gold,
		XP:             sheet.XP,
		Location:       sheet.Location,
		Background:     sheet.Background,
		Motivation:     sheet.Motivation,
		Appearance:     sheet.Appearance,
		CreatedAt:      sheet.CreatedAt,
		UpdatedAt:      sheet.UpdatedAt,
	}
}
