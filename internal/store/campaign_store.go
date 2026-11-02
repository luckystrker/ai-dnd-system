package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const campaignColumns = `id, slug, title, status, owner_user_id, length, setting, theme, goal, tone,
  opening_scene, description, bound_chat_id, bound_thread_id, current_day, time_of_day,
  in_game_date, weather, created_at`

const characterColumns = `id, campaign_id, slug, name, owner_user_id, class, race, level, stats,
  background, motivation, appearance, hp, max_hp, conditions, inventory, abilities, gold, xp,
  location, updated_at, created_at`

const questColumns = `id, campaign_id, slug, title, giver_npc_slug, objective, difficulty,
  reward_plan, status, deadline_day, created_day, created_at, updated_at`

const threadColumns = `id, campaign_id, text, kind, status, linked_quest_id, day_opened,
  day_closed, created_at, updated_at`

// CreateCampaign создаёт кампанию: владелец становится dm.
func (s *Store) CreateCampaign(input NewCampaignInput, owner NewOwnerInput) (*Campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, err := s.uniqueCampaignSlug(Slugify(input.Title))
	if err != nil {
		return nil, err
	}
	campaign := &Campaign{
		ID:           newUUID(),
		Title:        input.Title,
		Slug:         slug,
		Status:       StatusSetup,
		OwnerUserID:  owner.UserID,
		Length:       input.Length,
		Setting:      input.Setting,
		Theme:        input.Theme,
		Goal:         input.Goal,
		Tone:         input.Tone,
		OpeningScene: input.OpeningScene,
		Members:      []CampaignMember{{UserID: owner.UserID, Name: owner.Name, Username: owner.Username, Role: RoleDM}},
		CreatedAt:    nowISO(),
	}
	if err := s.insertCampaign(campaign, input.Description); err != nil {
		return nil, err
	}
	return campaign, nil
}

// GetCampaign находит кампанию по id или slug; nil без ошибки, если нет.
func (s *Store) GetCampaign(idOrSlug string) (*Campaign, error) {
	row := s.db.QueryRow(
		"SELECT "+campaignColumns+" FROM campaigns WHERE id = ? OR slug = ? LIMIT 1",
		idOrSlug, idOrSlug,
	)
	campaign, err := s.scanCampaign(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return campaign, nil
}

func (s *Store) mustGetCampaign(idOrSlug string) (*Campaign, error) {
	campaign, err := s.GetCampaign(idOrSlug)
	if err != nil {
		return nil, err
	}
	if campaign == nil {
		return nil, storeError(fmt.Sprintf("Кампания «%s» не найдена.", idOrSlug), ErrNotFound)
	}
	return campaign, nil
}

// ListCampaigns возвращает все кампании от новых к старым.
func (s *Store) ListCampaigns() ([]Campaign, error) {
	rows, err := s.db.Query("SELECT " + campaignColumns + " FROM campaigns ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	var campaigns []Campaign
	for rows.Next() {
		campaign, err := scanCampaignRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		campaigns = append(campaigns, *campaign)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// Соединение одно (SetMaxOpenConns(1)): участников добираем после
	// закрытия курсора, иначе вложенный запрос заблокируется.
	for index := range campaigns {
		if err := s.loadMembers(&campaigns[index]); err != nil {
			return nil, err
		}
	}
	return campaigns, nil
}

// ListForUser — кампании, где пользователь участник.
func (s *Store) ListForUser(userID string) ([]Campaign, error) {
	all, err := s.ListCampaigns()
	if err != nil {
		return nil, err
	}
	var result []Campaign
	for _, campaign := range all {
		for _, member := range campaign.Members {
			if member.UserID == userID {
				result = append(result, campaign)
				break
			}
		}
	}
	return result, nil
}

// FindByBoundChat находит активную (или любую при anyStatus) кампанию чата.
// nil-safe сравнение: кампания без топика совпадает только с запросом без топика.
func (s *Store) FindByBoundChat(chatID string, messageThreadID *int, anyStatus bool) (*Campaign, error) {
	statusClause := "AND status = 'active'"
	if anyStatus {
		statusClause = ""
	}
	row := s.db.QueryRow(
		"SELECT "+campaignColumns+" FROM campaigns WHERE bound_chat_id = ? AND bound_thread_id IS ? "+statusClause+" LIMIT 1",
		chatID, nullableInt(messageThreadID),
	)
	campaign, err := s.scanCampaign(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return campaign, nil
}

// BindAndActivate запускает кампанию и привязывает её к чату.
func (s *Store) BindAndActivate(campaignID, actorUserID string, chat BoundChat) (*Campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignID)
	if err != nil {
		return nil, err
	}
	if err := requireRole(campaign, actorUserID, RoleDM); err != nil {
		return nil, err
	}
	if campaign.Status == StatusActive {
		return nil, storeError(fmt.Sprintf("Кампания «%s» уже запущена.", campaign.Title), ErrConflict)
	}
	existing, err := s.FindByBoundChat(chat.ChatID, chat.MessageThreadID, false)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.ID != campaign.ID {
		return nil, storeError(
			fmt.Sprintf("В этом чате уже идёт кампания «%s». Один чат/топик — одна активная кампания.", existing.Title),
			ErrConflict,
		)
	}
	_, err = s.db.Exec(
		`UPDATE campaigns
         SET status = 'active', bound_chat_id = ?, bound_thread_id = ?, current_day = COALESCE(current_day, 1)
         WHERE id = ?`,
		chat.ChatID, nullableInt(chat.MessageThreadID), campaign.ID,
	)
	if err != nil {
		return nil, err
	}
	return s.mustGetCampaign(campaign.ID)
}

// AdvanceDay увеличивает игровой день и начинает его утром (dm-вариант для
// административных сценариев и обратной совместимости).
func (s *Store) AdvanceDay(campaignID, actorUserID string) (*Campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignID)
	if err != nil {
		return nil, err
	}
	if err := requireRole(campaign, actorUserID, RoleDM); err != nil {
		return nil, err
	}
	updated, _, err := s.advanceDayLocked(campaign)
	return updated, err
}

// AdvanceDayForMember двигает день без проверки роли dm: по новой
// авторизации день — операция памяти кампании, её может делать любой участник;
// роль dm осталась только у административных команд. Возвращает и номер
// закрытого дня — previous читается под тем же мьютексом, что и запись.
func (s *Store) AdvanceDayForMember(campaignID string) (*Campaign, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignID)
	if err != nil {
		return nil, 0, err
	}
	return s.advanceDayLocked(campaign)
}

func (s *Store) advanceDayLocked(campaign *Campaign) (*Campaign, int, error) {
	if campaign.Status != StatusActive {
		return nil, 0, storeError("Игровые дни можно двигать только в активной кампании.", ErrConflict)
	}
	previous := 1
	if campaign.CurrentDay != nil {
		previous = *campaign.CurrentDay
	}
	if _, err := s.db.Exec(
		"UPDATE campaigns SET current_day = COALESCE(current_day, 1) + 1, time_of_day = 'morning' WHERE id = ?",
		campaign.ID,
	); err != nil {
		return nil, 0, err
	}
	updated, err := s.mustGetCampaign(campaign.ID)
	if err != nil {
		return nil, 0, err
	}
	return updated, previous, nil
}

// SetEnvironment обновляет игровое время/окружение.
func (s *Store) SetEnvironment(campaignID string, patch EnvironmentPatch) (*Campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignID)
	if err != nil {
		return nil, err
	}
	var sets []string
	var values []any
	if patch.TimeOfDay != nil {
		sets = append(sets, "time_of_day = ?")
		values = append(values, string(*patch.TimeOfDay))
	}
	if patch.InGameDate != nil {
		sets = append(sets, "in_game_date = ?")
		values = append(values, nullableString(*patch.InGameDate))
	}
	if patch.Weather != nil {
		sets = append(sets, "weather = ?")
		values = append(values, nullableString(*patch.Weather))
	}
	if len(sets) > 0 {
		values = append(values, campaign.ID)
		if _, err := s.db.Exec("UPDATE campaigns SET "+strings.Join(sets, ", ")+" WHERE id = ?", values...); err != nil {
			return nil, err
		}
	}
	return s.mustGetCampaign(campaign.ID)
}

