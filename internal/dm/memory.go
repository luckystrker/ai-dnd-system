package dm

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/luckystriker/ai-dnd-system/internal/store"
	"github.com/luckystriker/ai-dnd-system/internal/tools"
)

// Капы памяти кампании — 1:1 с agent/instructions/campaign-context.ts.
const (
	summaryCap        = 2000
	keyEventsCap      = 2000
	headlinesCap      = 2500
	dayTailLines      = 30
	npcRosterCap      = 20
	activeQuestsCap   = 10
	openThreadsCap    = 10
	worldStateCap     = 1500
	factionRosterCap  = 15
	mapConnectionsCap = 10
	npcMemoryChars    = 100
)

var activeQuestStatuses = map[store.QuestStatus]bool{
	store.QuestOffered: true, store.QuestAccepted: true, store.QuestActive: true,
}

var difficultyLabel = map[store.QuestDifficulty]string{
	store.QuestEasy: "лёгкий", store.QuestMedium: "средний", store.QuestHard: "сложный",
}

var threadKindLabel = map[store.ThreadKind]string{
	store.ThreadPromise: "обещание", store.ThreadMystery: "тайна",
	store.ThreadDebt: "долг", store.ThreadUnresolved: "незавершённое",
}

var timeOfDayLabel = map[store.TimeOfDay]string{
	store.TimeMorning: "утро", store.TimeDay: "день",
	store.TimeEvening: "вечер", store.TimeNight: "ночь",
}

// BuildMemoryBlock собирает блок памяти кампании для хвоста контекста.
// visible — id записей транскрипта, уже видимых модели в окне истории: их
// повторно не вставляем. Пустая карта (нет сессии, кампании или ошибка
// чтения) означает «память не добавлять»: игра важнее.
func BuildMemoryBlock(st *store.Store, sess *tools.Session, visible map[string]bool) string {
	if st == nil || sess == nil {
		return ""
	}
	key := sess.CampaignID
	if key == "" {
		key = sess.CampaignSlug
	}
	if key == "" {
		return ""
	}
	campaign, err := st.GetCampaign(key)
	if err != nil || campaign == nil {
		return ""
	}
	characters, err := st.ListCharacters(campaign.ID)
	if err != nil {
		return ""
	}
	block, err := buildMemoryBlock(st, campaign, sess, characters, visible)
	if err != nil {
		return ""
	}
	return block
}

func buildMemoryBlock(st *store.Store, campaign *store.Campaign, sess *tools.Session, characters []store.CharacterSheet, visible map[string]bool) (string, error) {
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	meta := []string{
		"Сеттинг: " + campaign.Setting,
		"Тема: " + campaign.Theme,
	}
	if campaign.Goal != nil && *campaign.Goal != "" {
		meta = append(meta, "Цель: "+*campaign.Goal)
	}
	if campaign.Tone != nil && *campaign.Tone != "" {
		meta = append(meta, "Тон: "+*campaign.Tone)
	}
	if env := environmentLine(campaign, day); env != "" {
		meta = append(meta, env)
	}

	sections := []string{
		fmt.Sprintf("## Память кампании «%s» (игровой день %d)", campaign.Title, day),
		strings.Join(meta, "; "),
	}
	if speaker := speakerSection(sess, characters); speaker != "" {
		sections = append(sections, speaker)
	}

	headlines, err := st.ListDayHeadlines(campaign.Slug)
	if err != nil {
		return "", err
	}
	if len(headlines) > 0 {
		lines := make([]string, 0, len(headlines))
		for _, item := range headlines {
			lines = append(lines, fmt.Sprintf("- День %d: %s", item.Day, item.Headline))
		}
		sections = append(sections, "### Хроника (по дням)\n"+capTail(strings.Join(lines, "\n"), headlinesCap))
	}

	summary, err := st.ReadCampaignSummary(campaign.Slug)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(summary) != "" {
		sections = append(sections, "### Хроника прошлых дней (детально)\n"+capTail(summary, summaryCap))
	}

	permanent, regular, err := st.SplitKeyEvents(campaign.Slug)
	if err != nil {
		return "", err
	}
	if len(permanent) > 0 || len(regular) > 0 {
		parts := []string{}
		if len(permanent) > 0 {
			parts = append(parts, "Важное (всегда):")
			parts = append(parts, permanent...)
		}
		if len(regular) > 0 {
			parts = append(parts, "События:")
			parts = append(parts, capTail(strings.Join(regular, "\n"), keyEventsCap))
		}
		sections = append(sections, "### Ключевые события\n"+strings.Join(parts, "\n"))
	}

	world, err := st.RenderWorldState(campaign.Slug)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(world) != "" {
		sections = append(sections, "### Состояние мира\n"+capTail(world, worldStateCap))
	}

	quests, err := st.ListQuests(campaign.ID)
	if err != nil {
		return "", err
	}
	sections = append(sections, activeQuestsSection(quests))

	threads, err := st.ListThreads(campaign.ID)
	if err != nil {
		return "", err
	}
	sections = append(sections, openThreadsSection(threads))

	factions, err := st.ListFactions(campaign.ID)
	if err != nil {
		return "", err
	}
	if factionsSection := factionsSection(factions); factionsSection != "" {
		sections = append(sections, factionsSection)
	}

	current, err := st.CurrentLocation(campaign.ID)
	if err != nil {
		return "", err
	}
	if mapSection := mapSection(current); mapSection != "" {
		sections = append(sections, mapSection)
	}

	record, err := st.ReadDayTail(campaign.Slug, day, dayTailLines)
	if err != nil {
		return "", err
	}
	if tail := dayTailSection(record, visible); tail != "" {
		sections = append(sections, tail)
	}

	npcs, err := st.ListNpcs(campaign.ID)
	if err != nil {
		return "", err
	}
	sections = append(sections, "### NPC\n"+npcRoster(st, npcs, campaign.ID))
	sections = append(sections, "### Партия\n"+partyState(campaign, characters))
	sections = append(sections, toolHint)
	return strings.Join(sections, "\n\n"), nil
}

