package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	agotools "github.com/luckystrker/ago/tools"

	"github.com/luckystriker/ai-dnd-system/internal/engine"
	"github.com/luckystriker/ai-dnd-system/internal/store"
)

func diceTools(d Deps) ([]namedTool, error) {
	builders := []func() (*agotools.FunctionTool, error){}

	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("roll_dice",
			`Roll dice using standard notation (e.g. '4d20', '2d6+1d8+3', '1d20+5', 'd8'). Use for any uncertain game outcome: enemy attacks vs a player's AC, random tables, damage outside the combat tool, loot, etc. Advantage/disadvantage applies to d20 dice ONLY (each d20 is rolled twice, keeping the higher for advantage or the lower for disadvantage). Group modifiers: khN/klN keep the highest/lowest N (e.g. '4d6kh3' rolls 4d6 and keeps the 3 highest), dhN/dlN drop the highest/lowest N (e.g. '4d6dl1' drops the lowest, same as keep-highest-3 — use for ability score generation), and '!' rolls exploding dice (a die on its max face adds another, e.g. '8d6!'). Never use invented results.`,
			func(args rollDiceArgs) (any, error) { return d.rollDice(args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("skill_check",
			"Perform a d20 skill check using the character's ability modifier from the character sheet in SQLite. Natural 20 always succeeds, natural 1 always fails. Pass advantage=true when the situation favors the character (roll 2d20 keep higher), advantage=false for disadvantage (keep lower), null for a normal single roll. The output contains the full roll breakdown (skill, ability, d20, modifier, total, DC, outcome) - always show it to the player.",
			func(args skillCheckArgs) (any, error) { return d.skillCheck(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("initiative",
			"Roll initiative and START turn-based combat. Call this the moment a fight begins: list EVERY participant — party characters (side=party) and enemies (side=enemy, with their armor class ac and current hit points hp). Party members' HP is loaded from their sheets automatically. Each combatant gets a stable id (shown in the result next to its name); pass that id to the combat tool's attacker/enemy/target fields — ids are exact, names are matched as a fallback only. If a new enemy joins an ongoing fight, call this again with the full updated list — the order is re-rolled and ids may change.",
			func(args initiativeArgs) (any, error) { return d.initiative(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("combat",
			`Resolve turn-based combat tracked by the initiative tool. actions: attack — the CURRENT combatant of the party attacks an enemy by its id or name (d20 + ability modifier + proficiency vs enemy AC; natural 20 crits with double damage, natural 1 misses; damage dice come from the weapon in the character's inventory); only one attack per turn, the turn then advances; pass the id (from initiative) for an exact match — names are a tolerant fallback; damage — apply damage (or heal) to a party member's HP in the turn order by id or name and persist it to the character sheet; dodge — the current party combatant takes the Dodge action: incoming attacks against them have disadvantage until their next turn (roll the enemy's attack with roll_dice advantage='disadvantage'); the turn advances; next — advance to the next combatant without an attack (after enemy turns, movement, Dash/Disengage, etc.); status — show the current order, HP and whose turn it is; end — end the combat and clear all enemies.`,
			func(args combatArgs) (any, error) { return d.combat(args.Session, args) })
	})
	builders = append(builders, func() (*agotools.FunctionTool, error) {
		return tool("level_up",
			"Level up a character: advances level to the given (or XP-computed) value, increases max HP by hit die + CON per level, and adds the new ability you pass. Call when complete_quest reports a level-up, or when the story grants one. The ability must follow the balance rules of character creation (level-appropriate, no epic spells at low levels).",
			func(args levelUpArgs) (any, error) { return d.levelUp(args.Session, args) })
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

// --- roll_dice ---

type rollDiceArgs struct {
	Notation  string  `json:"notation" agschema:"description=Dice notation: 4d20 2d4 2d6+1d8+3 1d20+5 d8 4d6kh3 4d6dl1 8d6!,required"`
	Advantage *string `json:"advantage" agschema:"enum=advantage|disadvantage|normal,description=Applies to d20 dice only; defaults to normal"`
}

func (d Deps) rollDice(args rollDiceArgs) (any, error) {
	advantage := "normal"
	if args.Advantage != nil && *args.Advantage != "" {
		advantage = *args.Advantage
	}
	var adv *bool
	switch advantage {
	case "advantage":
		adv = ptr(true)
	case "disadvantage":
		adv = ptr(false)
	case "normal":
	default:
		return nil, errInvalid("Недопустимый режим преимущества «"+advantage+"».",
			"Use advantage, disadvantage or normal.")
	}
	result, err := engine.RollDiceNotation(args.Notation, engine.NotationOptions{Random: d.random(), Advantage: adv})
	if err != nil {
		return nil, errInvalid(engine.FormatRollError(args.Notation, err),
			"Fix the dice notation and retry. Examples: 1d20+5 2d6+1d8+3 4d6kh3.")
	}
	return engine.FormatDiceRoll(result, engine.RollAdvantage(advantage)), nil
}

// --- skill_check ---

type skillCheckArgs struct {
	CharacterName string   `json:"character_name" agschema:"description=Character name or slug,required"`
	Skill         string   `json:"skill" agschema:"description=Skill name in English or Russian,required"`
	Difficulty    int      `json:"difficulty" agschema:"description=Difficulty class (DC),min=1,max=100,required"`
	Advantage     *bool    `json:"advantage" agschema:"description=true advantage false disadvantage null normal"`
	Session       *Session `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) skillCheck(sess *Session, args skillCheckArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	sheet, err := d.characterByName(campaign.ID, args.CharacterName)
	if err != nil {
		return nil, err
	}
	if sheet != nil {
		if err := d.canActForCharacter(sess, campaign, args.CharacterName); err != nil {
			return nil, err
		}
	}
	stats := map[string]any{}
	if sheet != nil {
		for key, value := range sheet.Stats {
			stats[key] = value
		}
	}
	environment := engine.Environment{}
	if campaign.TimeOfDay != nil {
		environment.TimeOfDay = string(*campaign.TimeOfDay)
	}
	if campaign.Weather != nil {
		environment.Weather = *campaign.Weather
	}
	env := engine.EnvironmentModifiersForCheck(environment, args.Skill)
	effective := engine.CombineAdvantage(args.Advantage, env.Advantage)

	key := sess.DiceKey()
	lucky := engine.MakeLuckyRandom(d.random(), engine.IsLowStreak(d.Dice.Recent(key)))
	result := engine.SkillCheck(stats, args.Skill, args.Difficulty, effective, lucky)
	d.Dice.Append(key, result.Roll)
	return engine.FormatSkillCheck(args.CharacterName, args.Skill, result, effective, len(stats) > 0, env.Reasons), nil
}

// --- initiative ---

type initiativeCombatantArg struct {
	Name  string `json:"name" agschema:"description=Combatant name"`
	Side  string `json:"side" agschema:"enum=party|enemy,description=party — player character; enemy — enemy"`
	Bonus *int   `json:"bonus" agschema:"description=Initiative modifier (DEX),min=-10,max=20"`
	HP    *int   `json:"hp" agschema:"description=Enemies: current hit points (required for enemy),min=1,max=100000"`
	AC    *int   `json:"ac" agschema:"description=Enemies: armor class (required for enemy),min=1,max=40"`
}

type initiativeArgs struct {
	Combatants []initiativeCombatantArg `json:"combatants" agschema:"description=Every participant: party characters and enemies with hp and ac,required"`
	Session    *Session                 `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) initiative(sess *Session, args initiativeArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	unlock := d.lockCampaign(campaign.ID)
	defer unlock()
	if len(args.Combatants) == 0 {
		return nil, errInvalid("Список участников пуст.",
			"Pass every combatant including party characters and enemies with hp and ac.")
	}
	sheets, err := d.DB.ListCharacters(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	inputs := make([]engine.CombatantInput, 0, len(args.Combatants))
	for _, combatant := range args.Combatants {
		side := engine.CombatantSide(combatant.Side)
		if side != engine.SideParty && side != engine.SideEnemy {
			return nil, errInvalid("Недопустимая сторона «"+combatant.Side+"».",
				"Use party or enemy.")
		}
		bonus := 0
		if combatant.Bonus != nil {
			bonus = *combatant.Bonus
		}
		input := engine.CombatantInput{Name: combatant.Name, Side: side, Bonus: bonus, HP: combatant.HP, AC: combatant.AC}
		if side == engine.SideEnemy {
			if combatant.HP == nil || combatant.AC == nil {
				return nil, errInvalid("Для врага «"+combatant.Name+"» нужны hp и ac.",
					"Pass hp and ac for every enemy.")
			}
		} else {
			for index := range sheets {
				sheet := &sheets[index]
				if strings.EqualFold(sheet.Name, combatant.Name) || strings.EqualFold(sheet.Slug, combatant.Name) || sheet.ID == combatant.Name {
					input.ID = sheet.ID
					break
				}
			}
		}
		inputs = append(inputs, input)
	}
	order := engine.RollInitiative(inputs, d.random())
	order = attachPartyHP(order, sheets)
	enemies := make([]store.SerializedEnemy, 0)
	for _, entry := range order {
		if entry.Side != engine.SideEnemy {
			continue
		}
		hp, ac := 0, 0
		if entry.HP != nil {
			hp = *entry.HP
		}
		if entry.AC != nil {
			ac = *entry.AC
		}
		enemies = append(enemies, store.SerializedEnemy{ID: entry.ID, Name: entry.Name, HP: hp, AC: ac})
	}
	combat := engine.CombatOrder{Started: len(order) > 0, Round: 1, Current: -1}
	if len(order) > 0 {
		combat.Current = 0
	}
	combat.Order = order
	if err := d.DB.SaveCombatState(campaign.Slug, store.CombatSnapshot{Combat: combat, Enemies: enemies}); err != nil {
		return nil, fromStoreError(err)
	}
	return engine.FormatInitiative(order), nil
}

func attachPartyHP(entries []engine.CombatantEntry, sheets []store.CharacterSheet) []engine.CombatantEntry {
	for index := range entries {
		if entries[index].Side != engine.SideParty {
			continue
		}
		for _, sheet := range sheets {
			if sheet.ID == entries[index].ID || strings.EqualFold(sheet.Name, entries[index].Name) {
				entries[index].HP = sheet.HP
				entries[index].MaxHP = sheet.MaxHP
				break
			}
		}
	}
	return entries
}

// --- combat ---

type combatArgs struct {
	Action     string   `json:"action" agschema:"enum=attack|damage|dodge|next|status|end,description=Combat action,required"`
	Attacker   *string  `json:"attacker" agschema:"description=Party member id or name (recommended id from initiative)"`
	Enemy      *string  `json:"enemy" agschema:"description=Enemy id or name (recommended id from initiative)"`
	Target     *string  `json:"target" agschema:"description=Party member id or name for damage or healing"`
	Amount     *int     `json:"amount" agschema:"description=Damage or healing amount,min=1,max=1000"`
	Heal       *bool    `json:"heal" agschema:"description=Heal instead of damage"`
	AttackStat *string  `json:"attack_stat" agschema:"enum=str|dex,description=Attack ability: str or dex (finesse)"`
	Bonus      *int     `json:"bonus" agschema:"description=Additional attack bonus from magic or spells,min=0,max=5"`
	Weapon     *string  `json:"weapon" agschema:"description=Weapon name from the character inventory; damage dice come from it"`
	DamageDice *string  `json:"damage_dice" agschema:"description=Fallback damage dice if the inventory has none: up to 3 dice with sides 4/6/8/10/12"`
	Session    *Session `json:"-" aginject:"ttrpg.session"`
}

const combatNotStarted = "Бой не начат. Сначала вызови initiative со всеми участниками (партия и враги с HP и КД)."

func (d Deps) combat(sess *Session, args combatArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	unlock := d.lockCampaign(campaign.ID)
	defer unlock()
	snapshot, err := d.DB.LoadCombatState(campaign.Slug)
	if err != nil {
		return nil, fromStoreError(err)
	}
	sheets, err := d.DB.ListCharacters(campaign.ID)
	if err != nil {
		return nil, fromStoreError(err)
	}
	statuses := partyStatuses(sheets)
	if args.Action == "status" {
		if snapshot == nil {
			return combatNotStarted, nil
		}
		return combatStatusText(*snapshot, statuses), nil
	}
	if snapshot == nil || !snapshot.Combat.Started || len(snapshot.Combat.Order) == 0 {
		return nil, errConflict(combatNotStarted, "Call initiative with all participants first.")
	}
	if snapshot.Combat.Current < 0 || snapshot.Combat.Current >= len(snapshot.Combat.Order) {
		return nil, errConflict("Снимок боя повреждён: текущий ход вне порядка инициативы.",
			"Restart combat with initiative.")
	}
	switch args.Action {
	case "end":
		if err := d.DB.ClearCombatState(campaign.Slug); err != nil {
			return nil, fromStoreError(err)
		}
		return "Бой окончен: порядок ходов и враги сброшены.", nil
	case "next":
		advanced := engine.NextCombatant(snapshot.Combat, combatAlive(*snapshot, statuses))
		snapshot.Combat = advanced
		if err := d.DB.SaveCombatState(campaign.Slug, *snapshot); err != nil {
			return nil, fromStoreError(err)
		}
		turn := currentTurnText(advanced)
		if turn == "" {
			return combatNotStarted, nil
		}
		return "Ход переходит дальше. " + turn, nil
	case "damage":
		if args.Target == nil || args.Amount == nil {
			return nil, errInvalid("Для action=damage нужны target и amount.",
				"Pass target (party member) and amount.")
		}
		return d.combatDamage(campaign, snapshot, *args.Target, *args.Amount, args.Heal != nil && *args.Heal, statuses)
	case "dodge":
		if args.Attacker == nil {
			return nil, errInvalid("Для action=dodge нужен attacker.",
				"Pass the current party combatant as attacker.")
		}
		return d.combatDodge(campaign, snapshot, *args.Attacker, sess, statuses)
	case "attack":
		if args.Attacker == nil || args.Enemy == nil {
			return nil, errInvalid("Для action=attack нужны attacker и enemy.",
				"Pass the current party combatant and the enemy id.")
		}
		return d.combatAttack(campaign, snapshot, args, sess, statuses)
	default:
		return nil, errInvalid("Неизвестное действие combat «"+args.Action+"».",
			"Use attack, damage, dodge, next, status or end.")
	}
}

func (d Deps) combatAttack(campaign *store.Campaign, snapshot *store.CombatSnapshot, args combatArgs, sess *Session, statuses map[string]partyHP) (any, error) {
	attackerRef := *args.Attacker
	sheet, err := d.characterByName(campaign.ID, attackerRef)
	if err != nil {
		return nil, err
	}
	if sheet == nil {
		return nil, errInvalid("Персонаж «"+attackerRef+"» не найден в партии.",
			"Use a party character id or name from initiative.")
	}
	if err := d.canActForCharacter(sess, campaign, sheet.Name); err != nil {
		return nil, err
	}
	currentEntry := snapshot.Combat.Order[snapshot.Combat.Current]
	if snapshot.Combat.Acted {
		return nil, errConflict("Участник уже атаковал в этом ходу. Заверши ход через combat action=next.",
			"Call combat action=next before another attack.")
	}
	if !matchesEntry(currentEntry, attackerRef) {
		return nil, errConflict(fmt.Sprintf("Сейчас ход %s — атаковать может только участник, чей ход наступил.", currentEntry.Name),
			"Wait for the attacker's turn or use combat action=next.")
	}
	target, err := findEnemy(snapshot.Enemies, *args.Enemy)
	if err != nil {
		return nil, err
	}
	attackerName := *args.Attacker
	attackStat := "str"
	if args.AttackStat != nil {
		attackStat = *args.AttackStat
	}
	stats := map[string]any{}
	for key, value := range sheet.Stats {
		stats[key] = value
	}
	level := sheet.Level
	if level < 1 {
		level = 1
	}
	environment := engine.Environment{}
	if campaign.TimeOfDay != nil {
		environment.TimeOfDay = string(*campaign.TimeOfDay)
	}
	if campaign.Weather != nil {
		environment.Weather = *campaign.Weather
	}
	weapon := ""
	if args.Weapon != nil {
		weapon = *args.Weapon
	}
	attackType := "melee"
	if isRangedWeapon(weapon) {
		attackType = "ranged"
	}
	env := engine.EnvironmentModifiersForAttack(environment, attackType)
	advantage := engine.CombineAdvantage(env.Advantage)
	rollResult, err := engine.RollDice(20, 1, d.random(), advantage)
	if err != nil {
		return nil, errInternal(err)
	}
	roll := rollResult.Total
	isCrit := roll == 20
	isFumble := roll == 1
	bonus := 0
	if args.Bonus != nil {
		bonus = *args.Bonus
	}
	totalBonus := engine.AbilityModifier(stats, attackStat) + engine.ProficiencyBonus(level) + bonus
	hit := isCrit || (!isFumble && roll+totalBonus >= target.AC)
	fallbackDice := ""
	if args.DamageDice != nil {
		fallbackDice = *args.DamageDice
	}
	diceSpec, diceSource := resolveDamageDice(sheet.Inventory, weapon, fallbackDice)
	count, sides, parseErr := parseDamageDice(diceSpec)
	if parseErr != nil {
		return nil, errInvalid(parseErr.Error(), "Use a damage dice string like 1d8.")
	}
	damage := 0
	if hit {
		damageResult, err := engine.RollDice(sides, count, d.random(), nil)
		if err != nil {
			return nil, errInternal(err)
		}
		damage = damageResult.Total
		if isCrit {
			damage *= 2
		}
	}
	advMark := ""
	if advantage != nil {
		if *advantage {
			advMark = " (преимущество)"
		} else {
			advMark = " (помеха)"
		}
	}
	envNote := ""
	if len(env.Reasons) > 0 {
		envNote = " [" + strings.Join(env.Reasons, "; ") + "]"
	}
	var resultLine string
	defeated := false
	if hit {
		remaining := target.HP - damage
		if remaining < 0 {
			remaining = 0
		}
		defeated = remaining == 0
		hpNote := fmt.Sprintf("У %s осталось %d HP.", target.Name, remaining)
		if defeated {
			hpNote = target.Name + " повержен!"
		}
		critMark := ""
		if isCrit {
			critMark = " — КРИТ!"
		}
		diceNote := " (" + diceSpec + ")"
		if diceSource != "указано" {
			diceNote = " (" + diceSpec + ", " + diceSource + ")"
		}
		resultLine = fmt.Sprintf("%s атакует %s%s%s: d20 = %d%s + %d против КД %d → попадание, урон %d. %s%s",
			attackerName, target.Name, diceNote, advMark, roll, critMark, totalBonus, target.AC, damage, hpNote, envNote)
		target.HP = remaining
	} else {
		fumbleMark := ""
		if isFumble {
			fumbleMark = " (натуральная 1)"
		}
		resultLine = fmt.Sprintf("%s атакует %s%s: d20 = %d + %d против КД %d → промах%s.%s",
			attackerName, target.Name, advMark, roll, totalBonus, target.AC, fumbleMark, envNote)
	}

	enemies := make([]store.SerializedEnemy, 0, len(snapshot.Enemies))
	for _, enemy := range snapshot.Enemies {
		if enemy.ID == target.ID {
			if target.HP <= 0 {
				continue
			}
			enemy.HP = target.HP
		}
		enemies = append(enemies, enemy)
	}
	if hit && len(enemies) == 0 {
		if err := d.DB.ClearCombatState(campaign.Slug); err != nil {
			return nil, fromStoreError(err)
		}
		return resultLine + "\nВсе враги повержены. Бой окончен.", nil
	}
	snapshot.Enemies = enemies
	advanced := engine.NextCombatant(snapshot.Combat, combatAlive(*snapshot, statuses))
	snapshot.Combat = advanced
	if err := d.DB.SaveCombatState(campaign.Slug, *snapshot); err != nil {
		return nil, fromStoreError(err)
	}
	turn := currentTurnText(advanced)
	if turn != "" {
		return resultLine + "\n" + turn, nil
	}
	return resultLine, nil
}

func (d Deps) combatDamage(campaign *store.Campaign, snapshot *store.CombatSnapshot, ref string, amount int, heal bool, statuses map[string]partyHP) (any, error) {
	index := -1
	for i, entry := range snapshot.Combat.Order {
		if entry.Side == engine.SideParty && matchesEntry(entry, ref) {
			index = i
			break
		}
	}
	if index == -1 {
		return nil, errInvalid("Персонаж «"+ref+"» не найден в порядке ходов партии.",
			"Use a party member id or name from initiative.")
	}
	entry := snapshot.Combat.Order[index]
	sheet, err := d.characterByName(campaign.ID, entry.Name)
	if err != nil {
		return nil, err
	}
	if sheet == nil {
		return nil, errNotFound("Персонаж «"+entry.Name+"» не найден в кампании.",
			"Restart initiative so the sheet is loaded.")
	}
	currentHP := sheet.HP
	if currentHP == nil {
		return nil, errConflict("У «"+entry.Name+"» нет HP в листе персонажа.",
			"Set HP with update_character or restart initiative after fixing the sheet.")
	}
	nextHP := *currentHP
	if heal {
		nextHP += amount
	} else {
		nextHP -= amount
		if nextHP < 0 {
			nextHP = 0
		}
	}
	snapshot.Combat.Order[index].HP = &nextHP
	if err := d.DB.ApplyCombatSnapshotHP(campaign.ID, sheet.Name, nextHP, *snapshot); err != nil {
		return nil, fromStoreError(err)
	}
	label := "урон"
	if heal {
		label = "лечение"
	}
	maxHP := "?"
	if state, ok := statuses[strings.ToLower(entry.Name)]; ok && state.MaxHP != nil {
		maxHP = strconv.Itoa(*state.MaxHP)
	}
	fall := ""
	if nextHP == 0 && !heal {
		fall = " Персонаж без сознания!"
	}
	return fmt.Sprintf("%s: %s %d HP → %d/%s.%s", entry.Name, label, amount, nextHP, maxHP, fall), nil
}

func (d Deps) combatDodge(campaign *store.Campaign, snapshot *store.CombatSnapshot, attackerRef string, sess *Session, statuses map[string]partyHP) (any, error) {
	current := snapshot.Combat.Order[snapshot.Combat.Current]
	if current.Side != engine.SideParty {
		return nil, errConflict("Уклоняться может только персонаж партии. Сейчас ход "+current.Name+".",
			"Use dodge only on a party combatant's turn.")
	}
	if !matchesEntry(current, attackerRef) {
		return nil, errConflict(fmt.Sprintf("Сейчас ход %s — Dodge может использовать только участник чей ход наступил.", current.Name),
			"Wait for the combatant's turn or use combat action=next.")
	}
	if err := d.canActForCharacter(sess, campaign, current.Name); err != nil {
		return nil, err
	}
	snapshot.Combat.Order[snapshot.Combat.Current].Dodging = true
	advanced := engine.NextCombatant(snapshot.Combat, combatAlive(*snapshot, statuses))
	snapshot.Combat = advanced
	if err := d.DB.SaveCombatState(campaign.Slug, *snapshot); err != nil {
		return nil, fromStoreError(err)
	}
	base := current.Name + " уклоняется (Dodge): до его следующего хода атаки по нему идут с помехой."
	turn := currentTurnText(advanced)
	if turn == "" {
		return base, nil
	}
	return base + "\n" + turn, nil
}

// partyHP — HP/MaxHP партии из листов SQLite (единственный источник истины).
type partyHP struct {
	HP    *int
	MaxHP *int
}

func partyStatuses(sheets []store.CharacterSheet) map[string]partyHP {
	statuses := make(map[string]partyHP, len(sheets)*2)
	for _, sheet := range sheets {
		state := partyHP{HP: sheet.HP, MaxHP: sheet.MaxHP}
		statuses[sheet.ID] = state
		statuses[strings.ToLower(sheet.Name)] = state
	}
	return statuses
}

func statusFor(entry engine.CombatantEntry, statuses map[string]partyHP) (partyHP, bool) {
	if state, ok := statuses[entry.ID]; ok {
		return state, true
	}
	state, ok := statuses[strings.ToLower(entry.Name)]
	return state, ok
}

func combatAlive(snapshot store.CombatSnapshot, statuses map[string]partyHP) func(engine.CombatantEntry) bool {
	return func(entry engine.CombatantEntry) bool {
		if entry.Side == engine.SideEnemy {
			for _, enemy := range snapshot.Enemies {
				if enemy.ID == entry.ID {
					return true
				}
			}
			return false
		}
		if state, ok := statusFor(entry, statuses); ok && state.HP != nil {
			return *state.HP > 0
		}
		return entry.HP == nil || *entry.HP > 0
	}
}

func combatStatusText(snapshot store.CombatSnapshot, statuses map[string]partyHP) string {
	combat := snapshot.Combat
	if !combat.Started || len(combat.Order) == 0 {
		return combatNotStarted
	}
	lines := make([]string, 0, len(combat.Order))
	for index, entry := range combat.Order {
		marker := "  "
		if index == combat.Current {
			if combat.Acted {
				marker = "→ ✓"
			} else {
				marker = "→"
			}
		}
		dodge := ""
		if entry.Dodging {
			dodge = " [уклоняется]"
		}
		lines = append(lines, fmt.Sprintf("%s%d. %s [%s] (%d)%s%s", marker, index+1, entry.Name, entry.ID, entry.Total, hpNote(entry, snapshot, statuses), dodge))
	}
	parts := append([]string{"Порядок ходов:"}, lines...)
	if turn := currentTurnText(combat); turn != "" {
		parts = append(parts, turn)
	}
	return strings.Join(parts, "\n")
}

func currentTurnText(combat engine.CombatOrder) string {
	if !combat.Started || combat.Current < 0 || combat.Current >= len(combat.Order) {
		return ""
	}
	entry := combat.Order[combat.Current]
	dodge := ""
	if entry.Dodging {
		dodge = " [уклоняется]"
	}
	return fmt.Sprintf("Раунд %d. Ход: %s (%d)%s.", combat.Round, entry.Name, entry.Total, dodge)
}

func hpNote(entry engine.CombatantEntry, snapshot store.CombatSnapshot, statuses map[string]partyHP) string {
	if entry.Side == engine.SideEnemy {
		for _, enemy := range snapshot.Enemies {
			if enemy.ID == entry.ID {
				return fmt.Sprintf(", HP %d, КД %d", enemy.HP, enemy.AC)
			}
		}
		return ""
	}
	if state, ok := statusFor(entry, statuses); ok && state.HP != nil {
		maxHP := "?"
		if state.MaxHP != nil {
			maxHP = strconv.Itoa(*state.MaxHP)
		}
		return fmt.Sprintf(", HP %d/%s", *state.HP, maxHP)
	}
	return ""
}

func matchesEntry(entry engine.CombatantEntry, ref string) bool {
	return entry.ID == ref || strings.EqualFold(strings.TrimSpace(entry.Name), strings.TrimSpace(ref))
}

func findEnemy(enemies []store.SerializedEnemy, ref string) (*store.SerializedEnemy, error) {
	for index := range enemies {
		if enemies[index].ID == ref {
			return &enemies[index], nil
		}
	}
	needle := strings.ToLower(strings.TrimSpace(ref))
	var matches []*store.SerializedEnemy
	for index := range enemies {
		if strings.ToLower(strings.TrimSpace(enemies[index].Name)) == needle {
			matches = append(matches, &enemies[index])
		}
	}
	switch len(matches) {
	case 0:
		active := make([]string, 0, len(enemies))
		for _, enemy := range enemies {
			active = append(active, fmt.Sprintf("%s [%s] (%d HP, КД %d)", enemy.Name, enemy.ID, enemy.HP, enemy.AC))
		}
		return nil, errNotFound("Нет врага «"+ref+"». Активные враги: "+strings.Join(active, ", ")+".",
			"Use an enemy id from the initiative result.")
	case 1:
		return matches[0], nil
	default:
		active := make([]string, 0, len(enemies))
		for _, enemy := range enemies {
			active = append(active, fmt.Sprintf("%s [%s] (HP %d, КД %d)", enemy.Name, enemy.ID, enemy.HP, enemy.AC))
		}
		return nil, errInvalid("Несколько врагов подходят под «"+ref+"». Уточни цель по id: "+strings.Join(active, ", ")+".",
			"Pass the exact enemy id from initiative.")
	}
}

var rangedWeaponHints = []string{"лук", "арбалет", "праща", "дротик", "bow", "crossbow", "sling", "dart", "blowgun"}

func isRangedWeapon(weapon string) bool {
	lower := strings.ToLower(weapon)
	for _, hint := range rangedWeaponHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func resolveDamageDice(inventory []string, weapon, damageDice string) (string, string) {
	if weapon != "" {
		if spec, ok := engine.WeaponDamageDice(inventory, weapon); ok {
			return spec, "оружие «" + weapon + "»"
		}
	}
	if spec, ok := engine.WeaponDamageDice(inventory, ""); ok {
		return spec, "инвентарь"
	}
	if damageDice != "" {
		return damageDice, "указано"
	}
	return "1d4", "без оружия (1d4)"
}

var damageDicePattern = regexp.MustCompile(`(?i)^(\d+)d(\d+)$`)

func parseDamageDice(spec string) (int, int, error) {
	match := damageDicePattern.FindStringSubmatch(strings.TrimSpace(spec))
	if match == nil {
		return 0, 0, fmt.Errorf("Недопустимый урон %q, ожидалось вида \"1d8\".", spec)
	}
	count, _ := strconv.Atoi(match[1])
	sides, _ := strconv.Atoi(match[2])
	if count < 1 || count > 3 || (sides != 4 && sides != 6 && sides != 8 && sides != 10 && sides != 12) {
		return 0, 0, fmt.Errorf("Недопустимый урон %q: максимум 3 кости, грани 4/6/8/10/12.", spec)
	}
	return count, sides, nil
}

// --- level_up ---

type levelUpAbilityArg struct {
	Name        string `json:"name" agschema:"description=New ability or spell name"`
	Description string `json:"description" agschema:"description=What it does and how it is used"`
}

type levelUpArgs struct {
	Character string             `json:"character" agschema:"description=Character name or slug"`
	Level     *int               `json:"level" agschema:"description=New level; defaults to the level for accumulated XP,min=2,max=20"`
	Ability   *levelUpAbilityArg `json:"ability" agschema:"description=New ability gained at this level"`
	Session   *Session           `json:"-" aginject:"ttrpg.session"`
}

func (d Deps) levelUp(sess *Session, args levelUpArgs) (any, error) {
	campaign, err := d.campaign(sess)
	if err != nil {
		return nil, err
	}
	sheet, err := d.characterByName(campaign.ID, args.Character)
	if err != nil {
		return nil, err
	}
	if sheet == nil {
		return nil, errNotFound("Персонаж «"+args.Character+"» не найден в кампании.",
			"Check the character name with get_character and retry.")
	}
	oldLevel := sheet.Level
	if oldLevel < 1 {
		oldLevel = 1
	}
	xp := 0
	if sheet.XP != nil {
		xp = *sheet.XP
	}
	newLevel := engine.LevelForXP(xp)
	if args.Level != nil {
		newLevel = *args.Level
	}
	if newLevel <= oldLevel {
		return nil, errConflict(fmt.Sprintf("Персонаж «%s» уже на уровне %d — повышение до %d невозможно.", sheet.Name, oldLevel, newLevel),
			"Grant XP or pass a higher level explicitly.")
	}
	delta := newLevel - oldLevel
	perLevel := engine.ClassHitDieFor(sheet.CharacterClass) + constitutionModifier(sheet.Stats)
	var newMaxHP *int
	var hpGain any
	if sheet.MaxHP != nil {
		value := *sheet.MaxHP + perLevel*delta
		newMaxHP = &value
		hpGain = perLevel * delta
	}
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	var ability *store.CharacterAbility
	gained := any(nil)
	if args.Ability != nil {
		level := newLevel
		ability = &store.CharacterAbility{Name: args.Ability.Name, Description: args.Ability.Description, Level: &level}
		gained = args.Ability.Name
	}
	// Уровень, способность, ключевое событие и транскрипт — одна транзакция:
	// сбой посередине не оставит полуповышенного персонажа.
	eventID := fmt.Sprintf("levelup:%s:%d", sheet.ID, newLevel)
	text := fmt.Sprintf("%s достиг %d уровня.", sheet.Name, newLevel)
	if _, err := d.DB.LevelUpCharacter(campaign.ID, sheet.Name, day, store.LevelUpInput{
		NewLevel: newLevel, MaxHP: newMaxHP, Ability: ability,
		KeyEventText: text, KeyEventID: eventID, TranscriptText: text,
	}); err != nil {
		return nil, fromStoreError(err)
	}
	return map[string]any{
		"ok": true,
		"character": map[string]any{
			"name": sheet.Name, "level": newLevel, "maxHp": nullableInt(newMaxHP),
			"gainedAbility": gained, "hpGain": hpGain,
		},
		"note": "Уровень повышен. Объяви игрокам что персонаж получил новый уровень и опиши новую способность в игре.",
	}, nil
}

func constitutionModifier(stats map[string]float64) int {
	for _, key := range []string{"constitution", "con", "выносливость", "телосложение", "CON"} {
		if value, ok := stats[key]; ok {
			return engine.StatModifier(int(value))
		}
	}
	return 0
}