// AddMember добавляет участника (только dm; роль dm — только владелец).
func (s *Store) AddMember(campaignID, inviterUserID string, member NewMemberInput) (*Campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignID)
	if err != nil {
		return nil, err
	}
	if err := requireRole(campaign, inviterUserID, RoleDM); err != nil {
		return nil, err
	}
	role := RolePlayer
	if member.Role != nil {
		role = *member.Role
	}
	if role == RoleDM && inviterUserID != campaign.OwnerUserID {
		return nil, storeError("Назначать администраторов (dm) может только владелец кампании.", ErrAccessDenied)
	}
	for _, existing := range campaign.Members {
		if existing.UserID == member.UserID {
			return nil, storeError("Этот пользователь уже участник кампании.", ErrDuplicate)
		}
	}
	if err := s.insertMember(campaign.ID, CampaignMember{
		UserID: member.UserID, Name: member.Name, Username: member.Username, Role: role,
	}); err != nil {
		return nil, err
	}
	return s.mustGetCampaign(campaign.ID)
}

// AutoRegister идемпотентно вступает в кампанию как player (/join);
// при полной партии новый участник молча пропускается.
func (s *Store) AutoRegister(campaignID string, user NewMemberInput) (*Campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignID)
	if err != nil {
		return nil, err
	}
	for _, existing := range campaign.Members {
		if existing.UserID == user.UserID {
			return campaign, nil
		}
	}
	players := 0
	for _, member := range campaign.Members {
		if member.Role == RolePlayer {
			players++
		}
	}
	if players >= MaxParty {
		return campaign, nil
	}
	if err := s.insertMember(campaign.ID, CampaignMember{
		UserID: user.UserID, Name: user.Name, Username: user.Username, Role: RolePlayer,
	}); err != nil {
		return nil, err
	}
	return s.mustGetCampaign(campaign.ID)
}

// SaveCharacter создаёт лист персонажа.
func (s *Store) SaveCharacter(campaignID, actorUserID string, input NewCharacterInput) (*CharacterSheet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignID)
	if err != nil {
		return nil, err
	}
	member := false
	for _, entry := range campaign.Members {
		if entry.UserID == actorUserID {
			member = true
			break
		}
	}
	if !member {
		return nil, storeError(
			"Персонажа может создать только участник кампании. Сначала нужно вступить в кампанию (команда /join или приглашение от DM).",
			ErrAccessDenied,
		)
	}
	characters, err := s.ListCharacters(campaign.ID)
	if err != nil {
		return nil, err
	}
	if len(characters) >= MaxParty {
		return nil, storeError(fmt.Sprintf("Партия заполнена (максимум %d персонажей).", MaxParty), ErrPartyFull)
	}
	for _, sheet := range characters {
		if strings.ToLower(sheet.Name) == strings.ToLower(input.Name) {
			return nil, storeError(fmt.Sprintf("Персонаж с именем %s уже есть в кампании.", input.Name), ErrDuplicate)
		}
	}
	slug, err := s.uniqueCharacterSlug(campaign.ID, Slugify(input.Name))
	if err != nil {
		return nil, err
	}
	level := 1
	if input.Level != nil {
		level = *input.Level
	}
	stats := input.Stats
	if stats == nil {
		stats = map[string]float64{}
	}
	var hp *int
	if input.HP != nil {
		hp = input.HP
	} else {
		hp = input.MaxHP
	}
	sheet := &CharacterSheet{
		ID:             newUUID(),
		CampaignID:     campaign.ID,
		Name:           input.Name,
		Slug:           slug,
		OwnerUserID:    actorUserID,
		CharacterClass: input.CharacterClass,
		Race:           input.Race,
		Level:          level,
		Stats:          stats,
		Background:     input.Background,
		Motivation:     input.Motivation,
		Appearance:     input.Appearance,
		MaxHP:          input.MaxHP,
		HP:             hp,
		Gold:           input.Gold,
		CreatedAt:      nowISO(),
	}
	if len(input.Equipment) > 0 {
		sheet.Inventory = input.Equipment
	}
	if len(input.Abilities) > 0 {
		sheet.Abilities = input.Abilities
	}
	if err := s.insertCharacter(sheet); err != nil {
		return nil, err
	}
	return sheet, nil
}

