package tools

import (
	"fmt"
	"strings"

	agotools "github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/store"
)

// tool — короткая обёртка над ago, чтобы builders читались компактно.
func tool(name, description string, fn any) (*agotools.FunctionTool, error) {
	return agotools.NewFunctionTool(name, description, fn)
}

// namedTool — тул с именем для сборки toolkit-ов и фильтрации.
type namedTool struct {
	name string
	tool agotools.Tool
}

func campaignTools(d Deps) ([]namedTool, error) {
	builders := []func() (*agotools.FunctionTool, error){}

	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("save_campaign",
			"Save a new campaign after the creation interview is complete. The caller becomes the campaign owner and DM.",
			func(args saveCampaignArgs) (any, error) { return d.saveCampaign(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("start_campaign",
			"Start a campaign in the current chat: binds the campaign to this chat (or forum topic) and makes it active. Only the campaign DM can do this. One chat/topic can host only one active campaign.",
			func(args startCampaignArgs) (any, error) { return d.startCampaign(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("finish_campaign",
			"Finish/close a campaign: marks it as finished, keeps all data (history, characters, NPCs) for later reading, and frees the chat so a new campaign can be started in it. Only the campaign DM can do this. Use when the story is over or the group wants to close the campaign.",
			func(args finishCampaignArgs) (any, error) { return d.finishCampaign(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("invite_member",
			"Invite a user into a campaign. Only a campaign DM can invite; only the owner can invite another DM. Invitees join as players by default.",
			func(args inviteMemberArgs) (any, error) { return d.inviteMember(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("list_campaigns",
			"List campaigns the caller owns or is a member of, with their statuses.",
			func(args listCampaignsArgs) (any, error) { return d.listCampaigns(args.Session) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("save_character",
			"Save a new player character into an existing campaign. The caller must be a campaign member. A character cannot exist outside a campaign. Pass the starting equipment, abilities and HP generated for the character's class/race/level so the sheet is complete from the start.",
			func(args saveCharacterArgs) (any, error) { return d.saveCharacter(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("get_character",
			"Show the full current character sheet: stats, HP, conditions, inventory, abilities, gold, XP, location, background, motivation. Use when a player asks to see their character (/mychar) or the current state of a party member.",
			func(args getCharacterArgs) (any, error) { return d.getCharacter(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("update_character",
			"Update the dynamic state of a player character in the campaign: HP, conditions, inventory, gold, XP, level, location. Call whenever the game changes a character's state (damage, loot, level up, travel) so the party state stays current between sessions.",
			func(args updateCharacterArgs) (any, error) { return d.updateCharacter(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("grant_character",
			"Grant a character new items, abilities, gold, XP or conditions - everything is ADDED to what the character already has (no replacement). Use for loot, quest rewards, learning new abilities, level-up gains.",
			func(args grantCharacterArgs) (any, error) { return d.grantCharacter(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("get_game_context",
			"Load the campaign bound to the current chat and its characters into context. Call this at the first turn of a new session in a game chat before narrating anything.",
			func(args gameContextArgs) (any, error) { return d.getGameContext(args.Session) })
	})

	out := make([]namedTool, 0, len(builders))
	for _, build := range builders {
		created, err := build()
		if err != nil {
			return nil, err
		}
		out = append(out, namedTool{name: created.Name(), tool: created})
	}
	return out, nil
}

type saveCampaignArgs struct {
	Title        string   `json:"title" agschema:"description=Campaign title,required"`
	Length       string   `json:"length" agschema:"enum=short|medium|long,description=short = 1-3 sessions medium = 4-10 long = 10+,required"`
	Setting      string   `json:"setting" agschema:"description=World and environment of the campaign,required"`
	Theme        string   `json:"theme" agschema:"description=Main theme of the campaign,required"`
	Goal         *string  `json:"goal" agschema:"description=Ultimate goal of the players if defined"`
	Tone         *string  `json:"tone" agschema:"description=Narration tone"`
	OpeningScene *string  `json:"openingScene" agschema:"description=Opening scene where the game starts"`
	Description  *string  `json:"description" agschema:"description=Detailed campaign description"`
	Session      *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) saveCampaign(sess *Session, args saveCampaignArgs) (any, error) {
	if err := d.requireUser(sess); err != nil {
		return nil, err
	}
	length := store.CampaignLength(args.Length)
	switch length {
	case store.LengthShort, store.LengthMedium, store.LengthLong:
	default:
		return nil, errInvalid("Недопустимая длина кампании «"+args.Length+"».",
			"Use one of: short, medium, long.")
	}
	if strings.TrimSpace(args.Title) == "" || strings.TrimSpace(args.Setting) == "" || strings.TrimSpace(args.Theme) == "" {
		return nil, errInvalid("Название сеттинг и тема кампании обязательны.",
			"Provide non-empty title, setting and theme.")
	}
	description := ""
	if args.Description != nil {
		description = *args.Description
	}
	campaign, err := d.DB.CreateCampaign(store.NewCampaignInput{
		Title: args.Title, Length: length, Setting: args.Setting, Theme: args.Theme,
		Goal: args.Goal, Tone: args.Tone, OpeningScene: args.OpeningScene, Description: description,
	}, store.NewOwnerInput{UserID: sess.UserID, Name: optionalString(sess.Name), Username: optionalString(sess.Username)})
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true,
		"campaign": map[string]any{
			"id": campaign.ID, "slug": campaign.Slug, "title": campaign.Title, "status": campaign.Status,
		},
		"note": "Кампания сохранена в статусе setup. Чтобы начать игру её нужно запустить через start_campaign — тогда она привяжется к текущему чату.",
	}, nil
}

type startCampaignArgs struct {
	CampaignID string   `json:"campaignId" agschema:"description=Campaign id or slug,required"`
	Session    *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) startCampaign(sess *Session, args startCampaignArgs) (any, error) {
	if sess == nil || sess.UserID == "" {
		return nil, errNoIdentity()
	}
	if sess.ChatID == "" {
		return nil, errInvalid("Не удалось определить текущий чат.",
			"Retry from the bound Telegram chat.")
	}
	campaign, err := d.DB.BindAndActivate(args.CampaignID, sess.UserID, store.BoundChat{
		ChatID: sess.ChatID, MessageThreadID: sess.MessageThreadID,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	characters, err := d.DB.ListCharacters(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	characterList := make([]map[string]any, 0, len(characters))
	for _, sheet := range characters {
		characterList = append(characterList, map[string]any{
			"id": sheet.ID, "name": sheet.Name, "characterClass": sheet.CharacterClass,
			"race": sheet.Race, "level": sheet.Level,
		})
	}
	return map[string]any{
		"ok": true,
		"campaign": map[string]any{
			"id": campaign.ID, "slug": campaign.Slug, "title": campaign.Title,
			"status":  string(campaign.Status),
			"setting": campaign.Setting, "theme": campaign.Theme,
			"goal": nullableString(campaign.Goal), "tone": nullableString(campaign.Tone),
			"openingScene": nullableString(campaign.OpeningScene), "boundChat": boundChatMap(campaign.BoundChat),
		},
		"characters": characterList,
		"note":       "Кампания привязана к этому чату. Открой первую сцену и представь мир игрокам.",
	}, nil
}

type finishCampaignArgs struct {
	CampaignID *string  `json:"campaignId" agschema:"description=Campaign id or slug; defaults to the campaign of this chat"`
	Session    *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) finishCampaign(sess *Session, args finishCampaignArgs) (any, error) {
	if sess == nil || sess.UserID == "" {
		return nil, errNoIdentity()
	}
	campaign, err := d.campaign(sess)
	if args.CampaignID != nil && *args.CampaignID != "" {
		campaign, err = d.campaignByID(*args.CampaignID)
	}
	if err != nil {
		return nil, err
	}
	if memberRole(campaign, sess.UserID) != store.RoleDM {
		return nil, errDMOnly()
	}
	finished, err := d.DB.FinishCampaign(campaign.ID, sess.UserID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true,
		"campaign": map[string]any{
			"id": finished.ID, "slug": finished.Slug, "title": finished.Title, "status": string(finished.Status),
		},
		"note": fmt.Sprintf("Кампания «%s» завершена. Данные сохранены, чат свободен для новой кампании (/newcampaign). Попрощайся с игроками и подведи краткий итог их приключений.", finished.Title),
	}, nil
}

type inviteMemberArgs struct {
	CampaignID string   `json:"campaignId" agschema:"description=Campaign id or slug,required"`
	UserID     string   `json:"userId" agschema:"description=Telegram user id to invite,required"`
	Name       *string  `json:"name" agschema:"description=Telegram display name if known"`
	Username   *string  `json:"username" agschema:"description=Telegram username if known"`
	Role       *string  `json:"role" agschema:"enum=dm|player,description=Campaign role; defaults to player. Only the owner can assign dm"`
	Session    *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) inviteMember(sess *Session, args inviteMemberArgs) (any, error) {
	if sess == nil || sess.UserID == "" {
		return nil, errNoIdentity()
	}
	role := store.RolePlayer
	if args.Role != nil {
		role = store.MemberRole(*args.Role)
	}
	campaign, err := d.DB.AddMember(args.CampaignID, sess.UserID, store.NewMemberInput{
		UserID: args.UserID, Name: args.Name, Username: args.Username, Role: &role,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok":          true,
		"member":      map[string]any{"userId": args.UserID, "role": role},
		"memberCount": len(campaign.Members),
		"note":        "Участник добавлен. Теперь он может создавать персонажей в этой кампании.",
	}, nil
}

type listCampaignsArgs struct {
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) listCampaigns(sess *Session) (any, error) {
	if err := d.requireUser(sess); err != nil {
		return nil, err
	}
	campaigns, err := d.DB.ListForUser(sess.UserID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	items := make([]map[string]any, 0, len(campaigns))
	for _, campaign := range campaigns {
		characters, err := d.DB.ListCharacters(campaign.ID)
		if err != nil {
			return nil, fromStoreError(err)
		}
		items = append(items, map[string]any{
			"id": campaign.ID, "slug": campaign.Slug, "title": campaign.Title,
			"status": string(campaign.Status), "length": string(campaign.Length), "setting": campaign.Setting,
			"isOwner": campaign.OwnerUserID == sess.UserID, "memberCount": len(campaign.Members),
			"characterCount": len(characters), "boundChat": boundChatMap(campaign.BoundChat),
		})
	}
	return map[string]any{"ok": true, "campaigns": items}, nil
}

type saveCharacterArgs struct {
	CampaignID     string         `json:"campaignId" agschema:"description=Campaign id or slug,required"`
	Name           string         `json:"name" agschema:"description=Character name,required"`
	CharacterClass string         `json:"characterClass" agschema:"description=D&D 5e character class,required"`
	Race           string         `json:"race" agschema:"description=Character race,required"`
	Level          *int           `json:"level" agschema:"description=Level defaults to 1,min=1,max=20"`
	Stats          map[string]int `json:"stats" agschema:"description=Ability scores: strength dexterity constitution intelligence wisdom charisma"`
	Background     *string        `json:"background" agschema:"description=Character background"`
	Motivation     *string        `json:"motivation" agschema:"description=Character motivation and goals"`
	Appearance     *string        `json:"appearance" agschema:"description=Short English appearance description for scene illustrations,max=300"`
	Equipment      []string       `json:"equipment" agschema:"description=Starting equipment items"`
	Abilities      []abilityArg   `json:"abilities" agschema:"description=Starting abilities and spells"`
	Gold           *int           `json:"gold" agschema:"description=Starting gold,min=0"`
	MaxHP          *int           `json:"maxHp" agschema:"description=Maximum hit points,min=1,max=1000"`
	HP             *int           `json:"hp" agschema:"description=Current hit points defaults to maxHp,min=-10,max=1000"`
	Session        *Session       `json:"-" aginject:"ttrpg.session"`
}

type abilityArg struct {
	Name        string `json:"name" agschema:"description=Ability or spell name"`
	Description string `json:"description" agschema:"description=What it does and how it is used"`
	Level       *int   `json:"level" agschema:"description=Minimum character level,min=1,max=20"`
}

func (d Deps) saveCharacter(sess *Session, args saveCharacterArgs) (any, error) {
	if sess == nil || sess.UserID == "" {
		return nil, errNoIdentity()
	}
	stats := map[string]float64{}
	for key, value := range args.Stats {
		stats[key] = float64(value)
	}
	abilities := abilitiesFromArgs(args.Abilities)
	sheet, err := d.DB.SaveCharacter(args.CampaignID, sess.UserID, store.NewCharacterInput{
		Name: args.Name, CharacterClass: args.CharacterClass, Race: args.Race,
		Level: args.Level, Stats: stats, Background: args.Background,
		Motivation: args.Motivation, Appearance: args.Appearance,
		Equipment: args.Equipment, Abilities: abilities, Gold: args.Gold,
		MaxHP: args.MaxHP, HP: args.HP,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok":        true,
		"character": characterCardMap(*sheet),
		"note":      "Персонаж сохранён. Покажи игроку полную карточку персонажа.",
	}, nil
}

type getCharacterArgs struct {
	Character *string  `json:"character" agschema:"description=Character name or slug; defaults to the caller's single character"`
	Session   *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) getCharacter(sess *Session, args getCharacterArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	characters, err := d.DB.ListCharacters(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	var sheet *store.CharacterSheet
	if args.Character != nil && strings.TrimSpace(*args.Character) != "" {
		ref := strings.TrimSpace(*args.Character)
		needle := strings.ToLower(ref)
		for index := range characters {
			candidate := &characters[index]
			if candidate.ID == ref || strings.ToLower(candidate.Slug) == needle || strings.ToLower(candidate.Name) == needle {
				sheet = candidate
				break
			}
		}
		if sheet == nil {
			return nil, errNotFound(fmt.Sprintf("Персонаж «%s» не найден в кампании.", ref),
				"Call list_campaigns or check the character name and retry.")
		}
	} else {
		var own []store.CharacterSheet
		for _, candidate := range characters {
			if candidate.OwnerUserID == sess.UserID {
				own = append(own, candidate)
			}
		}
		switch len(own) {
		case 0:
			return nil, errNotFound("У вас нет персонажа в этой кампании.",
				"Specify the character name or create one via /newchar.")
		case 1:
			sheet = &own[0]
		default:
			names := make([]string, 0, len(own))
			for _, candidate := range own {
				names = append(names, candidate.Name)
			}
			return nil, errInvalid("У вас несколько персонажей: "+strings.Join(names, ", ")+". Укажи имя.",
				"Pass the character name explicitly.")
		}
	}
	return map[string]any{
		"ok": true, "campaignSlug": campaign.Slug, "character": characterCardMap(*sheet),
	}, nil
}

type updateCharacterArgs struct {
	Character  string        `json:"character" agschema:"description=Character name or slug,required"`
	Level      *int          `json:"level" agschema:"description=New level,min=1,max=20"`
	HP         *int          `json:"hp" agschema:"description=Current hit points,min=-10,max=1000"`
	MaxHP      *int          `json:"maxHp" agschema:"description=Maximum hit points,min=1,max=1000"`
	Conditions *[]string     `json:"conditions" agschema:"description=Full new list of active conditions"`
	Inventory  *[]string     `json:"inventory" agschema:"description=Full new inventory list"`
	Abilities  *[]abilityArg `json:"abilities" agschema:"description=Full new ability list"`
	Gold       *int          `json:"gold" agschema:"description=Gold,min=0"`
	XP         *int          `json:"xp" agschema:"description=Experience points,min=0"`
	Location   *string       `json:"location" agschema:"description=Where the character is now"`
	Session    *Session      `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) updateCharacter(sess *Session, args updateCharacterArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	var abilities *[]store.CharacterAbility
	if args.Abilities != nil {
		converted := abilitiesFromArgs(*args.Abilities)
		abilities = &converted
	}
	sheet, err := d.DB.UpdateCharacter(campaign.ID, args.Character, store.CharacterStatePatch{
		Level: args.Level, HP: args.HP, MaxHP: args.MaxHP, Conditions: args.Conditions,
		Inventory: args.Inventory, Abilities: abilities, Gold: args.Gold, XP: args.XP,
		Location: args.Location,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true,
		"character": map[string]any{
			"name": sheet.Name, "level": sheet.Level, "hp": nullableInt(sheet.HP),
			"maxHp": nullableInt(sheet.MaxHP), "conditions": stringsOrEmpty(sheet.Conditions),
			"inventory": stringsOrEmpty(sheet.Inventory), "abilities": abilitiesMap(sheet.Abilities),
			"gold": nullableInt(sheet.Gold), "xp": nullableInt(sheet.XP),
			"location": nullableString(sheet.Location),
		},
		"note": "Состояние персонажа обновлено.",
	}, nil
}

type grantCharacterArgs struct {
	Character  string       `json:"character" agschema:"description=Character name or slug,required"`
	Items      []string     `json:"items" agschema:"description=New inventory items to add"`
	Abilities  []abilityArg `json:"abilities" agschema:"description=New abilities and spells; duplicates by name are ignored"`
	Gold       *int         `json:"gold" agschema:"description=Gold to add,min=0"`
	XP         *int         `json:"xp" agschema:"description=Experience to add,min=0"`
	Conditions []string     `json:"conditions" agschema:"description=New active conditions to add"`
	Session    *Session     `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) grantCharacter(sess *Session, args grantCharacterArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	gold := 0
	if args.Gold != nil {
		gold = *args.Gold
	}
	xp := 0
	if args.XP != nil {
		xp = *args.XP
	}
	sheet, err := d.DB.GrantCharacter(campaign.ID, args.Character, store.CharacterGrantPatch{
		Inventory: args.Items, Abilities: abilitiesFromArgs(args.Abilities),
		Gold: gold, XP: xp, Conditions: args.Conditions,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	if args.Gold != nil && *args.Gold > 0 {
		eventID := fmt.Sprintf("grant:%s:gold:%d:%d", args.Character, day, *args.Gold)
		if err := d.DB.AppendLedgerRow(campaign.Slug, store.LedgerRow{
			Day: day, Type: store.LedgerFound, ItemOrGold: fmt.Sprintf("%d золотых", *args.Gold), By: &args.Character,
		}, &eventID); err != nil {
			return nil, fromStoreError(err)
		}
	}
	if len(args.Items) > 0 {
		eventID := fmt.Sprintf("grant:%s:items:%d:%d", args.Character, day, len(args.Items))
		if err := d.DB.AppendLedgerRow(campaign.Slug, store.LedgerRow{
			Day: day, Type: store.LedgerFound, ItemOrGold: strings.Join(args.Items, ", "), By: &args.Character,
		}, &eventID); err != nil {
			return nil, fromStoreError(err)
		}
	}
	return map[string]any{
		"ok": true, "character": characterCardMap(*sheet),
		"note": "Предметы способности и ресурсы добавлены персонажу. Отрази это в ответе игрокам.",
	}, nil
}

type gameContextArgs struct {
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) getGameContext(sess *Session) (any, error) {
	if sess == nil || sess.UserID == "" && !sess.Trusted {
		return nil, errNoIdentity()
	}
	campaign, err := d.campaign(sess)
	if err == noCampaignError {
		return map[string]any{
			"ok": true, "campaign": nil,
			"note": "В этом чате нет привязанной кампании. Можно создать новую (/newcampaign) или запустить существующую (/startcampaign).",
		}, nil
	}
	if err != nil {
		return nil, err
	}
	characters, err := d.DB.ListCharacters(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	members := make([]map[string]any, 0, len(campaign.Members))
	for _, member := range campaign.Members {
		name := member.UserID
		if member.Name != nil {
			name = *member.Name
		} else if member.Username != nil {
			name = *member.Username
		}
		members = append(members, map[string]any{"userId": member.UserID, "name": name, "role": member.Role})
	}
	characterList := make([]map[string]any, 0, len(characters))
	for _, sheet := range characters {
		characterList = append(characterList, map[string]any{
			"id": sheet.ID, "name": sheet.Name, "ownerUserId": sheet.OwnerUserID,
			"characterClass": sheet.CharacterClass, "race": sheet.Race, "level": sheet.Level,
			"stats": sheet.Stats, "motivation": nullableString(sheet.Motivation),
			"background": nullableString(sheet.Background),
		})
	}
	return map[string]any{
		"ok": true,
		"campaign": map[string]any{
			"id": campaign.ID, "slug": campaign.Slug, "title": campaign.Title,
			"status": string(campaign.Status), "length": string(campaign.Length), "setting": campaign.Setting,
			"theme": campaign.Theme, "goal": nullableString(campaign.Goal),
			"tone": nullableString(campaign.Tone), "openingScene": nullableString(campaign.OpeningScene),
			"members": members,
		},
		"characters": characterList,
	}, nil
}

// --- общие преобразования ---

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nullableString(value *string) any {
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

func stringsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func abilitiesFromArgs(items []abilityArg) []store.CharacterAbility {
	out := make([]store.CharacterAbility, 0, len(items))
	for _, item := range items {
		out = append(out, store.CharacterAbility{Name: item.Name, Description: item.Description, Level: item.Level})
	}
	return out
}

func abilitiesMap(items []store.CharacterAbility) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"name": item.Name, "description": item.Description, "level": nullableInt(item.Level)})
	}
	return out
}

func boundChatMap(chat *store.BoundChat) any {
	if chat == nil {
		return nil
	}
	return map[string]any{"chatId": chat.ChatID, "messageThreadId": nullableInt(chat.MessageThreadID)}
}

func characterCardMap(sheet store.CharacterSheet) map[string]any {
	return map[string]any{
		"id": sheet.ID, "name": sheet.Name, "slug": sheet.Slug, "ownerUserId": sheet.OwnerUserID,
		"characterClass": sheet.CharacterClass, "race": sheet.Race, "level": sheet.Level,
		"stats": sheet.Stats, "hp": nullableInt(sheet.HP), "maxHp": nullableInt(sheet.MaxHP),
		"conditions": stringsOrEmpty(sheet.Conditions), "inventory": stringsOrEmpty(sheet.Inventory),
		"abilities": abilitiesMap(sheet.Abilities), "gold": nullableInt(sheet.Gold), "xp": nullableInt(sheet.XP),
		"location": nullableString(sheet.Location), "background": nullableString(sheet.Background),
		"motivation": nullableString(sheet.Motivation), "appearance": nullableString(sheet.Appearance),
		"createdAt": sheet.CreatedAt, "updatedAt": nullableString(sheet.UpdatedAt),
	}
}
