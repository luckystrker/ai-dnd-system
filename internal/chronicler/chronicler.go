// Package chronicler — отдельный агент-летописец кампании: читает транскрипт
// дня и пишет долговременную память через ChroniclerToolkit.
package chronicler

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luckystrker/ago/agent"
	agoconfig "github.com/luckystrker/ago/config"

	"github.com/luckystriker/ai-dnd-system/internal/store"
	"github.com/luckystriker/ai-dnd-system/internal/tools"
)

//go:embed instructions.md
var instructions string

// Report — итог закрытия дня. Счётчики считаются по изменениям в БД, а не по
// словам модели.
type Report struct {
	DaySummaryWritten   bool   `json:"daySummaryWritten"`
	KeyEventsWritten    int    `json:"keyEventsWritten"`
	NpcsUpdated         int    `json:"npcsUpdated"`
	CharactersUpdated   int    `json:"charactersUpdated"`
	LocationsUpdated    int    `json:"locationsUpdated"`
	FactionsUpdated     int    `json:"factionsUpdated"`
	WorldChangesWritten int    `json:"worldChangesWritten"`
	Note                string `json:"note,omitempty"`
}

// Config — настройки летописца.
type Config struct {
	Model         agoconfig.ModelConfig
	Store         *store.Store
	CampaignID    string
	CampaignSlug  string
	MaxToolRounds int
	Timeout       time.Duration
	// ToolDeps позволяет переиспользовать зависимости тулов (Random, Dice,
	// локи); DB подставляется из Store.
	ToolDeps tools.Deps
}

// Chronicler — собранный агент с Trusted-сессией своей кампании.
type Chronicler struct {
	agent   *agent.Agent
	store   *store.Store
	session *tools.Session
	timeout time.Duration
}

// New собирает летописца. Сессия Trusted: кампания задаётся кодом, аргументов
// модели для выбора кампании нет.
func New(cfg Config) (*Chronicler, error) {
	if cfg.Model == nil {
		return nil, errors.New("chronicler: model config is required")
	}
	if cfg.Store == nil {
		return nil, errors.New("chronicler: store is required")
	}
	if cfg.CampaignID == "" && cfg.CampaignSlug == "" {
		return nil, errors.New("chronicler: campaign is required")
	}
	key := cfg.CampaignID
	if key == "" {
		key = cfg.CampaignSlug
	}
	campaign, err := cfg.Store.GetCampaign(key)
	if err != nil {
		return nil, err
	}
	if campaign == nil {
		return nil, fmt.Errorf("chronicler: campaign %q not found", key)
	}
	session := &tools.Session{
		CampaignID: campaign.ID, CampaignSlug: campaign.Slug, Trusted: true,
	}
	deps := cfg.ToolDeps
	deps.DB = cfg.Store
	toolkit, err := tools.ChroniclerToolkit(deps)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	agentOpts := []agent.Option{
		agent.WithConfig(cfg.Model),
		agent.WithTools(toolkit),
		agent.WithDependencies(map[string]any{"ttrpg.session": session}),
	}
	if cfg.MaxToolRounds > 0 {
		agentOpts = append(agentOpts, agent.WithMaxToolRounds(cfg.MaxToolRounds))
	}
	return &Chronicler{
		agent:   agent.New("chronicler", instructions, agentOpts...),
		store:   cfg.Store,
		session: session,
		timeout: timeout,
	}, nil
}

// Session возвращает Trusted-сессию летописца (для тестов/диагностики).
func (c *Chronicler) Session() *tools.Session { return c.session }