// UpdateCharacter обновляет динамическое состояние листа.
func (s *Store) UpdateCharacter(campaignIDOrSlug, nameOrSlug string, patch CharacterStatePatch) (*CharacterSheet, error) {
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
	level := row.Level
	if patch.Level != nil {
		level = *patch.Level
	}
	hp := nullInt(row.HP)
	if patch.HP != nil {
		hp = patch.HP
	}
	maxHP := nullInt(row.MaxHP)
	if patch.MaxHP != nil {
		maxHP = patch.MaxHP
	}
	var conditions any
	if row.Conditions.Valid {
		conditions = row.Conditions.String
	}
	if patch.Conditions != nil {
		conditions = marshalJSON(*patch.Conditions)
	}
	var inventory any
	if row.Inventory.Valid {
		inventory = row.Inventory.String
	}
	if patch.Inventory != nil {
		inventory = marshalJSON(*patch.Inventory)
	}
	var abilities any
	if row.Abilities.Valid {
		abilities = row.Abilities.String
	}
	if patch.Abilities != nil {
		abilities = marshalJSON(*patch.Abilities)
	}
	gold := nullInt(row.Gold)
	if patch.Gold != nil {
		gold = patch.Gold
	}
	xp := nullInt(row.XP)
	if patch.XP != nil {
		xp = patch.XP
	}
	location := nullString(row.Location)
	if patch.Location != nil {
		location = patch.Location
	}
	updatedAt := nowISO()
	_, err = s.db.Exec(
		`UPDATE characters SET
           level = ?, hp = ?, max_hp = ?, conditions = ?, inventory = ?,
           abilities = ?, gold = ?, xp = ?, location = ?, updated_at = ?
         WHERE id = ?`,
		level, nullableInt(hp), nullableInt(maxHP), conditions,
		inventory, abilities, nullableInt(gold),
		nullableInt(xp), nullableStringPtr(location), updatedAt, row.ID,
	)
	if err != nil {
		return nil, err
	}
	return s.mustGetCharacter(row.ID)
}

// GrantCharacter аддитивно выдаёт предметы/способности/золото/XP.
func (s *Store) GrantCharacter(campaignIDOrSlug, nameOrSlug string, patch CharacterGrantPatch) (*CharacterSheet, error) {
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
	_, err = s.db.Exec(
		`UPDATE characters SET
           inventory = ?, abilities = ?, conditions = ?, gold = ?, xp = ?, updated_at = ?
         WHERE id = ?`,
		marshalJSON(inventory), marshalJSON(abilities), marshalJSON(conditions),
		gold, xp, nowISO(), row.ID,
	)
	if err != nil {
		return nil, err
	}
	return s.mustGetCharacter(row.ID)
}

// FinishCampaign завершает кампанию и освобождает чат.
func (s *Store) FinishCampaign(campaignID, actorUserID string) (*Campaign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignID)
	if err != nil {
		return nil, err
	}
	if err := requireRole(campaign, actorUserID, RoleDM); err != nil {
		return nil, err
	}
	if campaign.Status != StatusActive && campaign.Status != StatusSetup {
		return nil, storeError(fmt.Sprintf("Кампания «%s» уже завершена.", campaign.Title), ErrConflict)
	}
	if _, err := s.db.Exec(
		"UPDATE campaigns SET status = 'finished', bound_chat_id = NULL, bound_thread_id = NULL WHERE id = ?",
		campaign.ID,
	); err != nil {
		return nil, err
	}
	return s.mustGetCampaign(campaign.ID)
}

// CreateQuest создаёт квест.
func (s *Store) CreateQuest(campaignIDOrSlug string, input NewQuestInput) (*Quest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	quests, err := s.ListQuests(campaign.ID)
	if err != nil {
		return nil, err
	}
	for _, quest := range quests {
		if strings.ToLower(quest.Title) == strings.ToLower(input.Title) {
			return nil, storeError(fmt.Sprintf("Квест «%s» уже есть в кампании.", input.Title), ErrDuplicate)
		}
	}
	slug, err := s.uniqueQuestSlug(campaign.ID, Slugify(input.Title))
	if err != nil {
		return nil, err
	}
	status := QuestOffered
	if input.Status != nil {
		status = *input.Status
	}
	createdDay := 1
	if campaign.CurrentDay != nil {
		createdDay = *campaign.CurrentDay
	}
	quest := &Quest{
		ID:           newUUID(),
		CampaignID:   campaign.ID,
		Slug:         slug,
		Title:        input.Title,
		GiverNpcSlug: input.GiverNpcSlug,
		Objective:    input.Objective,
		Difficulty:   input.Difficulty,
		RewardPlan:   input.RewardPlan,
		Status:       status,
		DeadlineDay:  input.DeadlineDay,
		CreatedDay:   createdDay,
		CreatedAt:    nowISO(),
	}
	if err := s.insertQuest(quest); err != nil {
		return nil, err
	}
	return quest, nil
}

