// Package store — хранилище кампаний на SQLite (порт TS-хранилища; до
// слайса 8 его код лежал в `agent/lib/campaigns`).
//
// Одна база (data/campaigns.db, CAMPAIGN_DB_PATH) хранит кампании, участников,
// персонажей, квесты, открытые нити, журнал дней и транскрипт, ключевые
// события, журнал экономики, NPC, локации, фракции, состояние мира, снимок боя
// и FTS5-индекс журнальных таблиц. Схема 1:1 с TS-версией: Go открывает
// существующую прод-базу без миграции данных, а миграции колонок и починки
// повторяют TS-стор (сверено golden-тестом, эталон — testdata/schema_ts.sql).
package store

import "time"

// MemberRole — роль участника кампании.
type MemberRole string

const (
	RoleDM     MemberRole = "dm"
	RolePlayer MemberRole = "player"
)

// CampaignLength — длина кампании.
type CampaignLength string

const (
	LengthShort  CampaignLength = "short"
	LengthMedium CampaignLength = "medium"
	LengthLong   CampaignLength = "long"
)

// CampaignStatus — статус жизненного цикла кампании.
type CampaignStatus string

const (
	StatusSetup    CampaignStatus = "setup"
	StatusActive   CampaignStatus = "active"
	StatusFinished CampaignStatus = "finished"
)

// TimeOfDay — время суток в игре.
type TimeOfDay string

const (
	TimeMorning TimeOfDay = "morning"
	TimeDay     TimeOfDay = "day"
	TimeEvening TimeOfDay = "evening"
	TimeNight   TimeOfDay = "night"
)

// NpcStatus — статус NPC.
type NpcStatus string

const (
	NpcAlive   NpcStatus = "alive"
	NpcDead    NpcStatus = "dead"
	NpcUnknown NpcStatus = "unknown"
)

// QuestStatus — статус квеста.
type QuestStatus string

const (
	QuestOffered   QuestStatus = "offered"
	QuestAccepted  QuestStatus = "accepted"
	QuestActive    QuestStatus = "active"
	QuestCompleted QuestStatus = "completed"
	QuestFailed    QuestStatus = "failed"
	QuestAbandoned QuestStatus = "abandoned"
)

// QuestDifficulty — сложность квеста.
type QuestDifficulty string

const (
	QuestEasy   QuestDifficulty = "easy"
	QuestMedium QuestDifficulty = "medium"
	QuestHard   QuestDifficulty = "hard"
)

// ThreadKind — тип открытой нити.
type ThreadKind string

const (
	ThreadPromise    ThreadKind = "promise"
	ThreadMystery    ThreadKind = "mystery"
	ThreadDebt       ThreadKind = "debt"
	ThreadUnresolved ThreadKind = "unresolved"
)

// LedgerType — тип записи журнала экономики.
type LedgerType string

const (
	LedgerFound LedgerType = "found"
	LedgerSpent LedgerType = "spent"
)

// MaxParty — максимальный размер партии.
const MaxParty = 6

// CampaignMember — участник кампании.
type CampaignMember struct {
	UserID   string
	Name     *string
	Username *string
	Role     MemberRole
}

// BoundChat — привязка кампании к чату/топику.
type BoundChat struct {
	ChatID          string
	MessageThreadID *int
}

// Campaign — кампания.
type Campaign struct {
	ID           string
	Title        string
	Slug         string
	Status       CampaignStatus
	OwnerUserID  string
	Length       CampaignLength
	Setting      string
	Theme        string
	Goal         *string
	Tone         *string
	OpeningScene *string
	BoundChat    *BoundChat
	CurrentDay   *int
	TimeOfDay    *TimeOfDay
	InGameDate   *string
	Weather      *string
	Members      []CampaignMember
	CreatedAt    string
}

// CharacterAbility — способность/заклинание персонажа.
type CharacterAbility struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Level       *int   `json:"level,omitempty"`
}

// CharacterSheet — лист персонажа.
type CharacterSheet struct {
	ID             string
	CampaignID     string
	Name           string
	Slug           string
	OwnerUserID    string
	CharacterClass string
	Race           string
	Level          int
	Stats          map[string]float64
	Background     *string
	Motivation     *string
	Appearance     *string
	HP             *int
	MaxHP          *int
	Conditions     []string
	Inventory      []string
	Abilities      []CharacterAbility
	Gold           *int
	XP             *int
	Location       *string
	UpdatedAt      *string
	CreatedAt      string
}

// CharacterStatePatch — патч динамического состояния (nil — не менять).
type CharacterStatePatch struct {
	Level      *int
	HP         *int
	MaxHP      *int
	Conditions *[]string
	Inventory  *[]string
	Abilities  *[]CharacterAbility
	Gold       *int
	XP         *int
	Location   *string
}

// CharacterGrantPatch — аддитивный патч: значения прибавляются.
type CharacterGrantPatch struct {
	Inventory  []string
	Abilities  []CharacterAbility
	Gold       int
	XP         int
	Conditions []string
}

// QuestRewardPlan — запланированная награда (nil-поля считаются по таблицам).
type QuestRewardPlan struct {
	XP    *int     `json:"xp,omitempty"`
	Gold  *int     `json:"gold,omitempty"`
	Items []string `json:"items,omitempty"`
	Note  *string  `json:"note,omitempty"`
}

// Quest — квест.
type Quest struct {
	ID           string
	CampaignID   string
	Slug         string
	Title        string
	GiverNpcSlug *string
	Objective    string
	Difficulty   QuestDifficulty
	RewardPlan   *QuestRewardPlan
	Status       QuestStatus
	DeadlineDay  *int
	CreatedDay   int
	CreatedAt    string
	UpdatedAt    *string
}

