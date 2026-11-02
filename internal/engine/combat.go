package engine

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// CombatantSide — сторона участника боя.
type CombatantSide string

const (
	SideParty CombatantSide = "party"
	SideEnemy CombatantSide = "enemy"
)

// CombatantInput — участник, которого передал тул инициативы.
type CombatantInput struct {
	Name string
	// ID — стабильный идентификатор; пустой — генерируется из имени (slug).
	ID    string
	Side  CombatantSide
	Bonus int
	// HP — враги: текущие хиты (для урона в бою).
	HP *int
	// AC — враги: класс брони (для проверки попаданий).
	AC *int
}

// CombatantEntry — участник в порядке ходов.
type CombatantEntry struct {
	ID    string
	Name  string
	Side  CombatantSide
	Bonus int
	Roll  int
	Total int
	HP    *int
	MaxHP *int
	AC    *int
	// Dodging — действие Dodge: помеха атакам по участнику до начала его
	// следующего хода; NextCombatant сбрасывает флаг тому, чей ход наступил.
	Dodging bool
}

// CombatOrder — состояние порядка ходов.
type CombatOrder struct {
	Started bool
	Round   int
	// Current — индекс текущего участника в Order; -1, пока бой не начат.
	Current int
	// Acted — текущий участник уже совершил атаку в этом ходу.
	Acted bool
	Order []CombatantEntry
}

// EmptyCombatOrder — пустой бой.
func EmptyCombatOrder() CombatOrder {
	return CombatOrder{Started: false, Round: 1, Current: -1, Acted: false, Order: []CombatantEntry{}}
}

var whitespacePattern = regexp.MustCompile(`\s+`)

// slugify превращает имя в id: нижний регистр, пробелы → дефисы, только
// буквы/цифры/дефис.
func slugify(name string) string {
	slug := whitespacePattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	var builder strings.Builder
	for _, r := range slug {
		if r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return "combatant"
	}
	return builder.String()
}

// RollInitiative бросает инициативу каждому участнику и сортирует по убыванию
// (ничьи — по имени). Одинаковые slug-id дедуплицируются суффиксами -2, -3, ...
func RollInitiative(combatants []CombatantInput, random RandomSource) []CombatantEntry {
	source := randomOr(random)
	usedIDs := make(map[string]bool, len(combatants))
	entries := make([]CombatantEntry, 0, len(combatants))
	for _, combatant := range combatants {
		base := strings.TrimSpace(combatant.ID)
		if base == "" {
			base = slugify(combatant.Name)
		}
		id := base
		if usedIDs[id] {
			for suffix := 2; ; suffix++ {
				candidate := base + "-" + strconv.Itoa(suffix)
				if !usedIDs[candidate] {
					id = candidate
					break
				}
			}
		}
		usedIDs[id] = true
		roll := 1 + int(source()*20)
		entries = append(entries, CombatantEntry{
			ID:    id,
			Name:  combatant.Name,
			Side:  combatant.Side,
			Bonus: combatant.Bonus,
			Roll:  roll,
			Total: roll + combatant.Bonus,
			HP:    combatant.HP,
			AC:    combatant.AC,
		})
	}
	sort.SliceStable(entries, func(a, b int) bool {
		if entries[a].Total != entries[b].Total {
			return entries[a].Total > entries[b].Total
		}
		return entries[a].Name < entries[b].Name
	})
	return entries
}

// NextCombatant передвигает указатель на следующего живого участника.
// При замыкании круга номер раунда увеличивается; acted сбрасывается, а dodge
// снимается с того, чей ход начинается. Если живых нет — порядок без изменений.
func NextCombatant(order CombatOrder, alive func(CombatantEntry) bool) CombatOrder {
	count := len(order.Order)
	if !order.Started || count == 0 {
		return order
	}
	for step := 1; step <= count; step++ {
		index := (order.Current + step) % count
		if !alive(order.Order[index]) {
			continue
		}
		nextOrder := make([]CombatantEntry, len(order.Order))
		copy(nextOrder, order.Order)
		nextOrder[index].Dodging = false
		next := order
		next.Order = nextOrder
		next.Current = index
		next.Acted = false
		if order.Current >= 0 && index == 0 {
			next.Round = order.Round + 1
		}
		return next
	}
	return order
}

// SerializedCombatant — форма снимка боя в JSON (combat_snapshot).
type SerializedCombatant struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Side    CombatantSide `json:"side"`
	Bonus   int           `json:"bonus"`
	Roll    int           `json:"roll"`
	Total   int           `json:"total"`
	HP      *int          `json:"hp,omitempty"`
	MaxHP   *int          `json:"maxHp,omitempty"`
	AC      *int          `json:"ac,omitempty"`
	Dodging bool          `json:"dodging,omitempty"`
}

// SerializedCombatOrder — снимок порядка ходов в JSON.
type SerializedCombatOrder struct {
	Started bool                  `json:"started"`
	Round   int                   `json:"round"`
	Current int                   `json:"current"`
	Acted   bool                  `json:"acted"`
	Order   []SerializedCombatant `json:"order"`
}