// UpdateQuest обновляет квест (completed — только через CompleteQuest).
func (s *Store) UpdateQuest(campaignIDOrSlug, questIDOrSlug string, patch QuestPatch) (*Quest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	row, err := s.findQuestRow(campaign.ID, questIDOrSlug)
	if err != nil {
		return nil, err
	}
	quest := questFromRow(*row)
	if patch.Status != nil && *patch.Status == QuestCompleted {
		return nil, storeError(
			fmt.Sprintf("Квест «%s» нельзя перевести в completed через update_quest — используй complete_quest: он выдаст награды.", quest.Title),
			ErrConflict,
		)
	}
	if patch.Title != nil && strings.ToLower(*patch.Title) != strings.ToLower(quest.Title) {
		quests, err := s.ListQuests(campaign.ID)
		if err != nil {
			return nil, err
		}
		for _, other := range quests {
			if other.ID != quest.ID && strings.ToLower(other.Title) == strings.ToLower(*patch.Title) {
				return nil, storeError(fmt.Sprintf("Квест с названием «%s» уже есть в кампании.", *patch.Title), ErrDuplicate)
			}
		}
	}
	updated := *quest
	if patch.Title != nil {
		updated.Title = *patch.Title
	}
	if patch.GiverNpcSlug != nil {
		updated.GiverNpcSlug = patch.GiverNpcSlug
	}
	if patch.Objective != nil {
		updated.Objective = *patch.Objective
	}
	if patch.Difficulty != nil {
		updated.Difficulty = *patch.Difficulty
	}
	if patch.RewardPlan != nil {
		updated.RewardPlan = patch.RewardPlan
	}
	if patch.Status != nil {
		updated.Status = *patch.Status
	}
	if patch.DeadlineDay != nil {
		updated.DeadlineDay = patch.DeadlineDay
	}
	updated.UpdatedAt = ptr(nowISO())
	var rewardPlan any
	if row.RewardPlan.Valid {
		rewardPlan = row.RewardPlan.String
	}
	if patch.RewardPlan != nil {
		rewardPlan = marshalJSON(updated.RewardPlan)
	}
	_, err = s.db.Exec(
		`UPDATE quests SET
           title = ?, giver_npc_slug = ?, objective = ?, difficulty = ?,
           reward_plan = ?, status = ?, deadline_day = ?, updated_at = ?
         WHERE id = ?`,
		updated.Title, nullableStringPtr(updated.GiverNpcSlug), updated.Objective,
		string(updated.Difficulty), rewardPlan, string(updated.Status),
		nullableInt(updated.DeadlineDay), stringValue(updated.UpdatedAt), row.ID,
	)
	if err != nil {
		return nil, err
	}
	return s.mustGetQuest(row.ID)
}

// SetQuestStatus меняет статус квеста без ограничения update_quest: этим
// пользуется complete_quest, который сам выдаёт награды перед закрытием.
func (s *Store) SetQuestStatus(campaignIDOrSlug, questIDOrSlug string, status QuestStatus) (*Quest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	row, err := s.findQuestRow(campaign.ID, questIDOrSlug)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(
		"UPDATE quests SET status = ?, updated_at = ? WHERE id = ?",
		string(status), nowISO(), row.ID,
	); err != nil {
		return nil, err
	}
	return s.mustGetQuest(row.ID)
}