const toolHint = "Полный транскрипт дня — через read_day, карточка NPC — через get_npc. " +
	"Квесты и нити — через list_quests / list_open_threads. " +
	"Локации — list_locations, факты мира — list_world_state, " +
	"журнал лута — list_ledger. " +
	"Если нужен факт из прошлого, а день неизвестен — search_memory по ключевым словам. " +
	"Изменения состояния фиксируй через update_character / upsert_npc; новый день — advance_day; " +
	"время суток — advance_time; погода — set_weather; " +
	"перемещение партии — move_party; фракции — upsert_faction / adjust_standing."

// sessionStartMarker — явный маркер начала сессии в хвосте блока памяти.
// Правило recap в промпте срабатывает только по нему: модель не угадывает
// паузу по тексту сообщения игрока.
func sessionStartMarker(gap time.Duration) string {
	pause := "первый ход в этом чате"
	if gap > 0 {
		pause = fmt.Sprintf("пауза %d ч", int(gap.Hours()))
	}
	return "### Начало новой сессии (" + pause + ")\n" +
		"Первым делом вызови get_game_context. Первый абзац ответа — пересказ в 2–3 предложения: " +
		"где партия, чем занималась и какая цель активна (если цели нет — опиши текущее занятие, " +
		"не выдумывай её). Не начинай с погоды, дороги или описания места; после пересказа — " +
		"не больше двух коротких знаков, что мир изменился, и продолжай сцену."
}

func activeQuestsSection(quests []store.Quest) string {
	lines := []string{}
	for _, quest := range quests {
		if !activeQuestStatuses[quest.Status] {
			continue
		}
		parts := []string{labelOr(difficultyLabel[quest.Difficulty], string(quest.Difficulty))}
		if quest.GiverNpcSlug != nil && *quest.GiverNpcSlug != "" {
			parts = append(parts, "от: "+*quest.GiverNpcSlug)
		}
		if quest.DeadlineDay != nil {
			parts = append(parts, fmt.Sprintf("дедлайн: день %d", *quest.DeadlineDay))
		}
		lines = append(lines, fmt.Sprintf("- %s: «%s» — %s (%s)",
			quest.Status, quest.Title, quest.Objective, strings.Join(parts, "; ")))
		if len(lines) >= activeQuestsCap {
			break
		}
	}
	if len(lines) == 0 {
		return "### Активные квесты\n(нет активных квестов)"
	}
	return "### Активные квесты\n" + strings.Join(lines, "\n")
}

func labelOr(label, fallback string) string {
	if label != "" {
		return label
	}
	return fallback
}