// CloseDay закрывает игровой день: просит модель пройтись по тулам и считает
// изменения памяти по БД до/после. Возвращённый Report — факт изменений, а не
// отчёт модели.
func (c *Chronicler) CloseDay(ctx context.Context, campaign *store.Campaign, day int, highlights string) (Report, error) {
	if campaign == nil {
		loaded, err := c.store.GetCampaign(c.session.CampaignID)
		if err != nil {
			return Report{}, err
		}
		if loaded == nil {
			return Report{}, errors.New("chronicler: campaign not found")
		}
		campaign = loaded
	}
	before, err := c.snapshot(campaign, day)
	if err != nil {
		return Report{}, err
	}
	prompt := fmt.Sprintf(
		"Закрой игровой день %d кампании «%s». Прочитай транскрипт дня через read_day (day=%d), "+
			"запиши саммари и headline, хронику, ключевые события, обнови NPC/персонажей/квесты/нити/"+
			"локации/фракции/состояние мира по инструкции.",
		day, campaign.Title, day,
	)
	if strings.TrimSpace(highlights) != "" {
		prompt += "\n\nВехи от DM: " + strings.TrimSpace(highlights)
	}
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	reply, err := c.agent.Ask(runCtx, prompt)
	if err != nil {
		return Report{}, err
	}
	after, err := c.snapshot(campaign, day)
	if err != nil {
		return Report{}, err
	}
	report := diffSnapshot(before, after)
	report.Note = strings.TrimSpace(reply.Body())
	return report, nil
}

type memorySnapshot struct {
	daySummary bool
	keyEvents  int
	npcs       map[string]string
	characters map[string]string
	locations  map[string]string
	factions   map[string]string
	world      map[string]string
}

func (c *Chronicler) snapshot(campaign *store.Campaign, day int) (memorySnapshot, error) {
	result := memorySnapshot{
		npcs: map[string]string{}, characters: map[string]string{},
		locations: map[string]string{}, factions: map[string]string{},
		world: map[string]string{},
	}
	dayRecord, err := c.store.ReadDay(campaign.Slug, day)
	if err != nil {
		return result, err
	}
	if dayRecord != nil && dayRecord.Summary != nil {
		summary := strings.TrimSpace(*dayRecord.Summary)
		result.daySummary = summary != "" && !strings.HasPrefix(summary, store.AutoDigestMark)
	}
	permanent, regular, err := c.store.SplitKeyEvents(campaign.Slug)
	if err != nil {
		return result, err
	}
	result.keyEvents = len(permanent) + len(regular)
	characters, err := c.store.ListCharacters(campaign.ID)
	if err != nil {
		return result, err
	}
	for _, sheet := range characters {
		result.characters[sheet.ID] = hashJSON(sheet)
	}
	npcs, err := c.store.ListNpcs(campaign.ID)
	if err != nil {
		return result, err
	}
	for _, npc := range npcs {
		card, err := c.store.GetNpc(campaign.ID, npc.ID)
		if err != nil {
			return result, err
		}
		result.npcs[npc.ID] = hashJSON(card)
	}
	locations, err := c.store.ListLocations(campaign.ID)
	if err != nil {
		return result, err
	}
	for _, location := range locations {
		result.locations[location.ID] = hashJSON(location)
	}
	factions, err := c.store.ListFactions(campaign.ID)
	if err != nil {
		return result, err
	}
	for _, faction := range factions {
		result.factions[faction.ID] = hashJSON(faction)
	}
	rendered, err := c.store.RenderWorldState(campaign.Slug)
	if err != nil {
		return result, err
	}
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result.world[line] = line
	}
	return result, nil
}

func hashJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func changed(before, after map[string]string) int {
	count := 0
	for id, value := range after {
		if before[id] != value {
			count++
		}
	}
	return count
}

func diffSnapshot(before, after memorySnapshot) Report {
	return Report{
		DaySummaryWritten:   !before.daySummary && after.daySummary,
		KeyEventsWritten:    max(0, after.keyEvents-before.keyEvents),
		NpcsUpdated:         changed(before.npcs, after.npcs),
		CharactersUpdated:   changed(before.characters, after.characters),
		LocationsUpdated:    changed(before.locations, after.locations),
		FactionsUpdated:     changed(before.factions, after.factions),
		WorldChangesWritten: changed(before.world, after.world),
	}
}