// SerializeCombatant конвертирует участника в снимок (nil-поля опускаются).
func SerializeCombatant(entry CombatantEntry) SerializedCombatant {
	return SerializedCombatant{
		ID:      entry.ID,
		Name:    entry.Name,
		Side:    entry.Side,
		Bonus:   entry.Bonus,
		Roll:    entry.Roll,
		Total:   entry.Total,
		HP:      entry.HP,
		MaxHP:   entry.MaxHP,
		AC:      entry.AC,
		Dodging: entry.Dodging,
	}
}

// DeserializeCombatant строго разбирает участника из JSON-объекта; любые
// посторонние данные дают false — вызывающий фоллбэчит на пустой бой.
func DeserializeCombatant(data any) (CombatantEntry, bool) {
	object, ok := data.(map[string]any)
	if !ok {
		return CombatantEntry{}, false
	}
	id, ok := object["id"].(string)
	if !ok || id == "" {
		return CombatantEntry{}, false
	}
	name, ok := object["name"].(string)
	if !ok || name == "" {
		return CombatantEntry{}, false
	}
	side, ok := object["side"].(string)
	if !ok || (side != string(SideParty) && side != string(SideEnemy)) {
		return CombatantEntry{}, false
	}
	bonus, ok := integerValue(object["bonus"])
	if !ok {
		return CombatantEntry{}, false
	}
	roll, ok := integerValue(object["roll"])
	if !ok {
		return CombatantEntry{}, false
	}
	total, ok := integerValue(object["total"])
	if !ok {
		return CombatantEntry{}, false
	}
	entry := CombatantEntry{ID: id, Name: name, Side: CombatantSide(side), Bonus: bonus, Roll: roll, Total: total}
	if value, ok := optionalInt(object["hp"]); ok {
		entry.HP = value
	}
	if value, ok := optionalInt(object["maxHp"]); ok {
		entry.MaxHP = value
	}
	if value, ok := optionalInt(object["ac"]); ok {
		entry.AC = value
	}
	if dodging, ok := object["dodging"].(bool); ok && dodging {
		entry.Dodging = true
	}
	return entry, true
}

// DeserializeCombatOrder строго разбирает порядок ходов из JSON-объекта.
func DeserializeCombatOrder(data any) (CombatOrder, bool) {
	object, ok := data.(map[string]any)
	if !ok {
		return CombatOrder{}, false
	}
	started, ok := object["started"].(bool)
	if !ok {
		return CombatOrder{}, false
	}
	round, ok := integerValue(object["round"])
	if !ok {
		return CombatOrder{}, false
	}
	current, ok := integerValue(object["current"])
	if !ok {
		return CombatOrder{}, false
	}
	acted, ok := object["acted"].(bool)
	if !ok {
		return CombatOrder{}, false
	}
	rawOrder, ok := object["order"].([]any)
	if !ok {
		return CombatOrder{}, false
	}
	order := make([]CombatantEntry, 0, len(rawOrder))
	for _, raw := range rawOrder {
		entry, entryOK := DeserializeCombatant(raw)
		if !entryOK {
			return CombatOrder{}, false
		}
		order = append(order, entry)
	}
	return CombatOrder{Started: started, Round: round, Current: current, Acted: acted, Order: order}, true
}

// SerializeCombatOrder конвертирует порядок ходов в снимок.
func SerializeCombatOrder(order CombatOrder) SerializedCombatOrder {
	serialized := SerializedCombatOrder{
		Started: order.Started,
		Round:   order.Round,
		Current: order.Current,
		Acted:   order.Acted,
		Order:   make([]SerializedCombatant, 0, len(order.Order)),
	}
	for _, entry := range order.Order {
		serialized.Order = append(serialized.Order, SerializeCombatant(entry))
	}
	return serialized
}

func optionalInt(value any) (*int, bool) {
	if value == nil {
		return nil, false
	}
	parsed, ok := integerValue(value)
	if !ok {
		return nil, false
	}
	return &parsed, true
}

var damageDicePattern = regexp.MustCompile(`(?i)(\d{1,2})d(4|6|8|10|12)`)

// WeaponDamageDice ищет спецификацию урона в тексте инвентаря: «короткий меч
// (1d6)» → «1d6». Если weapon задан — только в предметах с этим именем.
func WeaponDamageDice(items []string, weapon string) (string, bool) {
	needle := strings.ToLower(strings.TrimSpace(weapon))
	candidates := items
	if needle != "" {
		candidates = nil
		for _, item := range items {
			if strings.Contains(strings.ToLower(item), needle) {
				candidates = append(candidates, item)
			}
		}
	}
	for _, item := range candidates {
		match := damageDicePattern.FindStringSubmatch(item)
		if match != nil {
			return match[1] + "d" + match[2], true
		}
	}
	return "", false
}
