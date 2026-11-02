package tools

import (
	"fmt"
	"math"
	"sort"
	"strings"

	agotools "github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/engine"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func memoryTools(d Deps) ([]namedTool, error) {
	builders := []func() (*agotools.FunctionTool, error){}

	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("create_quest",
			"Create a quest in the campaign: register an offered/accepted quest with its objective difficulty and optional reward plan. Use when an NPC offers the party a task or the players take one on. The quest then lives in the campaign store and is shown in the injected memory. Rewards: fill rewardPlan for special rewards; empty fields are computed from tables on completion.",
			func(args createQuestArgs) (any, error) { return d.createQuest(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("update_quest",
			"Update a quest: change its status (offered/accepted/active/failed/abandoned), objective, difficulty, reward plan or deadline. Use when the story moves the quest forward. To COMPLETE a quest with rewards use complete_quest instead.",
			func(args updateQuestArgs) (any, error) { return d.updateQuest(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("complete_quest",
			"Complete (or fail/abandon) a quest: closes the quest grants the rewards to the whole party (XP to each member, gold split across the party, planned items) and records a key event. Rewards come from the quest's rewardPlan; empty fields are computed from tables by quest difficulty and party level. Reports level-ups found — call level_up for each affected character.",
			func(args completeQuestArgs) (any, error) { return d.completeQuest(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("list_quests",
			"List the campaign's quests, active ones by default. Use to answer the /quests command (the party's journal) or to check quest state and deadlines. Read-only: any campaign member can call it.",
			func(args listQuestsArgs) (any, error) { return d.listQuests(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("append_thread",
			"Append an open thread to the campaign ledger: a promise, mystery, debt or unresolved matter that must resurface later. Open threads are injected into the DM memory every turn so old hooks stay alive. Close them with resolve_thread when the story resolves them.",
			func(args appendThreadArgs) (any, error) { return d.appendThread(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("resolve_thread",
			"Resolve (close) an open thread: a promise kept, a mystery solved, a debt paid. Pass the thread id, or a text fragment that uniquely matches it. The thread moves to resolved and stops being injected into memory.",
			func(args resolveThreadArgs) (any, error) { return d.resolveThread(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("list_open_threads",
			"List open threads (promises, mysteries, debts) of the campaign. Use to refresh which loose ends are alive before planning a scene, or after reading a transcript to find threads to close.",
			func(args listThreadsArgs) (any, error) { return d.listThreads(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("append_key_event",
			"Save one key moment of the campaign into long-term memory. Use for turning points, promises, betrayals, discovered secrets — facts the DM must remember sessions later. One call = one concise event. Set permanent=true for the few pivotal facts (deaths, alliances, revealed secrets, oaths) that must NEVER fall out of the memory block as the campaign grows long.",
			func(args appendKeyEventArgs) (any, error) { return d.appendKeyEvent(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("append_campaign_summary",
			"Add (or replace) the campaign-wide summary section for one in-game day. This is the rolling digest the DM loads in later sessions instead of the full transcript.",
			func(args appendCampaignSummaryArgs) (any, error) { return d.appendCampaignSummary(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("update_day_summary",
			"Write the short summary of one in-game day. Call when closing a day (usually via the chronicler) so later sessions can recall the day without reading the full transcript. The optional headline is a one-line (<=140 chars) title of the day shown for EVERY past day in the memory block so the full campaign arc stays visible.",
			func(args updateDaySummaryArgs) (any, error) { return d.updateDaySummary(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("read_day",
			"Read the transcript of one in-game day of the campaign: who said and did what. Defaults to the current day of the campaign bound to this chat.",
			func(args readDayArgs) (any, error) { return d.readDay(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("search_memory",
			"Search the campaign's long-term memory by keywords: transcripts of past in-game days, key events, loot ledger, world state, NPCs, locations, factions, characters, quests and campaign info. Returns matching snippets with the source label and the in-game day number. Use when you need a fact but do not know which in-game day it happened in. Read-only.",
			func(args searchMemoryArgs) (any, error) { return d.searchMemory(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("list_npcs",
			"List the NPCs of the campaign as a short roster (name, role, status, location). Use get_npc to load a full card with relationships and memory.",
			func(args listNpcsArgs) (any, error) { return d.listNpcs(args.Session) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("get_npc",
			"Load the full card of one NPC: profile, relationships toward the party and the NPC's own memory of what the players did. Use before roleplaying a conversation with a known NPC.",
			func(args getNpcArgs) (any, error) { return d.getNpc(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("upsert_npc",
			"Create or update an NPC of the campaign: profile (role, status, location), relationships toward party members and a memory note about what the players did to this NPC. Call this whenever a notable NPC appears or something changes for them.",
			func(args upsertNpcArgs) (any, error) { return d.upsertNpc(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("list_locations",
			"List the campaign's known locations. By default returns discovered locations (the map the party knows) with their connections; each entry notes whether the party is currently there. Read-only.",
			func(args listLocationsArgs) (any, error) { return d.listLocations(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("upsert_location",
			"Create or update a location of the campaign (map node): description, connections to other locations, the day it was discovered, and whether the party is currently there. Only one location can be current at a time — setting current=true clears it from the others. Use move_party to travel, not this tool.",
			func(args upsertLocationArgs) (any, error) { return d.upsertLocation(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("move_party",
			"Move the party to a discovered location: marks it current (clearing the flag from the previous location), records the visit for the current in-game day, and writes an action line to the day transcript. The location must already exist (create it first with upsert_location). Use this for travel, not upsert_location's current flag.",
			func(args movePartyArgs) (any, error) { return d.moveParty(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("upsert_faction",
			"Create or update a faction of the campaign: a guild, clan, order or other organization with a standing (reputation) toward the party on a -5 (enemy) .. +5 (ally) scale. Use this to introduce political groups; use adjust_standing for reputation changes from quest outcomes.",
			func(args upsertFactionArgs) (any, error) { return d.upsertFaction(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("adjust_standing",
			"Adjust a faction's standing (reputation) toward the party by a delta, clamped to -5 .. +5. Use for in-story reputation shifts (a favor done, a slight). Quest completion adjusts standing automatically via complete_quest — use this tool for standalone events. The faction must exist (create it first with upsert_faction).",
			func(args adjustStandingArgs) (any, error) { return d.adjustStanding(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("list_world_state",
			"Read the current durable state of the world: who is dead, what places changed, what decisions were taken — grouped by category. Reflects what is true now, unlike the append-only key events. Read-only. The world-state is also loaded into the memory block each turn.",
			func(args listWorldStateArgs) (any, error) { return d.listWorldState(args.Session) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("record_world_change",
			"Record a durable change to the world's current state — the opposite of an append-only log: it holds what is true NOW, grouped by category. Use for deaths (category «Погибшие»), destroyed/built places, decisions taken, alliance shifts. Prevents the world from contradicting itself. Idempotent by text within a category.",
			func(args recordWorldChangeArgs) (any, error) { return d.recordWorldChange(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("list_ledger",
			"Read the campaign loot/economy ledger: an append-only log of gold and items found or spent, recorded automatically when characters are granted loot or quests completed. Optionally filter by in-game day or by found/spent, and limit the number of entries. The ledger is not loaded into the memory block (it grows) — use this tool or search_memory to recall it.",
			func(args listLedgerArgs) (any, error) { return d.listLedger(args.Session, args) })
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

// --- args ---

type rewardPlanArg struct {
	XP    *int     `json:"xp" agschema:"description=XP for each participant,min=0"`
	Gold  *int     `json:"gold" agschema:"description=Gold for the whole party,min=0"`
	Items []string `json:"items" agschema:"description=Items for each participant"`
	Note  *string  `json:"note" agschema:"description=Free part of the reward: NPC favor or reputation or story bonus"`
}

func (r *rewardPlanArg) toStore() *store.QuestRewardPlan {
	if r == nil {
		return nil
	}
	return &store.QuestRewardPlan{XP: r.XP, Gold: r.Gold, Items: r.Items, Note: r.Note}
}

type questStatusArg struct {
	Status *string `json:"status" agschema:"enum=offered|accepted|active|failed|abandoned,description=Quest status; completed only via complete_quest"`
}

type createQuestArgs struct {
	Title       string         `json:"title" agschema:"description=Short recognizable quest title,required"`
	GiverNpc    *string        `json:"giverNpc" agschema:"description=NPC name who gave the quest"`
	Objective   string         `json:"objective" agschema:"description=What must be done and for whom,required"`
	Difficulty  string         `json:"difficulty" agschema:"enum=easy|medium|hard,description=Quest difficulty; drives XP and gold tables,required"`
	Status      *string        `json:"status" agschema:"enum=offered|accepted|active|failed|abandoned,description=Quest status defaults to offered"`
	RewardPlan  *rewardPlanArg `json:"rewardPlan" agschema:"description=Planned reward; empty fields are computed on completion"`
	DeadlineDay *int           `json:"deadlineDay" agschema:"description=In-game day deadline,min=1,max=9999"`
	Session     *Session       `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) createQuest(sess *Session, args createQuestArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	difficulty, err := parseDifficulty(args.Difficulty)
	if err != nil {
		return nil, err
	}
	var status *store.QuestStatus
	if args.Status != nil {
		value, err := parseQuestStatus(*args.Status)
		if err != nil {
			return nil, err
		}
		status = &value
	}
	quest, err := d.DB.CreateQuest(campaign.ID, store.NewQuestInput{
		Title: args.Title, GiverNpcSlug: args.GiverNpc, Objective: args.Objective,
		Difficulty: difficulty, Status: status, RewardPlan: args.RewardPlan.toStore(),
		DeadlineDay: args.DeadlineDay,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok":    true,
		"quest": questMap(*quest),
		"note":  "Квест создан и появится в секции «Активные квесты» памяти кампании. Смена статусов — update_quest; завершение с выдачей наград — complete_quest.",
	}, nil
}

type updateQuestArgs struct {
	Quest       string         `json:"quest" agschema:"description=Quest title or slug/id,required"`
	Status      *string        `json:"status" agschema:"enum=offered|accepted|active|failed|abandoned,description=New status; completed only via complete_quest"`
	Title       *string        `json:"title" agschema:"description=New quest title"`
	Objective   *string        `json:"objective" agschema:"description=New objective"`
	Difficulty  *string        `json:"difficulty" agschema:"enum=easy|medium|hard,description=New difficulty"`
	DeadlineDay *int           `json:"deadlineDay" agschema:"description=New in-game deadline day,min=1,max=9999"`
	RewardPlan  *rewardPlanArg `json:"rewardPlan" agschema:"description=Planned reward; replaces the previous one entirely"`
	Session     *Session       `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) updateQuest(sess *Session, args updateQuestArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	patch := store.QuestPatch{Title: args.Title, Objective: args.Objective, DeadlineDay: args.DeadlineDay,
		RewardPlan: args.RewardPlan.toStore()}
	if args.Status != nil {
		status, err := parseQuestStatus(*args.Status)
		if err != nil {
			return nil, err
		}
		patch.Status = &status
	}
	if args.Difficulty != nil {
		difficulty, err := parseDifficulty(*args.Difficulty)
		if err != nil {
			return nil, err
		}
		patch.Difficulty = &difficulty
	}
	quest, err := d.DB.UpdateQuest(campaign.ID, args.Quest, patch)
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true, "quest": questMap(*quest),
		"note": "Квест обновлён. Если квест завершён в сюжете — вызови complete_quest чтобы выдать награды.",
	}, nil
}

type completeQuestArgs struct {
	Quest       string         `json:"quest" agschema:"description=Quest title or slug/id,required"`
	Result      string         `json:"result" agschema:"enum=completed|failed|abandoned,description=Quest outcome; completed grants rewards,required"`
	FactionSlug *string        `json:"factionSlug" agschema:"description=Faction whose standing changes with the outcome"`
	RewardPlan  *rewardPlanArg `json:"rewardPlan" agschema:"description=Override reward for this completion"`
	Session     *Session       `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) completeQuest(sess *Session, args completeQuestArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	if args.Result != "completed" && args.Result != "failed" && args.Result != "abandoned" {
		return nil, errInvalid("Недопустимый итог квеста «"+args.Result+"».",
			"Use completed, failed or abandoned.")
	}
	quests, err := d.DB.ListQuests(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	var quest *store.Quest
	needle := strings.ToLower(strings.TrimSpace(args.Quest))
	for index := range quests {
		candidate := &quests[index]
		if candidate.ID == args.Quest || strings.ToLower(candidate.Slug) == needle || strings.ToLower(candidate.Title) == needle {
			quest = candidate
			break
		}
	}
	if quest == nil {
		return nil, errNotFound(fmt.Sprintf("Квест «%s» не найден в кампании.", args.Quest),
			"Check the quest title or slug with list_quests and retry.")
	}
	if quest.Status == store.QuestCompleted {
		return nil, errConflict(fmt.Sprintf("Квест «%s» уже завершён — награды выданы повторное завершение невозможно.", quest.Title),
			"Choose another quest or report the current state to the players.")
	}
	factionSlug := quest.GiverNpcSlug
	if args.FactionSlug != nil && *args.FactionSlug != "" {
		factionSlug = args.FactionSlug
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	characters, err := d.DB.ListCharacters(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}

	rewards := quest.RewardPlan
	if rewards == nil {
		rewards = &store.QuestRewardPlan{}
	}
	if args.RewardPlan != nil {
		override := args.RewardPlan.toStore()
		if override.XP != nil {
			rewards.XP = override.XP
		}
		if override.Gold != nil {
			rewards.Gold = override.Gold
		}
		if override.Items != nil {
			rewards.Items = override.Items
		}
		if override.Note != nil {
			rewards.Note = override.Note
		}
	}

	granted := []map[string]any{}
	totalGold := 0
	levelUps := []map[string]any{}
	if args.Result == "completed" {
		level := partyLevel(characters)
		xp := engine.QuestXPPerCharacter(engine.QuestDifficulty(quest.Difficulty), level)
		if rewards.XP != nil {
			xp = *rewards.XP
		}
		goldRange := engine.QuestGoldRange(engine.QuestDifficulty(quest.Difficulty), level)
		gold := engine.MiddleOf(goldRange)
		if rewards.Gold != nil {
			gold = *rewards.Gold
		}
		share := gold / max(1, len(characters))
		for _, sheet := range characters {
			items := []string(nil)
			if len(rewards.Items) > 0 {
				items = rewards.Items
			}
			updated, err := d.DB.GrantCharacter(campaign.ID, sheet.Name, store.CharacterGrantPatch{
				XP: xp, Gold: share, Inventory: items,
			})
			if err != nil {
				return nil, fromStoreError(err)
			}
			totalGold += share
			oldLevel := sheet.Level
			if oldLevel < 1 {
				oldLevel = 1
			}
			xpNow := 0
			if updated.XP != nil {
				xpNow = *updated.XP
			}
			nextLevel := engine.LevelForXP(xpNow)
			newLevel := any(nil)
			if nextLevel > oldLevel {
				newLevel = nextLevel
				levelUps = append(levelUps, map[string]any{"character": sheet.Name, "newLevel": nextLevel})
			}
			granted = append(granted, map[string]any{
				"character": sheet.Name, "xp": xp, "gold": share,
				"items": stringsOrEmpty(rewards.Items), "newLevel": newLevel,
			})
		}
		parts := []string{}
		if totalGold > 0 {
			parts = append(parts, fmt.Sprintf("%d золотых", totalGold))
		}
		if len(rewards.Items) > 0 {
			parts = append(parts, strings.Join(rewards.Items, ", "))
		}
		if len(parts) > 0 {
			eventID := fmt.Sprintf("quest:%s:reward:%d", quest.ID, day)
			if err := d.DB.AppendLedgerRow(campaign.Slug, store.LedgerRow{
				Day: day, Type: store.LedgerFound, ItemOrGold: strings.Join(parts, ", "),
				Note: ptr("награда за квест «" + quest.Title + "»"),
			}, &eventID); err != nil {
				return nil, fromStoreError(err)
			}
		}
	}

	if _, err := d.DB.SetQuestStatus(campaign.ID, quest.ID, store.QuestStatus(args.Result)); err != nil {
		return nil, fromStoreError(err)
	}

	var factionChange any
	if factionSlug != nil && *factionSlug != "" {
		delta := factionStandingDelta(quest.Difficulty, args.Result)
		if delta != 0 {
			faction, err := d.DB.AdjustStanding(campaign.ID, *factionSlug, delta)
			if err == nil {
				factionChange = map[string]any{"faction": faction.Name, "standing": faction.Standing, "delta": delta}
			}
		}
	}

	label := "завершён"
	if args.Result == "failed" {
		label = "провален"
	} else if args.Result == "abandoned" {
		label = "брошен"
	}
	bonus := ""
	if rewards.Note != nil && *rewards.Note != "" {
		bonus = " Бонус: " + *rewards.Note
	}
	eventID := fmt.Sprintf("quest:%s:%s", quest.ID, args.Result)
	if err := d.DB.AppendKeyEvent(campaign.Slug, day, fmt.Sprintf("Квест «%s» %s.%s", quest.Title, label, bonus), &eventID, false); err != nil {
		return nil, fromStoreError(err)
	}
	transcriptID := fmt.Sprintf("evt:quest:%s:%s", quest.ID, args.Result)
	transcriptText := fmt.Sprintf("Квест «%s» %s", quest.Title, label)
	if args.Result == "completed" && len(granted) > 0 {
		transcriptText += fmt.Sprintf(": партия получила %v XP и %v золота (каждому)", granted[0]["xp"], granted[0]["gold"])
	}
	if err := d.DB.AppendTranscriptEntry(campaign.Slug, day, store.TranscriptEntry{
		Kind: "action", Text: transcriptText, EventID: &transcriptID,
	}); err != nil {
		return nil, fromStoreError(err)
	}

	note := "Квест отмечен как " + label + ". Награды не выдавались."
	if args.Result == "completed" {
		if len(granted) == 0 {
			note = "Квест отмечен завершённым но награды не выданы: в партии нет персонажей."
		} else if len(levelUps) > 0 {
			names := make([]string, 0, len(levelUps))
			for _, entry := range levelUps {
				names = append(names, entry["character"].(string))
			}
			note = fmt.Sprintf("Награды выданы. %d персонаж(ей) достиг(ли) нового уровня — вызови level_up для каждого: %s.", len(levelUps), strings.Join(names, ", "))
		} else {
			note = "Награды выданы всей партии. Отрази результат в ответе игрокам."
		}
	}
	return map[string]any{
		"ok": true,
		"quest": map[string]any{
			"title": quest.Title, "result": args.Result, "difficulty": quest.Difficulty,
		},
		"granted": granted, "totalGold": totalGold, "levelUps": levelUps,
		"factionChange": factionChange, "note": note,
	}, nil
}

type listQuestsArgs struct {
	Status     *string  `json:"status" agschema:"enum=offered|accepted|active|completed|failed|abandoned,description=Only quests with this status"`
	IncludeAll *bool    `json:"includeAll" agschema:"description=Include completed quests too"`
	Session    *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) listQuests(sess *Session, args listQuestsArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	quests, err := d.DB.ListQuests(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	active := map[store.QuestStatus]bool{store.QuestOffered: true, store.QuestAccepted: true, store.QuestActive: true}
	items := make([]map[string]any, 0, len(quests))
	for _, quest := range quests {
		if args.Status != nil {
			if string(quest.Status) != *args.Status {
				continue
			}
		} else if args.IncludeAll == nil || !*args.IncludeAll {
			if !active[quest.Status] {
				continue
			}
		}
		items = append(items, questMap(quest))
	}
	return map[string]any{
		"ok": true, "quests": items,
		"note": "Для игроков (команда /quests) показывай активные квесты в игровом виде: название цель кто выдал дедлайн. Запланированную награду (rewardPlan) игрокам не раскрывай.",
	}, nil
}

type appendThreadArgs struct {
	Text        string   `json:"text" agschema:"description=One sentence: who promised or owes what or which mystery is open,required"`
	Kind        *string  `json:"kind" agschema:"enum=promise|mystery|debt|unresolved,description=Thread kind"`
	LinkedQuest *string  `json:"linkedQuest" agschema:"description=Quest title this thread relates to"`
	Session     *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) appendThread(sess *Session, args appendThreadArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	var linked *string
	if args.LinkedQuest != nil && strings.TrimSpace(*args.LinkedQuest) != "" {
		quests, err := d.DB.ListQuests(campaign.ID)
		if err != nil {
			return nil, fromStoreError(err)
		}
		needle := strings.ToLower(strings.TrimSpace(*args.LinkedQuest))
		for index := range quests {
			candidate := &quests[index]
			if candidate.ID == *args.LinkedQuest || strings.ToLower(candidate.Slug) == needle || strings.ToLower(candidate.Title) == needle {
				linked = &candidate.ID
				break
			}
		}
		if linked == nil {
			return nil, errNotFound(fmt.Sprintf("Квест «%s» не найден — нить не создана.", *args.LinkedQuest),
				"Check the quest title with list_quests and retry.")
		}
	}
	var kind *store.ThreadKind
	if args.Kind != nil {
		value := store.ThreadKind(*args.Kind)
		switch value {
		case store.ThreadPromise, store.ThreadMystery, store.ThreadDebt, store.ThreadUnresolved:
			kind = &value
		default:
			return nil, errInvalid("Недопустимый тип нити «"+*args.Kind+"».",
				"Use promise, mystery, debt or unresolved.")
		}
	}
	thread, err := d.DB.AppendThread(campaign.ID, store.NewThreadInput{
		Text: args.Text, Kind: kind, LinkedQuestID: linked,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok":     true,
		"thread": threadMap(*thread),
		"note":   "Нить добавлена и будет всплывать в памяти DM пока не закрыта (resolve_thread).",
	}, nil
}

type resolveThreadArgs struct {
	Thread  string   `json:"thread" agschema:"description=Thread id or a unique text fragment,required"`
	Day     *int     `json:"day" agschema:"description=In-game closing day; defaults to the current day,min=1,max=9999"`
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) resolveThread(sess *Session, args resolveThreadArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	thread, err := d.DB.ResolveThread(campaign.ID, args.Thread, args.Day)
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true, "thread": threadMap(*thread),
		"note": "Нить закрыта и больше не подставляется в память DM.",
	}, nil
}

type listThreadsArgs struct {
	IncludeResolved *bool    `json:"includeResolved" agschema:"description=Include resolved threads too"`
	Session         *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) listThreads(sess *Session, args listThreadsArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	threads, err := d.DB.ListThreads(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	items := make([]map[string]any, 0, len(threads))
	for _, thread := range threads {
		if args.IncludeResolved == nil || !*args.IncludeResolved {
			if thread.Status != "open" {
				continue
			}
		}
		items = append(items, threadMap(thread))
	}
	return map[string]any{
		"ok": true, "threads": items,
		"note": "Открытые нити — это обещания и тайны к которым стоит возвращаться в игре. Закрывай их через resolve_thread когда сюжет их разрешил.",
	}, nil
}

type appendKeyEventArgs struct {
	Event     string   `json:"event" agschema:"description=Key event in one or two sentences: who did what and consequences,required"`
	Day       *int     `json:"day" agschema:"description=In-game day; defaults to the current campaign day,min=1"`
	Permanent *bool    `json:"permanent" agschema:"description=true for pivotal facts that must stay in memory for the whole campaign"`
	Session   *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) appendKeyEvent(sess *Session, args appendKeyEventArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	if args.Day != nil {
		day = *args.Day
	}
	permanent := args.Permanent != nil && *args.Permanent
	if err := d.DB.AppendKeyEvent(campaign.Slug, day, args.Event, nil, permanent); err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{"ok": true, "day": day, "note": "Ключевое событие сохранено в память кампании."}, nil
}

type appendCampaignSummaryArgs struct {
	Day     int      `json:"day" agschema:"description=In-game day for this summary,min=1,required"`
	Text    string   `json:"text" agschema:"description=Concise day summary for the campaign chronicle"`
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) appendCampaignSummary(sess *Session, args appendCampaignSummaryArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	if err := d.DB.UpsertCampaignSummaryDay(campaign.Slug, args.Day, args.Text); err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{"ok": true, "day": args.Day, "note": "Саммари дня добавлено в хронику кампании."}, nil
}

type updateDaySummaryArgs struct {
	Day      *int     `json:"day" agschema:"description=In-game day; defaults to the current day,min=1"`
	Summary  string   `json:"summary" agschema:"description=Day summary: place main events and outcomes,required"`
	Headline *string  `json:"headline" agschema:"description=One-line day headline up to 140 chars"`
	Session  *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) updateDaySummary(sess *Session, args updateDaySummaryArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	if args.Day != nil {
		day = *args.Day
	}
	if err := d.DB.SetDaySummary(campaign.Slug, day, args.Summary); err != nil {
		return nil, fromStoreError(err)
	}
	if args.Headline != nil && *args.Headline != "" {
		if err := d.DB.SetDayHeadline(campaign.Slug, day, *args.Headline); err != nil {
			return nil, fromStoreError(err)
		}
	}
	return map[string]any{"ok": true, "day": day, "note": "Саммари дня записано."}, nil
}

type readDayArgs struct {
	Day     *int     `json:"day" agschema:"description=In-game day; defaults to the current day,min=1"`
	Last    *int     `json:"last" agschema:"description=Return only the last N entries of the day,min=1,max=500"`
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) readDay(sess *Session, args readDayArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	if args.Day != nil {
		day = *args.Day
	}
	maxLines := 0
	if args.Last != nil {
		maxLines = *args.Last
	}
	record, err := d.DB.ReadDayTail(campaign.Slug, day, maxLines)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if record == nil {
		return nil, errNotFound(fmt.Sprintf("За игровой день %d записей ещё нет.", day),
			"Check the day number or add transcript entries first.")
	}
	return map[string]any{
		"ok": true, "campaignSlug": campaign.Slug, "day": record.Day,
		"date": nullableString(record.Date), "note": nullableString(record.Note),
		"summary": nullableString(record.Summary), "entries": stringsOrEmpty(record.Entries),
	}, nil
}

type searchMemoryArgs struct {
	Query   string   `json:"query" agschema:"description=Search keywords: name place item or event; case-insensitive,required"`
	Limit   *int     `json:"limit" agschema:"description=Maximum results,min=1,max=40"`
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) searchMemory(sess *Session, args searchMemoryArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	options := store.SearchOptions{}
	if args.Limit != nil {
		options.Limit = *args.Limit
	}
	hits, err := d.DB.SearchCampaignMemory(campaign.Slug, args.Query, options)
	if err != nil {
		return nil, fromStoreError(err)
	}
	results := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		results = append(results, map[string]any{
			"source": hit.Source, "day": nullableInt(hit.Day), "snippet": hit.Snippet,
		})
	}
	note := "Записи памяти по запросу."
	if len(results) == 0 {
		note = "Ничего не найдено."
	}
	return map[string]any{
		"ok": true, "campaignSlug": campaign.Slug, "count": len(results),
		"results": results, "note": note,
	}, nil
}

type listNpcsArgs struct {
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) listNpcs(sess *Session) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	npcs, err := d.DB.ListNpcs(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	items := make([]map[string]any, 0, len(npcs))
	for _, npc := range npcs {
		items = append(items, map[string]any{
			"name": npc.Name, "role": nullableString(npc.Role), "status": string(npc.Status),
			"location": nullableString(npc.Location), "lastSeenDay": nullableInt(npc.LastSeenDay),
		})
	}
	return map[string]any{"ok": true, "count": len(items), "npcs": items}, nil
}

type getNpcArgs struct {
	Name    string   `json:"name" agschema:"description=NPC name or slug,required"`
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) getNpc(sess *Session, args getNpcArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	npc, err := d.DB.GetNpc(campaign.ID, args.Name)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if npc == nil {
		return nil, errNotFound(fmt.Sprintf("NPC «%s» в кампании нет.", args.Name),
			"Check the roster with list_npcs or create the NPC with upsert_npc.")
	}
	return map[string]any{
		"ok": true,
		"npc": map[string]any{
			"name": npc.Name, "slug": npc.Slug, "role": nullableString(npc.Role),
			"status": string(npc.Status), "location": nullableString(npc.Location),
			"relationships": relationshipsMap(npc.Relationships),
			"firstSeenDay":  nullableInt(npc.FirstSeenDay), "lastSeenDay": nullableInt(npc.LastSeenDay),
			"memory": nullableString(optionalString(npc.Memory)),
		},
	}, nil
}

type npcRelationshipArg struct {
	Attitude int     `json:"attitude" agschema:"description=Attitude: -5 enemy .. +5 ally,min=-5,max=5"`
	Notes    *string `json:"notes" agschema:"description=Short note why the NPC feels this way"`
}

type upsertNpcArgs struct {
	Name            string                        `json:"name" agschema:"description=NPC name,required"`
	Role            *string                       `json:"role" agschema:"description=Role or description: innkeeper guard captain etc"`
	Status          *string                       `json:"status" agschema:"enum=alive|dead|unknown,description=NPC status"`
	Location        *string                       `json:"location" agschema:"description=Where the NPC is now"`
	Relationships   map[string]npcRelationshipArg `json:"relationships" agschema:"description=Relationships to characters keyed by character name"`
	FirstSeenDay    *int                          `json:"firstSeenDay" agschema:"description=In-game day of first appearance,min=1"`
	LastSeenDay     *int                          `json:"lastSeenDay" agschema:"description=In-game day of last meeting,min=1"`
	MemoryAppend    *string                       `json:"memoryAppend" agschema:"description=What the players did to this NPC / what the NPC remembers"`
	MemoryAppendDay *int                          `json:"memoryAppendDay" agschema:"description=In-game day for the memory note,min=1"`
	Session         *Session                      `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) upsertNpc(sess *Session, args upsertNpcArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	previous, err := d.DB.GetNpc(campaign.ID, args.Name)
	if err != nil {
		return nil, fromStoreError(err)
	}
	becomingDead := args.Status != nil && *args.Status == string(store.NpcDead) && (previous == nil || previous.Status != store.NpcDead)
	var status *store.NpcStatus
	if args.Status != nil {
		value := store.NpcStatus(*args.Status)
		switch value {
		case store.NpcAlive, store.NpcDead, store.NpcUnknown:
			status = &value
		default:
			return nil, errInvalid("Недопустимый статус NPC «"+*args.Status+"».",
				"Use alive, dead or unknown.")
		}
	}
	relationships := map[string]store.NpcRelationship{}
	for name, relation := range args.Relationships {
		relationships[name] = store.NpcRelationship{Attitude: relation.Attitude, Notes: relation.Notes}
	}
	npc, err := d.DB.UpsertNpc(campaign.ID, store.NpcUpsertInput{
		Name: args.Name, Role: args.Role, Status: status, Location: args.Location,
		Relationships: relationships, FirstSeenDay: args.FirstSeenDay, LastSeenDay: args.LastSeenDay,
		MemoryAppend: args.MemoryAppend, MemoryAppendDay: args.MemoryAppendDay,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	if becomingDead {
		day := 1
		if campaign.CurrentDay != nil {
			day = *campaign.CurrentDay
		} else if previous != nil && previous.LastSeenDay != nil {
			day = *previous.LastSeenDay
		}
		text := npc.Name
		if npc.Location != nil {
			text += " (" + *npc.Location + ")"
		}
		if npc.Role != nil {
			text += " — " + *npc.Role
		}
		if err := d.DB.UpsertWorldChange(campaign.Slug, store.WorldChange{
			Category: "Погибшие", Text: text, Day: &day,
		}); err != nil {
			return nil, fromStoreError(err)
		}
	}
	return map[string]any{
		"ok": true,
		"npc": map[string]any{
			"name": npc.Name, "slug": npc.Slug, "role": nullableString(npc.Role),
			"status": string(npc.Status), "location": nullableString(npc.Location),
			"relationships": relationshipsMap(npc.Relationships),
		},
		"note": "NPC сохранён.",
	}, nil
}

type locationConnectionArg struct {
	To            string  `json:"to" agschema:"description=Where the connection leads: location name or slug"`
	Via           *string `json:"via" agschema:"description=Through what: road river pass"`
	DiscoveredDay *int    `json:"discoveredDay" agschema:"description=In-game day the connection was discovered,min=1"`
}

type listLocationsArgs struct {
	IncludeUndiscovered *bool    `json:"includeUndiscovered" agschema:"description=Include locations without a discovered day"`
	Session             *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) listLocations(sess *Session, args listLocationsArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	locations, err := d.DB.ListLocations(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	items := make([]map[string]any, 0, len(locations))
	currentName := any(nil)
	for _, location := range locations {
		if location.Current != nil && *location.Current {
			currentName = location.Name
		}
		if args.IncludeUndiscovered == nil || !*args.IncludeUndiscovered {
			if location.DiscoveredDay == nil {
				continue
			}
		}
		items = append(items, locationMap(location))
	}
	note := "Текущая локация не задана."
	if currentName != nil {
		note = fmt.Sprintf("Партия сейчас в «%v».", currentName)
	}
	return map[string]any{"ok": true, "count": len(items), "current": currentName, "locations": items, "note": note}, nil
}

type upsertLocationArgs struct {
	Name          string                  `json:"name" agschema:"description=Location name,required"`
	Description   *string                 `json:"description" agschema:"description=What is known about the location"`
	Connections   []locationConnectionArg `json:"connections" agschema:"description=Connections to other locations; replaces existing ones"`
	DiscoveredDay *int                    `json:"discoveredDay" agschema:"description=In-game day the location was discovered,min=1"`
	Current       *bool                   `json:"current" agschema:"description=true if the party is here now; clears the flag from others"`
	Session       *Session                `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) upsertLocation(sess *Session, args upsertLocationArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	var connections *[]store.LocationConnection
	if args.Connections != nil {
		converted := make([]store.LocationConnection, 0, len(args.Connections))
		for _, connection := range args.Connections {
			converted = append(converted, store.LocationConnection{To: connection.To, Via: connection.Via, DiscoveredDay: connection.DiscoveredDay})
		}
		connections = &converted
	}
	location, err := d.DB.UpsertLocation(campaign.ID, store.UpsertLocationInput{
		Name: args.Name, Description: args.Description, Connections: connections,
		DiscoveredDay: args.DiscoveredDay, Current: args.Current,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{"ok": true, "location": locationMap(*location), "note": "Локация сохранена."}, nil
}

type movePartyArgs struct {
	Location string   `json:"location" agschema:"description=Destination location name or slug,required"`
	Via      *string  `json:"via" agschema:"description=How the party travelled: road river pass"`
	Session  *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) moveParty(sess *Session, args movePartyArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	target, err := d.DB.GetLocation(campaign.ID, args.Location)
	if err != nil {
		return nil, fromStoreError(err)
	}
	if target == nil {
		return nil, errNotFound(fmt.Sprintf("Локация «%s» не найдена.", args.Location),
			"Create it first with upsert_location.")
	}
	if err := d.DB.SetCurrent(campaign.ID, target.Slug); err != nil {
		return nil, fromStoreError(err)
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	if _, err := d.DB.MarkVisited(campaign.ID, target.Slug, day); err != nil {
		return nil, fromStoreError(err)
	}
	via := ""
	if args.Via != nil && *args.Via != "" {
		via = " через " + *args.Via
	}
	eventID := fmt.Sprintf("move:%s:%d", target.Slug, day)
	if err := d.DB.AppendTranscriptEntry(campaign.Slug, day, store.TranscriptEntry{
		Kind: "action", Text: fmt.Sprintf("Партия переместилась в «%s»%s.", target.Name, via), EventID: &eventID,
	}); err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true, "location": target.Name, "currentDay": day,
		"note": fmt.Sprintf("Партия теперь в локации «%s». Перемещение записано в транскрипт дня %d.", target.Name, day),
	}, nil
}

type upsertFactionArgs struct {
	Name        string   `json:"name" agschema:"description=Faction name,required"`
	Description *string  `json:"description" agschema:"description=What is known about the faction: goals members temper"`
	Standing    *int     `json:"standing" agschema:"description=Current reputation: -5 enemy .. +5 ally,min=-5,max=5"`
	Session     *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) upsertFaction(sess *Session, args upsertFactionArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	faction, err := d.DB.UpsertFaction(campaign.ID, store.UpsertFactionInput{
		Name: args.Name, Description: args.Description, Standing: args.Standing,
	})
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{"ok": true, "faction": factionMap(*faction), "note": "Фракция сохранена."}, nil
}

type adjustStandingArgs struct {
	Faction string   `json:"faction" agschema:"description=Faction name or slug,required"`
	Delta   int      `json:"delta" agschema:"description=How much to change reputation (clamped to -5..+5),min=-5,max=5,required"`
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) adjustStanding(sess *Session, args adjustStandingArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	faction, err := d.DB.AdjustStanding(campaign.ID, args.Faction, args.Delta)
	if err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true, "faction": factionMap(*faction), "delta": args.Delta,
		"note": fmt.Sprintf("Репутация фракции «%s» теперь %d.", faction.Name, faction.Standing),
	}, nil
}

type listWorldStateArgs struct {
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) listWorldState(sess *Session) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	rendered, err := d.DB.RenderWorldState(campaign.Slug)
	if err != nil {
		return nil, fromStoreError(err)
	}
	note := "Текущие факты мира по категориям."
	var state any = rendered
	if rendered == "" {
		note = "Состояние мира пусто."
		state = nil
	}
	return map[string]any{"ok": true, "empty": rendered == "", "state": state, "note": note}, nil
}

type recordWorldChangeArgs struct {
	Category string   `json:"category" agschema:"description=Fact category: «Погибшие» «Изменения» «Решения» etc,required"`
	Text     string   `json:"text" agschema:"description=The fact itself (current world state),required"`
	Day      *int     `json:"day" agschema:"description=In-game day the fact became true,min=1"`
	Session  *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) recordWorldChange(sess *Session, args recordWorldChangeArgs) (any, error) {
	campaign, err := d.campaignForWrite(sess)
	if err != nil {
		return nil, err
	}
	if err := d.DB.UpsertWorldChange(campaign.Slug, store.WorldChange{
		Category: args.Category, Text: args.Text, Day: args.Day,
	}); err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true, "category": args.Category,
		"note": fmt.Sprintf("Факт мира записан: [%s] %s.", args.Category, args.Text),
	}, nil
}

type listLedgerArgs struct {
	Day     *int     `json:"day" agschema:"description=Only entries of this in-game day,min=1"`
	Type    *string  `json:"type" agschema:"enum=found|spent,description=Only found or only spent entries"`
	Limit   *int     `json:"limit" agschema:"description=How many latest entries to return,min=1,max=200"`
	Session *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) listLedger(sess *Session, args listLedgerArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	filter := &store.LedgerFilter{Day: args.Day}
	if args.Type != nil {
		value := store.LedgerType(*args.Type)
		if value != store.LedgerFound && value != store.LedgerSpent {
			return nil, errInvalid("Недопустимый тип записи журнала «"+*args.Type+"».",
				"Use found or spent.")
		}
		filter.Type = &value
	}
	limit := 50
	if args.Limit != nil {
		limit = *args.Limit
	}
	entries, err := d.DB.ReadLedger(campaign.Slug, filter, limit)
	if err != nil {
		return nil, fromStoreError(err)
	}
	note := "Записи — от старых к новым."
	if len(entries) == 0 {
		note = "Журнал пуст (или нет записей по фильтру)."
	}
	return map[string]any{"ok": true, "count": len(entries), "entries": stringsOrEmpty(entries), "note": note}, nil
}

// --- helpers ---

func parseDifficulty(value string) (store.QuestDifficulty, error) {
	switch value {
	case string(store.QuestEasy), string(store.QuestMedium), string(store.QuestHard):
		return store.QuestDifficulty(value), nil
	default:
		return "", errInvalid("Недопустимая сложность квеста «"+value+"».",
			"Use easy, medium or hard.")
	}
}

func parseQuestStatus(value string) (store.QuestStatus, error) {
	switch store.QuestStatus(value) {
	case store.QuestOffered, store.QuestAccepted, store.QuestActive, store.QuestCompleted, store.QuestFailed, store.QuestAbandoned:
		return store.QuestStatus(value), nil
	default:
		return "", errInvalid("Недопустимый статус квеста «"+value+"».",
			"Use offered, accepted, active, failed or abandoned (completed via complete_quest).")
	}
}

func partyLevel(characters []store.CharacterSheet) int {
	if len(characters) == 0 {
		return 1
	}
	sum := 0
	for _, sheet := range characters {
		level := sheet.Level
		if level < 1 {
			level = 1
		}
		sum += level
	}
	return max(1, int(math.Round(float64(sum)/float64(len(characters)))))
}

func factionStandingDelta(difficulty store.QuestDifficulty, result string) int {
	switch result {
	case "completed":
		if difficulty == store.QuestHard {
			return 2
		}
		return 1
	case "failed", "abandoned":
		if difficulty == store.QuestHard {
			return -2
		}
		return -1
	default:
		return 0
	}
}

func questMap(quest store.Quest) map[string]any {
	return map[string]any{
		"id": quest.ID, "title": quest.Title, "giverNpc": nullableString(quest.GiverNpcSlug),
		"objective": quest.Objective, "difficulty": string(quest.Difficulty), "status": string(quest.Status),
		"rewardPlan": rewardPlanMap(quest.RewardPlan), "deadlineDay": nullableInt(quest.DeadlineDay),
		"createdDay": quest.CreatedDay,
	}
}

func rewardPlanMap(plan *store.QuestRewardPlan) any {
	if plan == nil {
		return nil
	}
	return map[string]any{
		"xp": nullableInt(plan.XP), "gold": nullableInt(plan.Gold),
		"items": stringsOrEmpty(plan.Items), "note": nullableString(plan.Note),
	}
}

func threadMap(thread store.OpenThread) map[string]any {
	return map[string]any{
		"id": thread.ID, "text": thread.Text, "kind": string(thread.Kind), "status": thread.Status,
		"dayOpened": thread.DayOpened, "dayClosed": nullableInt(thread.DayClosed),
	}
}

func relationshipsMap(values map[string]store.NpcRelationship) map[string]any {
	out := map[string]any{}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		relation := values[name]
		out[name] = map[string]any{"attitude": relation.Attitude, "notes": nullableString(relation.Notes)}
	}
	return out
}

func locationMap(location store.Location) map[string]any {
	connections := make([]map[string]any, 0, len(location.Connections))
	for _, connection := range location.Connections {
		connections = append(connections, map[string]any{
			"to": connection.To, "via": nullableString(connection.Via),
			"discoveredDay": nullableInt(connection.DiscoveredDay),
		})
	}
	current := false
	if location.Current != nil {
		current = *location.Current
	}
	return map[string]any{
		"name": location.Name, "slug": location.Slug, "description": nullableString(location.Description),
		"connections": connections, "discoveredDay": nullableInt(location.DiscoveredDay),
		"visitedDays": location.VisitedDays, "current": current,
	}
}

func factionMap(faction store.Faction) map[string]any {
	return map[string]any{
		"name": faction.Name, "slug": faction.Slug,
		"description": nullableString(faction.Description), "standing": faction.Standing,
	}
}
