// Команда dump-sessions — порт scripts/dump-sessions.ts: печать переписки из
// durable-потоков event_streams и транскрипта кампаний. eve-трейсов больше
// нет, поэтому источник — SQLite-база бота.
//
// Запуск:
//
//	go run ./cmd/dump-sessions                 # все чаты
//	go run ./cmd/dump-sessions vasya           # по username/имени/user id
//	go run ./cmd/dump-sessions 123456789       # по chat id
//	go run ./cmd/dump-sessions --all           # совместимость: и так все чаты
//	go run ./cmd/dump-sessions -db data/campaigns.db
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/luckystrker/ago/events"

	"github.com/luckystriker/ai-dnd-system/internal/store"
	"github.com/luckystriker/ai-dnd-system/internal/telegram"
)

func main() {
	fs := flag.NewFlagSet("dump-sessions", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "путь к SQLite-базе кампаний")
	_ = fs.Bool("all", false, "оставлено для совместимости: выводятся все чаты")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	filter := strings.ToLower(strings.TrimSpace(strings.Join(fs.Args(), " ")))

	st, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dump-sessions: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	if err := dump(st, filter); err != nil {
		fmt.Fprintf(os.Stderr, "dump-sessions: %v\n", err)
		os.Exit(1)
	}
}

func defaultDBPath() string {
	if path := strings.TrimSpace(os.Getenv("CAMPAIGN_DB_PATH")); path != "" {
		return path
	}
	return "data/campaigns.db"
}

func dump(st *store.Store, filter string) error {
	campaigns, err := st.ListCampaigns()
	if err != nil {
		return err
	}
	streams, err := store.NewEventStreamStorage(st).ListStreamIDs()
	if err != nil {
		return err
	}

	// Кампании по фильтру: username/имя/user id участника или chat id привязки.
	type match struct {
		campaign *store.Campaign
		streams  []string
	}
	var matched []match
	for index := range campaigns {
		campaign := &campaigns[index]
		if filter != "" && !campaignMatches(campaign, filter) {
			continue
		}
		matched = append(matched, match{campaign: campaign, streams: campaignStreams(campaign, streams)})
	}

	// Потоки без кампании (например, чат до создания кампании): фильтр по
	// stream_id или показ всех при пустом фильтре.
	known := map[string]bool{}
	for _, item := range matched {
		for _, id := range item.streams {
			known[id] = true
		}
	}
	var orphan []string
	for _, id := range streams {
		if known[id] {
			continue
		}
		if filter == "" || strings.Contains(strings.ToLower(id), filter) {
			orphan = append(orphan, id)
		}
	}

	if len(matched) == 0 && len(orphan) == 0 {
		fmt.Printf("Ничего не найдено.%s\n", filterHint(filter))
		return nil
	}

	for _, item := range matched {
		campaign := item.campaign
		chatID, thread := "", ""
		if campaign.BoundChat != nil {
			chatID = campaign.BoundChat.ChatID
			if campaign.BoundChat.MessageThreadID != nil {
				thread = fmt.Sprintf(", тема %d", *campaign.BoundChat.MessageThreadID)
			}
		}
		fmt.Printf("\n==== кампания «%s» (%s) | chat_id %s%s | %s ====\n",
			campaign.Title, campaign.Slug, chatID, thread, campaign.Status)
		members := make([]string, 0, len(campaign.Members))
		for _, member := range campaign.Members {
			who := member.UserID
			if member.Username != nil && *member.Username != "" {
				who = "@" + *member.Username
			} else if member.Name != nil && *member.Name != "" {
				who = *member.Name
			}
			members = append(members, fmt.Sprintf("%s (%s)", who, member.Role))
		}
		if len(members) > 0 {
			fmt.Printf("участники: %s\n", strings.Join(members, ", "))
		}
		if err := dumpTranscript(st, campaign); err != nil {
			return err
		}
		for _, streamID := range item.streams {
			if err := dumpStream(st, streamID); err != nil {
				return err
			}
		}
	}

	if len(orphan) > 0 {
		fmt.Println("\n==== потоки без кампании ====")
		for _, streamID := range orphan {
			if err := dumpStream(st, streamID); err != nil {
				return err
			}
		}
	}
	return nil
}

func filterHint(filter string) string {
	if filter == "" {
		return ""
	}
	return fmt.Sprintf(" Фильтр: %q. Попробуйте username, имя, user id или chat id, либо --all.", filter)
}

func campaignMatches(campaign *store.Campaign, filter string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		campaign.Title, campaign.Slug, campaign.OwnerUserID,
	}, " "))
	if campaign.BoundChat != nil {
		haystack += " " + strings.ToLower(campaign.BoundChat.ChatID)
	}
	for _, member := range campaign.Members {
		haystack += " " + strings.ToLower(member.UserID)
		if member.Username != nil {
			haystack += " " + strings.ToLower(*member.Username)
		}
		if member.Name != nil {
			haystack += " " + strings.ToLower(*member.Name)
		}
	}
	return strings.Contains(haystack, filter)
}

// campaignStreams — потоки чата и его топиков: tg:<chat>:<topic>.
func campaignStreams(campaign *store.Campaign, streams []string) []string {
	if campaign.BoundChat == nil {
		return nil
	}
	prefix := telegram.StreamID(campaign.BoundChat.ChatID + ":")
	var out []string
	for _, id := range streams {
		if strings.HasPrefix(id, prefix) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func dumpTranscript(st *store.Store, campaign *store.Campaign) error {
	day := 1
	if campaign.CurrentDay != nil {
		day = *campaign.CurrentDay
	}
	for d := 1; d <= day; d++ {
		record, err := st.ReadDay(campaign.Slug, d)
		if err != nil {
			return err
		}
		if record == nil || len(record.Entries) == 0 {
			continue
		}
		fmt.Printf("\n-- день %d --\n", d)
		for _, line := range record.Entries {
			fmt.Println(line)
		}
	}
	return nil
}

func dumpStream(st *store.Store, streamID string) error {
	history, err := store.NewEventStreamStorage(st).GetHistory(streamID)
	if err != nil {
		return err
	}
	if len(history) == 0 {
		return nil
	}
	fmt.Printf("\n-- поток %s --\n", streamID)
	for _, ev := range history {
		switch item := events.AsValue(ev).(type) {
		case events.ModelRequest:
			if item.Metadata["kind"] == "campaign_memory" {
				continue
			}
			printMessage(at(ev), "игрок", renderParts(item.Parts))
		case events.DrainedModelRequest:
			printMessage(at(ev), "игрок", renderParts(item.Parts))
		case events.ModelResponse:
			printMessage(at(ev), "бот", item.Content())
		}
	}
	return nil
}

func printMessage(timestamp time.Time, who, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	clock := ""
	if !timestamp.IsZero() {
		clock = timestamp.Local().Format("2006-01-02 15:04")
	}
	fmt.Printf("[%s] %s: %s\n", clock, who, strings.ReplaceAll(text, "\n", "\n    "))
}

func renderParts(parts []events.Input) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, events.RenderForPrompt(part))
	}
	return strings.Join(out, "\n")
}

func at(ev events.Event) time.Time { return ev.At() }