func openThreadsSection(threads []store.OpenThread) string {
	lines := []string{}
	for _, thread := range threads {
		if thread.Status != "open" {
			continue
		}
		kind := labelOr(threadKindLabel[thread.Kind], string(thread.Kind))
		lines = append(lines, fmt.Sprintf("- [%s] %s (открыта в день %d)", kind, thread.Text, thread.DayOpened))
		if len(lines) >= openThreadsCap {
			break
		}
	}
	if len(lines) == 0 {
		return "### Открытые нити\n(нет)"
	}
	return "### Открытые нити\n" + strings.Join(lines, "\n")
}

func factionsSection(factions []store.Faction) string {
	if len(factions) == 0 {
		return ""
	}
	lines := []string{}
	for _, faction := range factions {
		standing := fmt.Sprintf("%d", faction.Standing)
		if faction.Standing > 0 {
			standing = "+" + standing
		}
		lines = append(lines, fmt.Sprintf("- %s — репутация %s", faction.Name, standing))
		if len(lines) >= factionRosterCap {
			break
		}
	}
	if len(factions) > factionRosterCap {
		lines = append(lines, fmt.Sprintf("- …и ещё %d", len(factions)-factionRosterCap))
	}
	return "### Фракции\n" + strings.Join(lines, "\n")
}

func mapSection(current *store.Location) string {
	if current == nil {
		return ""
	}
	lines := []string{"Сейчас: " + current.Name}
	if len(current.Connections) > 0 {
		connections := []string{}
		for _, conn := range current.Connections {
			via := ""
			if conn.Via != nil && *conn.Via != "" {
				via = " через " + *conn.Via
			}
			connections = append(connections, conn.To+via)
			if len(connections) >= mapConnectionsCap {
				break
			}
		}
		lines = append(lines, "Отсюда можно пройти: "+strings.Join(connections, "; "))
	}
	if len(current.Connections) > mapConnectionsCap {
		lines = append(lines, fmt.Sprintf("…и ещё %d (list_locations)", len(current.Connections)-mapConnectionsCap))
	}
	return "### Карта\n" + strings.Join(lines, "\n")
}

func environmentLine(campaign *store.Campaign, day int) string {
	parts := []string{}
	if campaign.TimeOfDay != nil {
		if label := timeOfDayLabel[*campaign.TimeOfDay]; label != "" {
			parts = append(parts, label)
		}
	}
	date := fmt.Sprintf("день %d", day)
	if campaign.InGameDate != nil && *campaign.InGameDate != "" {
		date = *campaign.InGameDate
	}
	parts = append(parts, date)
	if campaign.Weather != nil && *campaign.Weather != "" {
		parts = append(parts, *campaign.Weather)
	}
	return "Игровое время: " + strings.Join(parts, ", ")
}

func speakerSection(sess *tools.Session, characters []store.CharacterSheet) string {
	if sess == nil || sess.ChatType == "" || sess.ChatType == "private" {
		return ""
	}
	speaker := sess.UserID
	if sess.Username != "" {
		speaker = "@" + sess.Username
	}
	lines := []string{"Сейчас пишет: " + speaker}
	own := []string{}
	others := []string{}
	for _, sheet := range characters {
		if sheet.OwnerUserID == sess.UserID {
			own = append(own, sheet.Name)
		} else {
			others = append(others, sheet.Name)
		}
	}
	if len(own) > 0 {
		lines = append(lines, fmt.Sprintf("Персонаж этого игрока: %s — он управляет только %s.",
			strings.Join(own, ", "), plural(own)))
	}
	if len(others) > 0 {
		lines = append(lines, "Не пиши, не действуй и не решай за: "+strings.Join(others, ", ")+" — это персонажи других игроков.")
	}
	return strings.Join(lines, "\n")
}

func plural(names []string) string {
	if len(names) > 1 {
		return "ими"
	}
	return "им"
}

