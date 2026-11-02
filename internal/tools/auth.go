package tools

import (
	"strings"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// campaign возвращает кампанию сессии; обычному вызывающему нужно быть
// участником. Trusted (летописец) проходит без членства.
func (d Deps) campaign(sess *Session) (*store.Campaign, error) {
	if sess == nil || sess.CampaignID == "" && sess.CampaignSlug == "" {
		return nil, errNoCampaign()
	}
	key := sess.CampaignID
	if key == "" {
		key = sess.CampaignSlug
	}
	campaign, err := d.DB.GetCampaign(key)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if campaign == nil {
		return nil, errNoCampaign()
	}
	if sess.Trusted {
		return campaign, nil
	}
	if memberOf(campaign, sess.UserID) == nil {
		return nil, errNotMember()
	}
	return campaign, nil
}

// campaignForWrite — кампания сессии для записи в память: любой участник,
// роль dm не требуется.
func (d Deps) campaignForWrite(sess *Session) (*store.Campaign, error) {
	return d.campaign(sess)
}

// campaignAdmin — кампания сессии для административной операции (dm).
func (d Deps) campaignAdmin(sess *Session) (*store.Campaign, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	member := memberOf(campaign, sess.UserID)
	if member == nil || member.Role != store.RoleDM {
		return nil, errDMOnly()
	}
	return campaign, nil
}

// campaignByID — явная цель админ-тула (start/invite/finish).
func (d Deps) campaignByID(idOrSlug string) (*store.Campaign, error) {
	campaign, err := d.DB.GetCampaign(idOrSlug)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if campaign == nil {
		return nil, errNotFound("Кампания «"+idOrSlug+"» не найдена.",
			"Check the campaign id or slug with list_campaigns and retry.")
	}
	return campaign, nil
}

func memberOf(campaign *store.Campaign, userID string) *store.CampaignMember {
	for index := range campaign.Members {
		if campaign.Members[index].UserID == userID {
			return &campaign.Members[index]
		}
	}
	return nil
}

func memberRole(campaign *store.Campaign, userID string) store.MemberRole {
	if member := memberOf(campaign, userID); member != nil {
		return member.Role
	}
	return ""
}

// requireUser проверяет, что сессия знает говорящего (save_campaign и списки).
func (d Deps) requireUser(sess *Session) error {
	if sess == nil || sess.UserID == "" {
		return errNoIdentity()
	}
	return nil
}

// characterByName находит лист по id/slug/имени без учёта регистра.
func (d Deps) characterByName(campaignID, ref string) (*store.CharacterSheet, error) {
	sheets, err := d.DB.ListCharacters(campaignID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	needle := strings.ToLower(strings.TrimSpace(ref))
	for index := range sheets {
		sheet := &sheets[index]
		if sheet.ID == ref || strings.ToLower(sheet.Slug) == needle || strings.ToLower(sheet.Name) == needle {
			return sheet, nil
		}
	}
	return nil, nil
}

// canActForCharacter — нельзя кидать проверки и действовать за чужого PC.
// Без листа или владельца проверка пропускается (как в TS).
func (d Deps) canActForCharacter(sess *Session, campaign *store.Campaign, characterName string) error {
	sheet, err := d.characterByName(campaign.ID, characterName)
	if err != nil {
		return err
	}
	if sheet == nil || sheet.OwnerUserID == "" {
		return nil
	}
	if sess != nil && (sess.Trusted || memberRole(campaign, sess.UserID) == store.RoleDM || sheet.OwnerUserID == sess.UserID) {
		return nil
	}
	return toolError(CodeAccessDenied, false,
		"Персонаж «"+sheet.Name+"» принадлежит другому игроку. Кидать проверки и действовать за чужого персонажа может только его владелец или DM.",
		"Use the character owned by the caller or ask the DM to perform the action.")
}