// ListQuests возвращает квесты кампании от новых к старым.
func (s *Store) ListQuests(campaignID string) ([]Quest, error) {
	rows, err := s.db.Query(
		"SELECT "+questColumns+" FROM quests WHERE campaign_id = ? ORDER BY created_at DESC",
		campaignID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var quests []Quest
	for rows.Next() {
		quest, err := scanQuest(rows)
		if err != nil {
			return nil, err
		}
		quests = append(quests, *quest)
	}
	return quests, rows.Err()
}

// AppendThread добавляет открытую нить.
func (s *Store) AppendThread(campaignIDOrSlug string, input NewThreadInput) (*OpenThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	kind := ThreadUnresolved
	if input.Kind != nil {
		kind = *input.Kind
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	thread := &OpenThread{
		ID:            newUUID(),
		CampaignID:    campaign.ID,
		Text:          jsTrim(input.Text),
		Kind:          kind,
		Status:        "open",
		LinkedQuestID: input.LinkedQuestID,
		DayOpened:     day,
		CreatedAt:     nowISO(),
	}
	if _, err := s.db.Exec(
		`INSERT INTO open_threads (
           id, campaign_id, text, kind, status, linked_quest_id, day_opened, created_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		thread.ID, thread.CampaignID, thread.Text, string(thread.Kind), thread.Status,
		nullableStringPtr(thread.LinkedQuestID), thread.DayOpened, thread.CreatedAt,
	); err != nil {
		return nil, err
	}
	return thread, nil
}

// ResolveThread закрывает нить по id или фрагменту текста.
func (s *Store) ResolveThread(campaignIDOrSlug, threadIDOrText string, day *int) (*OpenThread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	campaign, err := s.mustGetCampaign(campaignIDOrSlug)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		"SELECT "+threadColumns+" FROM open_threads WHERE campaign_id = ? AND status = 'open'",
		campaign.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(threadIDOrText)
	var matches []*OpenThread
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		if thread.ID == threadIDOrText || strings.Contains(strings.ToLower(thread.Text), needle) {
			matches = append(matches, thread)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(matches) == 0 {
		return nil, storeError(fmt.Sprintf("Открытая нить «%s» не найдена.", threadIDOrText), ErrNotFound)
	}
	if len(matches) > 1 {
		texts := make([]string, 0, len(matches))
		for _, thread := range matches {
			texts = append(texts, thread.Text)
		}
		return nil, storeError(
			fmt.Sprintf("Фрагмент «%s» подходит к нескольким нитям: %s. Уточни текст или передай id.",
				threadIDOrText, strings.Join(texts, "; ")),
			ErrConflict,
		)
	}
	closedDay := 1
	if day != nil {
		closedDay = *day
	} else if campaign.CurrentDay != nil {
		closedDay = *campaign.CurrentDay
	}
	if _, err := s.db.Exec(
		"UPDATE open_threads SET status = 'resolved', day_closed = ?, updated_at = ? WHERE id = ?",
		closedDay, nowISO(), matches[0].ID,
	); err != nil {
		return nil, err
	}
	return s.mustGetThread(matches[0].ID)
}

// ListThreads возвращает все нити кампании по времени создания.
func (s *Store) ListThreads(campaignID string) ([]OpenThread, error) {
	rows, err := s.db.Query(
		"SELECT "+threadColumns+" FROM open_threads WHERE campaign_id = ? ORDER BY created_at",
		campaignID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var threads []OpenThread
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, *thread)
	}
	return threads, rows.Err()
}

// ListCharacters возвращает листы персонажей кампании по времени создания.
func (s *Store) ListCharacters(campaignID string) ([]CharacterSheet, error) {
	rows, err := s.db.Query(
		"SELECT "+characterColumns+" FROM characters WHERE campaign_id = ? ORDER BY created_at",
		campaignID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sheets []CharacterSheet
	for rows.Next() {
		sheet, err := scanCharacter(rows)
		if err != nil {
			return nil, err
		}
		sheets = append(sheets, *sheet)
	}
	return sheets, rows.Err()
}

// ReadDescription читает описание кампании (колонка description).
func (s *Store) ReadDescription(campaignIDOrSlug string) (string, error) {
	var description sql.NullString
	err := s.db.QueryRow(
		"SELECT description FROM campaigns WHERE id = ? OR slug = ? LIMIT 1",
		campaignIDOrSlug, campaignIDOrSlug,
	).Scan(&description)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return description.String, nil
}

// --- внутренние помощники ---

func requireRole(campaign *Campaign, userID string, role MemberRole) error {
	for _, member := range campaign.Members {
		if member.UserID == userID && member.Role == role {
			return nil
		}
	}
	return storeError("Это действие доступно только администратору (dm) кампании.", ErrAccessDenied)
}

func (s *Store) scanCampaign(scanner interface{ Scan(...any) error }) (*Campaign, error) {
	campaign, err := scanCampaignRow(scanner)
	if err != nil {
		return nil, err
	}
	if err := s.loadMembers(campaign); err != nil {
		return nil, err
	}
	return campaign, nil
}

func scanCampaignRow(scanner interface{ Scan(...any) error }) (*Campaign, error) {
	var (
		campaign      Campaign
		status        string
		length        string
		goal          sql.NullString
		tone          sql.NullString
		opening       sql.NullString
		description   sql.NullString
		boundChatID   sql.NullString
		boundThreadID sql.NullInt64
		currentDay    sql.NullInt64
		timeOfDay     sql.NullString
		inGameDate    sql.NullString
		weather       sql.NullString
	)
	if err := scanner.Scan(
		&campaign.ID, &campaign.Slug, &campaign.Title, &status, &campaign.OwnerUserID,
		&length, &campaign.Setting, &campaign.Theme, &goal, &tone, &opening,
		&description, &boundChatID, &boundThreadID, &currentDay, &timeOfDay,
		&inGameDate, &weather, &campaign.CreatedAt,
	); err != nil {
		return nil, err
	}
	campaign.Status = CampaignStatus(status)
	if campaign.Status == "" {
		campaign.Status = StatusSetup
	}
	campaign.Length = CampaignLength(length)
	if campaign.Length == "" {
		campaign.Length = LengthMedium
	}
	campaign.Goal = nullString(goal)
	campaign.Tone = nullString(tone)
	campaign.OpeningScene = nullString(opening)
	if boundChatID.Valid {
		campaign.BoundChat = &BoundChat{ChatID: boundChatID.String, MessageThreadID: nullInt(boundThreadID)}
	}
	campaign.CurrentDay = nullInt(currentDay)
	if timeOfDay.Valid {
		value := TimeOfDay(timeOfDay.String)
		if value != TimeMorning && value != TimeDay && value != TimeEvening && value != TimeNight {
			value = ""
		}
		if value != "" {
			campaign.TimeOfDay = &value
		}
	}
	campaign.InGameDate = nullString(inGameDate)
	campaign.Weather = nullString(weather)
	return &campaign, nil
}

func (s *Store) loadMembers(campaign *Campaign) error {
	rows, err := s.db.Query(
		"SELECT user_id, name, username, role FROM campaign_members WHERE campaign_id = ? ORDER BY rowid",
		campaign.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			member   CampaignMember
			name     sql.NullString
			username sql.NullString
			role     string
		)
		if err := rows.Scan(&member.UserID, &name, &username, &role); err != nil {
			return err
		}
		member.Name = nullString(name)
		member.Username = nullString(username)
		if role == "dm" {
			member.Role = RoleDM
		} else {
			member.Role = RolePlayer
		}
		campaign.Members = append(campaign.Members, member)
	}
	return rows.Err()
}

type characterRow struct {
	ID         string
	CampaignID string
	Slug       string
	Name       string
	OwnerID    string
	Class      string
	Race       string
	Level      int
	Stats      string
	Background sql.NullString
	Motivation sql.NullString
	Appearance sql.NullString
	HP         sql.NullInt64
	MaxHP      sql.NullInt64
	Conditions sql.NullString
	Inventory  sql.NullString
	Abilities  sql.NullString
	Gold       sql.NullInt64
	XP         sql.NullInt64
	Location   sql.NullString
	UpdatedAt  sql.NullString
	CreatedAt  string
}

func scanCharacter(scanner interface{ Scan(...any) error }) (*CharacterSheet, error) {
	var row characterRow
	if err := scanner.Scan(
		&row.ID, &row.CampaignID, &row.Slug, &row.Name, &row.OwnerID, &row.Class,
		&row.Race, &row.Level, &row.Stats, &row.Background, &row.Motivation,
		&row.Appearance, &row.HP, &row.MaxHP, &row.Conditions, &row.Inventory,
		&row.Abilities, &row.Gold, &row.XP, &row.Location, &row.UpdatedAt, &row.CreatedAt,
	); err != nil {
		return nil, err
	}
	return sheetFromCharacterRow(row), nil
}

func sheetFromCharacterRow(row characterRow) *CharacterSheet {
	return &CharacterSheet{
		ID:             row.ID,
		CampaignID:     row.CampaignID,
		Name:           row.Name,
		Slug:           row.Slug,
		OwnerUserID:    row.OwnerID,
		CharacterClass: row.Class,
		Race:           row.Race,
		Level:          row.Level,
		Stats:          parseJSONRecord(row.Stats),
		Background:     nullString(row.Background),
		Motivation:     nullString(row.Motivation),
		Appearance:     nullString(row.Appearance),
		HP:             nullInt(row.HP),
		MaxHP:          nullInt(row.MaxHP),
		Conditions:     parseStringArray(row.Conditions),
		Inventory:      parseStringArray(row.Inventory),
		Abilities:      parseAbilityArray(row.Abilities),
		Gold:           nullInt(row.Gold),
		XP:             nullInt(row.XP),
		Location:       nullString(row.Location),
		UpdatedAt:      nullString(row.UpdatedAt),
		CreatedAt:      row.CreatedAt,
	}
}

func (s *Store) findCharacterRow(campaignID, nameOrSlug string) (*characterRow, error) {
	rows, err := s.db.Query(
		"SELECT "+characterColumns+" FROM characters WHERE campaign_id = ? ORDER BY created_at",
		campaignID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(nameOrSlug)
	for rows.Next() {
		var row characterRow
		if err := rows.Scan(
			&row.ID, &row.CampaignID, &row.Slug, &row.Name, &row.OwnerID, &row.Class,
			&row.Race, &row.Level, &row.Stats, &row.Background, &row.Motivation,
			&row.Appearance, &row.HP, &row.MaxHP, &row.Conditions, &row.Inventory,
			&row.Abilities, &row.Gold, &row.XP, &row.Location, &row.UpdatedAt, &row.CreatedAt,
		); err != nil {
			return nil, err
		}
		if row.ID == nameOrSlug || strings.ToLower(row.Slug) == needle || strings.ToLower(row.Name) == needle {
			return &row, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, storeError(fmt.Sprintf("Персонаж «%s» не найден в кампании.", nameOrSlug), ErrNotFound)
}

func (s *Store) mustGetCharacter(characterID string) (*CharacterSheet, error) {
	row, err := s.getCharacterRow(characterID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, storeError(fmt.Sprintf("Персонаж «%s» не найден в кампании.", characterID), ErrNotFound)
	}
	return sheetFromCharacterRow(*row), nil
}

func (s *Store) getCharacterRow(characterID string) (*characterRow, error) {
	rows, err := s.db.Query(
		"SELECT "+characterColumns+" FROM characters WHERE id = ?",
		characterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var row characterRow
	if err := rows.Scan(
		&row.ID, &row.CampaignID, &row.Slug, &row.Name, &row.OwnerID, &row.Class,
		&row.Race, &row.Level, &row.Stats, &row.Background, &row.Motivation,
		&row.Appearance, &row.HP, &row.MaxHP, &row.Conditions, &row.Inventory,
		&row.Abilities, &row.Gold, &row.XP, &row.Location, &row.UpdatedAt, &row.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &row, nil
}

type questRow struct {
	ID           string
	CampaignID   string
	Slug         string
	Title        string
	GiverNpcSlug sql.NullString
	Objective    string
	Difficulty   string
	RewardPlan   sql.NullString
	Status       string
	DeadlineDay  sql.NullInt64
	CreatedDay   int
	CreatedAt    string
	UpdatedAt    sql.NullString
}

func scanQuest(scanner interface{ Scan(...any) error }) (*Quest, error) {
	var row questRow
	if err := scanner.Scan(
		&row.ID, &row.CampaignID, &row.Slug, &row.Title, &row.GiverNpcSlug,
		&row.Objective, &row.Difficulty, &row.RewardPlan, &row.Status,
		&row.DeadlineDay, &row.CreatedDay, &row.CreatedAt, &row.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return questFromRow(row), nil
}

func questFromRow(row questRow) *Quest {
	return &Quest{
		ID:           row.ID,
		CampaignID:   row.CampaignID,
		Slug:         row.Slug,
		Title:        row.Title,
		GiverNpcSlug: nullString(row.GiverNpcSlug),
		Objective:    row.Objective,
		Difficulty:   parseQuestDifficulty(row.Difficulty),
		RewardPlan:   parseRewardPlan(row.RewardPlan),
		Status:       parseQuestStatus(row.Status),
		DeadlineDay:  nullInt(row.DeadlineDay),
		CreatedDay:   row.CreatedDay,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    nullString(row.UpdatedAt),
	}
}

func (s *Store) findQuestRow(campaignID, questIDOrSlug string) (*questRow, error) {
	rows, err := s.db.Query(
		"SELECT "+questColumns+" FROM quests WHERE campaign_id = ?",
		campaignID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	needle := strings.ToLower(questIDOrSlug)
	for rows.Next() {
		var row questRow
		if err := rows.Scan(
			&row.ID, &row.CampaignID, &row.Slug, &row.Title, &row.GiverNpcSlug,
			&row.Objective, &row.Difficulty, &row.RewardPlan, &row.Status,
			&row.DeadlineDay, &row.CreatedDay, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if row.ID == questIDOrSlug || strings.ToLower(row.Slug) == needle || strings.ToLower(row.Title) == needle {
			return &row, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, storeError(fmt.Sprintf("Квест «%s» не найден в кампании.", questIDOrSlug), ErrNotFound)
}

func (s *Store) mustGetQuest(questID string) (*Quest, error) {
	var row questRow
	err := s.db.QueryRow("SELECT "+questColumns+" FROM quests WHERE id = ?", questID).Scan(
		&row.ID, &row.CampaignID, &row.Slug, &row.Title, &row.GiverNpcSlug,
		&row.Objective, &row.Difficulty, &row.RewardPlan, &row.Status,
		&row.DeadlineDay, &row.CreatedDay, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, storeError(fmt.Sprintf("Квест не найден (id «%s»).", questID), ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return questFromRow(row), nil
}

func scanThread(scanner interface{ Scan(...any) error }) (*OpenThread, error) {
	var (
		thread   OpenThread
		kind     string
		status   string
		linked   sql.NullString
		dayClose sql.NullInt64
		updated  sql.NullString
	)
	if err := scanner.Scan(
		&thread.ID, &thread.CampaignID, &thread.Text, &kind, &status, &linked,
		&thread.DayOpened, &dayClose, &thread.CreatedAt, &updated,
	); err != nil {
		return nil, err
	}
	thread.Kind = parseThreadKind(kind)
	if status == "resolved" {
		thread.Status = "resolved"
	} else {
		thread.Status = "open"
	}
	thread.LinkedQuestID = nullString(linked)
	thread.DayClosed = nullInt(dayClose)
	thread.UpdatedAt = nullString(updated)
	return &thread, nil
}

func (s *Store) mustGetThread(threadID string) (*OpenThread, error) {
	rows, err := s.db.Query("SELECT "+threadColumns+" FROM open_threads WHERE id = ?", threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, storeError(fmt.Sprintf("Нить не найдена (id «%s»).", threadID), ErrNotFound)
	}
	return scanThread(rows)
}

func (s *Store) insertCampaign(campaign *Campaign, description string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(
		`INSERT INTO campaigns (
           id, slug, title, status, owner_user_id, length, setting, theme, goal, tone,
           opening_scene, description, bound_chat_id, bound_thread_id, current_day,
           time_of_day, in_game_date, weather, created_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		campaign.ID, campaign.Slug, campaign.Title, string(campaign.Status),
		campaign.OwnerUserID, string(campaign.Length), campaign.Setting, campaign.Theme,
		nullableStringPtr(campaign.Goal), nullableStringPtr(campaign.Tone),
		nullableStringPtr(campaign.OpeningScene), description,
		nullableBoundChatID(campaign.BoundChat), nullableBoundThreadID(campaign.BoundChat),
		nullableInt(campaign.CurrentDay), nullableTimeOfDay(campaign.TimeOfDay),
		nullableStringPtr(campaign.InGameDate), nullableStringPtr(campaign.Weather),
		campaign.CreatedAt,
	)
	if err != nil {
		return err
	}
	for _, member := range campaign.Members {
		if err := insertMemberTx(tx, campaign.ID, member); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) insertMember(campaignID string, member CampaignMember) error {
	_, err := s.db.Exec(
		"INSERT INTO campaign_members (campaign_id, user_id, name, username, role) VALUES (?, ?, ?, ?, ?)",
		campaignID, member.UserID, nullableStringPtr(member.Name), nullableStringPtr(member.Username), string(member.Role),
	)
	return err
}

func insertMemberTx(tx *sql.Tx, campaignID string, member CampaignMember) error {
	_, err := tx.Exec(
		"INSERT INTO campaign_members (campaign_id, user_id, name, username, role) VALUES (?, ?, ?, ?, ?)",
		campaignID, member.UserID, nullableStringPtr(member.Name), nullableStringPtr(member.Username), string(member.Role),
	)
	return err
}

func (s *Store) insertCharacter(sheet *CharacterSheet) error {
	_, err := s.db.Exec(
		`INSERT INTO characters (
           id, campaign_id, slug, name, owner_user_id, class, race, level, stats, background,
           motivation, appearance, hp, max_hp, conditions, inventory, abilities, gold, xp, location,
           updated_at, created_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sheet.ID, sheet.CampaignID, sheet.Slug, sheet.Name, sheet.OwnerUserID,
		sheet.CharacterClass, sheet.Race, sheet.Level, marshalJSON(sheet.Stats),
		nullableStringPtr(sheet.Background), nullableStringPtr(sheet.Motivation),
		nullableStringPtr(sheet.Appearance), nullableInt(sheet.HP), nullableInt(sheet.MaxHP),
		nullableConditions(sheet.Conditions), nullableStringSlice(sheet.Inventory),
		nullableAbilitySlice(sheet.Abilities), nullableInt(sheet.Gold), nullableInt(sheet.XP),
		nullableStringPtr(sheet.Location), nullableStringPtr(sheet.UpdatedAt), sheet.CreatedAt,
	)
	return err
}

func (s *Store) insertQuest(quest *Quest) error {
	rewardPlan := any(nil)
	if quest.RewardPlan != nil {
		rewardPlan = marshalJSON(quest.RewardPlan)
	}
	_, err := s.db.Exec(
		`INSERT INTO quests (
           id, campaign_id, slug, title, giver_npc_slug, objective, difficulty,
           reward_plan, status, deadline_day, created_day, created_at
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		quest.ID, quest.CampaignID, quest.Slug, quest.Title,
		nullableStringPtr(quest.GiverNpcSlug), quest.Objective, string(quest.Difficulty),
		rewardPlan, string(quest.Status), nullableInt(quest.DeadlineDay),
		quest.CreatedDay, quest.CreatedAt,
	)
	return err
}

func (s *Store) uniqueCampaignSlug(base string) (string, error) {
	return s.uniqueSlug("campaigns", "", "slug", base)
}

func (s *Store) uniqueCharacterSlug(campaignID, base string) (string, error) {
	return s.uniqueSlug("characters", campaignID, "slug", base)
}

func (s *Store) uniqueQuestSlug(campaignID, base string) (string, error) {
	return s.uniqueSlug("quests", campaignID, "slug", base)
}

// uniqueSlug повторяет цикл TS: base, base-2, base-3, ... (для квестов base
// не меняется, поэтому суффикс всегда от исходного slug).
func (s *Store) uniqueSlug(table, campaignID, column, base string) (string, error) {
	slug := base
	counter := 2
	for {
		var exists int
		var err error
		if campaignID == "" {
			err = s.db.QueryRow("SELECT 1 FROM "+table+" WHERE "+column+" = ?", slug).Scan(&exists)
		} else {
			err = s.db.QueryRow("SELECT 1 FROM "+table+" WHERE campaign_id = ? AND "+column+" = ?", campaignID, slug).Scan(&exists)
		}
		if err == sql.ErrNoRows {
			return slug, nil
		}
		if err != nil {
			return "", err
		}
		slug = base + "-" + strconv.Itoa(counter)
		counter++
	}
}

// --- JSON/бинарные помощники ---

func parseJSONRecord(value string) map[string]float64 {
	result := map[string]float64{}
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return result
	}
	record, ok := parsed.(map[string]any)
	if !ok {
		return result
	}
	for key, item := range record {
		if number, ok := item.(float64); ok {
			result[key] = number
		}
	}
	return result
}

func parseStringArray(value sql.NullString) []string {
	if !value.Valid || value.String == "" {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(value.String), &parsed); err != nil {
		return nil
	}
	items, ok := parsed.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, asString(item))
	}
	return result
}

func parseAbilityArray(value sql.NullString) []CharacterAbility {
	if !value.Valid || value.String == "" {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(value.String), &parsed); err != nil {
		return nil
	}
	items, ok := parsed.([]any)
	if !ok {
		return nil
	}
	abilities := make([]CharacterAbility, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := record["name"].(string)
		description, _ := record["description"].(string)
		if name == "" || description == "" {
			continue
		}
		ability := CharacterAbility{Name: name, Description: description}
		if level, ok := record["level"].(float64); ok {
			value := int(level)
			ability.Level = &value
		}
		abilities = append(abilities, ability)
	}
	return abilities
}

func parseQuestDifficulty(value string) QuestDifficulty {
	if value == string(QuestEasy) || value == string(QuestHard) {
		return QuestDifficulty(value)
	}
	return QuestMedium
}

func parseQuestStatus(value string) QuestStatus {
	switch QuestStatus(value) {
	case QuestOffered, QuestAccepted, QuestActive, QuestCompleted, QuestFailed, QuestAbandoned:
		return QuestStatus(value)
	default:
		return QuestOffered
	}
}

func parseThreadKind(value string) ThreadKind {
	switch ThreadKind(value) {
	case ThreadPromise, ThreadMystery, ThreadDebt, ThreadUnresolved:
		return ThreadKind(value)
	default:
		return ThreadUnresolved
	}
}

func parseRewardPlan(value sql.NullString) *QuestRewardPlan {
	if !value.Valid || value.String == "" {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(value.String), &parsed); err != nil {
		return nil
	}
	plan := &QuestRewardPlan{}
	if xp, ok := parsed["xp"].(float64); ok {
		value := int(xp)
		plan.XP = &value
	}
	if gold, ok := parsed["gold"].(float64); ok {
		value := int(gold)
		plan.Gold = &value
	}
	if items, ok := parsed["items"].([]any); ok {
		for _, item := range items {
			text := asString(item)
			if text != "" {
				plan.Items = append(plan.Items, text)
			}
		}
	}
	if note, ok := parsed["note"].(string); ok && jsTrim(note) != "" {
		plan.Note = &note
	}
	if plan.XP == nil && plan.Gold == nil && len(plan.Items) == 0 && plan.Note == nil {
		return nil
	}
	return plan
}

func asString(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case nil:
		return ""
	default:
		return fmt.Sprint(item)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableStringPtr(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func parseNullableInt(value sql.NullInt64) int {
	if !value.Valid {
		return 0
	}
	return int(value.Int64)
}

func nullableBoundChatID(chat *BoundChat) any {
	if chat == nil {
		return nil
	}
	return chat.ChatID
}

func nullableBoundThreadID(chat *BoundChat) any {
	if chat == nil || chat.MessageThreadID == nil {
		return nil
	}
	return *chat.MessageThreadID
}

func nullableTimeOfDay(value *TimeOfDay) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func nullableConditions(values []string) any {
	if values == nil {
		return nil
	}
	return marshalJSON(values)
}

func nullableStringSlice(values []string) any {
	if values == nil {
		return nil
	}
	return marshalJSON(values)
}

func nullableAbilitySlice(values []CharacterAbility) any {
	if values == nil {
		return nil
	}
	return marshalJSON(values)
}

func stringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func ptr[T any](value T) *T { return &value }