// NewQuestInput — входные данные создания квеста.
type NewQuestInput struct {
	Title        string
	GiverNpcSlug *string
	Objective    string
	Difficulty   QuestDifficulty
	RewardPlan   *QuestRewardPlan
	Status       *QuestStatus
	DeadlineDay  *int
}

// QuestPatch — патч квеста (nil — не менять).
type QuestPatch struct {
	Title        *string
	GiverNpcSlug *string
	Objective    *string
	Difficulty   *QuestDifficulty
	RewardPlan   *QuestRewardPlan
	Status       *QuestStatus
	DeadlineDay  *int
}

// OpenThread — открытая нить.
type OpenThread struct {
	ID            string
	CampaignID    string
	Text          string
	Kind          ThreadKind
	Status        string // "open" | "resolved"
	LinkedQuestID *string
	DayOpened     int
	DayClosed     *int
	CreatedAt     string
	UpdatedAt     *string
}

// NewThreadInput — входные данные нити.
type NewThreadInput struct {
	Text          string
	Kind          *ThreadKind
	LinkedQuestID *string
}

// NpcRelationship — отношение NPC к персонажу (-5..+5).
type NpcRelationship struct {
	Attitude int
	Notes    *string
}

// NpcProfile — профиль NPC.
type NpcProfile struct {
	ID            string
	CampaignID    string
	Name          string
	Slug          string
	Role          *string
	Status        NpcStatus
	Location      *string
	Relationships map[string]NpcRelationship
	FirstSeenDay  *int
	LastSeenDay   *int
	CreatedAt     string
	UpdatedAt     *string
}

// NpcUpsertInput — входные данные создания/обновления NPC.
type NpcUpsertInput struct {
	Name            string
	Role            *string
	Status          *NpcStatus
	Location        *string
	Relationships   map[string]NpcRelationship
	FirstSeenDay    *int
	LastSeenDay     *int
	MemoryAppend    *string
	MemoryAppendDay *int
}

// NpcWithMemory — профиль вместе с памятью.
type NpcWithMemory struct {
	NpcProfile
	Memory string
}

// LocationConnection — связь между локациями.
type LocationConnection struct {
	To            string
	Via           *string
	DiscoveredDay *int
}

// Location — локация.
type Location struct {
	ID            string
	CampaignID    string
	Name          string
	Slug          string
	Description   *string
	Connections   []LocationConnection
	DiscoveredDay *int
	VisitedDays   []int
	Current       *bool
	CreatedAt     string
	UpdatedAt     *string
}

// UpsertLocationInput — входные данные локации; Name пустой — обновление по имени.
type UpsertLocationInput struct {
	Name          string
	Description   *string
	Connections   *[]LocationConnection
	DiscoveredDay *int
	Current       *bool
}

// Faction — фракция.
type Faction struct {
	ID          string
	CampaignID  string
	Name        string
	Slug        string
	Description *string
	Standing    int
	CreatedAt   string
	UpdatedAt   *string
}

// UpsertFactionInput — входные данные фракции.
type UpsertFactionInput struct {
	Name        string
	Description *string
	Standing    *int
}

// LedgerRow — запись журнала экономики.
type LedgerRow struct {
	Day        int
	Type       LedgerType
	ItemOrGold string
	By         *string
	Note       *string
}

// WorldChange — изменение мира.
type WorldChange struct {
	Category string
	Text     string
	Day      *int
}

// NewCampaignInput — входные данные кампании.
type NewCampaignInput struct {
	Title        string
	Length       CampaignLength
	Setting      string
	Theme        string
	Goal         *string
	Tone         *string
	OpeningScene *string
	Description  string
}

// NewOwnerInput — создатель кампании (становится dm).
type NewOwnerInput struct {
	UserID   string
	Name     *string
	Username *string
}

// NewMemberInput — новый участник.
type NewMemberInput struct {
	UserID   string
	Name     *string
	Username *string
	Role     *MemberRole
}

// NewCharacterInput — входные данные персонажа.
type NewCharacterInput struct {
	Name           string
	CharacterClass string
	Race           string
	Level          *int
	Stats          map[string]float64
	Background     *string
	Motivation     *string
	Appearance     *string
	Equipment      []string
	Abilities      []CharacterAbility
	Gold           *int
	MaxHP          *int
	HP             *int
}

// EnvironmentPatch — патч игрового времени/окружения.
type EnvironmentPatch struct {
	TimeOfDay  *TimeOfDay
	InGameDate *string
	Weather    *string
}

// StoreErrorCode — код ошибки хранилища.
type StoreErrorCode string

const (
	ErrNotFound     StoreErrorCode = "not_found"
	ErrAccessDenied StoreErrorCode = "access_denied"
	ErrDuplicate    StoreErrorCode = "duplicate"
	ErrConflict     StoreErrorCode = "conflict"
	ErrPartyFull    StoreErrorCode = "party_full"
)

// StoreError — ошибка хранилища с человекочитаемым сообщением для модели.
type StoreError struct {
	Message string
	Code    StoreErrorCode
}

func (e *StoreError) Error() string { return e.Message }

func storeError(message string, code StoreErrorCode) *StoreError {
	return &StoreError{Message: message, Code: code}
}

// IsStoreErrorCode проверяет код ошибки хранилища.
func IsStoreErrorCode(err error, code StoreErrorCode) bool {
	storeErr, ok := err.(*StoreError)
	return ok && storeErr.Code == code
}

// nowISO — как new Date().toISOString(): UTC с миллисекундами.
func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func todayISO() string {
	return time.Now().UTC().Format("2006-01-02")
}