func npcRoster(st *store.Store, npcs []store.NpcProfile, campaignID string) string {
	if len(npcs) == 0 {
		return "(NPC пока не заведены)"
	}
	lines := []string{}
	for _, npc := range npcs {
		parts := []string{labelOr(deref(npc.Role), "NPC"), string(npc.Status)}
		if npc.Location != nil && *npc.Location != "" {
			parts = append(parts, "локация: "+*npc.Location)
		}
		if relations := relationshipsLine(npc.Relationships); relations != "" {
			parts = append(parts, "отношения: "+relations)
		}
		memory, err := st.LastMemoryLine(campaignID, npc.Slug, npcMemoryChars)
		if err == nil && memory != "" {
			parts = append(parts, "помнит: "+memory)
		}
		lines = append(lines, "- "+npc.Name+" — "+strings.Join(parts, "; "))
		if len(lines) >= npcRosterCap {
			break
		}
	}
	if len(npcs) > npcRosterCap {
		lines = append(lines, fmt.Sprintf("- …и ещё %d (list_npcs / get_npc)", len(npcs)-npcRosterCap))
	}
	return strings.Join(lines, "\n")
}

func relationshipsLine(relationships map[string]store.NpcRelationship) string {
	if len(relationships) == 0 {
		return ""
	}
	names := make([]string, 0, len(relationships))
	for name := range relationships {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		attitude := relationships[name].Attitude
		sign := ""
		if attitude > 0 {
			sign = "+"
		}
		parts = append(parts, fmt.Sprintf("%s: %s%d", name, sign, attitude))
	}
	return strings.Join(parts, ", ")
}

func partyState(campaign *store.Campaign, characters []store.CharacterSheet) string {
	if len(characters) == 0 {
		return "(персонажи ещё не созданы)"
	}
	lines := []string{}
	for _, sheet := range characters {
		parts := []string{fmt.Sprintf("%s, ур. %d", sheet.CharacterClass, sheet.Level)}
		if sheet.HP != nil || sheet.MaxHP != nil {
			parts = append(parts, fmt.Sprintf("HP %s/%s", intOrQuestion(sheet.HP), intOrQuestion(sheet.MaxHP)))
		}
		if len(sheet.Conditions) > 0 {
			parts = append(parts, "состояния: "+strings.Join(sheet.Conditions, ", "))
		}
		if sheet.Gold != nil {
			parts = append(parts, fmt.Sprintf("золото: %d", *sheet.Gold))
		}
		if sheet.Location != nil && *sheet.Location != "" {
			parts = append(parts, "локация: "+*sheet.Location)
		}
		if owner := memberLabel(campaign, sheet.OwnerUserID); owner != "" {
			parts = append(parts, "играет "+owner)
		}
		lines = append(lines, fmt.Sprintf("- %s (%s)", sheet.Name, strings.Join(parts, "; ")))
	}
	return strings.Join(lines, "\n")
}

func memberLabel(campaign *store.Campaign, userID string) string {
	for _, member := range campaign.Members {
		if member.UserID != userID {
			continue
		}
		if member.Username != nil && *member.Username != "" {
			return "@" + *member.Username
		}
		if member.Name != nil && *member.Name != "" {
			return *member.Name
		}
		return member.UserID
	}
	return ""
}

func dayTailSection(record *store.DayRecord, visible map[string]bool) string {
	if record == nil {
		return ""
	}
	parts := []string{}
	if record.Headline != nil && *record.Headline != "" {
		parts = append(parts, "Шапка дня: "+*record.Headline)
	}
	if record.Summary != nil && *record.Summary != "" {
		parts = append(parts, "Саммари дня: "+*record.Summary)
	}
	entries := []string{}
	for _, entry := range record.Entries {
		// Не дублируем запись, если её ход уже виден модели в окне истории.
		// Сравнение по стабильному id записи, а не по подстроке текста:
		// короткая реплика («ок») не должна совпадать с «окно».
		if eventID := transcriptEventID(entry); eventID != "" && visible[eventID] {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) > 0 {
		parts = append(parts, fmt.Sprintf("Последние события (последние %d записей транскрипта):", len(entries)))
		parts = append(parts, entries...)
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("### Текущий день %d\n%s", record.Day, strings.Join(parts, "\n"))
}

// transcriptEventID вытаскивает id записи из маркера "<!-- evt:... -->".
func transcriptEventID(line string) string {
	index := strings.LastIndex(line, "<!-- evt:")
	if index < 0 {
		return ""
	}
	rest := line[index+len("<!-- evt:"):]
	end := strings.Index(rest, " -->")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func capTail(text string, max int) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return "…(начало опущено)\n" + string(runes[len(runes)-max:])
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intOrQuestion(value *int) string {
	if value == nil {
		return "?"
	}
	return fmt.Sprintf("%d", *value)
}
