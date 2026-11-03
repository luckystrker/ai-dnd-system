package tools

import (
	"fmt"
	"sort"

	agotools "github.com/luckystrker/ago/tools"
)

// ChroniclerTools — подмножество тулов летописца (как 25 файлов в
// agent/subagents/chronicler/tools). Одна реализация с DM, без копий.
var ChroniclerTools = []string{
	"adjust_standing",
	"advance_time",
	"append_campaign_summary",
	"append_key_event",
	"append_thread",
	"complete_quest",
	"get_npc",
	"level_up",
	"list_ledger",
	"list_locations",
	"list_npcs",
	"list_open_threads",
	"list_quests",
	"list_world_state",
	"move_party",
	"read_day",
	"record_world_change",
	"resolve_thread",
	"set_weather",
	"update_character",
	"update_day_summary",
	"update_quest",
	"upsert_faction",
	"upsert_location",
	"upsert_npc",
}

// DMToolkit — все 43 тула DM.
func DMToolkit(d Deps) (*agotools.Toolkit, error) {
	return buildToolkit("dm", d, nil)
}

// ChroniclerToolkit — только тулы летописца.
func ChroniclerToolkit(d Deps) (*agotools.Toolkit, error) {
	allowed := make(map[string]bool, len(ChroniclerTools))
	for _, name := range ChroniclerTools {
		allowed[name] = true
	}
	return buildToolkit("chronicler", d, allowed)
}

// AllToolNames — имена всех тулов (для тестов и диагностики).
func AllToolNames(d Deps) ([]string, error) {
	all, err := buildAll(d.withDefaults())
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func buildToolkit(name string, d Deps, allowed map[string]bool) (*agotools.Toolkit, error) {
	d = d.withDefaults()
	all, err := buildAll(d)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(all))
	for toolName := range all {
		if allowed == nil || allowed[toolName] {
			names = append(names, toolName)
		}
	}
	sort.Strings(names)
	members := make([]agotools.Tool, 0, len(names))
	for _, toolName := range names {
		members = append(members, all[toolName])
	}
	return agotools.NewToolkit(name, nil, members...)
}

func buildAll(d Deps) (map[string]agotools.Tool, error) {
	groups := []func(Deps) ([]namedTool, error){
		campaignTools, memoryTools, environmentTools, diceTools, tableTools,
	}
	out := map[string]agotools.Tool{}
	for _, group := range groups {
		toolsList, err := group(d)
		if err != nil {
			return nil, err
		}
		for _, item := range toolsList {
			if _, duplicate := out[item.name]; duplicate {
				return nil, fmt.Errorf("tools: duplicate tool %q", item.name)
			}
			out[item.name] = item.tool
		}
	}
	illustration, err := illustrateTool(d)
	if err != nil {
		return nil, err
	}
	out[illustration.Name()] = illustration
	return out, nil
}
