package tools

import (
	"encoding/json"
	"testing"
)

func TestRestoredDescriptionsMatchTS(t *testing.T) {
	dm, err := DMToolkit(Deps{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	descriptions := map[string]string{}
	for _, item := range dm.Tools() {
		schemas, err := item.Schemas(nil)
		if err != nil {
			t.Fatalf("%s: %v", item.Name(), err)
		}
		data, err := json.Marshal(schemas)
		if err != nil {
			t.Fatal(err)
		}
		var decoded []map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded) > 0 {
			descriptions[item.Name()], _ = decoded[0]["description"].(string)
		}
	}

	expected := map[string]string{
		"roll_dice":           "Roll dice using standard notation (e.g. '4d20', '2d6+1d8+3', '1d20+5', 'd8'). Use for any uncertain game outcome: enemy attacks vs a player's AC, random tables, damage outside the combat tool, loot, etc. Advantage/disadvantage applies to d20 dice ONLY (each d20 is rolled twice, keeping the higher for advantage or the lower for disadvantage). Group modifiers: khN/klN keep the highest/lowest N (e.g. '4d6kh3' rolls 4d6 and keeps the 3 highest), dhN/dlN drop the highest/lowest N (e.g. '4d6dl1' drops the lowest, same as keep-highest-3 — use for ability score generation), and '!' rolls exploding dice (a die on its max face adds another, e.g. '8d6!'). Never use invented results.",
		"append_key_event":    "Save one key moment of the campaign into long-term memory. Use for turning points, promises, betrayals, discovered secrets — facts the DM must remember sessions later. One call = one concise event. Set permanent=true for the few pivotal facts (deaths, alliances, revealed secrets, oaths) that must NEVER fall out of the memory block as the campaign grows long — permanent events are shown in full every turn; regular ones are truncated to the recent tail.",
		"complete_quest":      "Complete (or fail/abandon) a quest: closes the quest, grants the rewards to the whole party (XP to each member, gold split across the party, planned items) and records a key event. Rewards come from the quest's rewardPlan; empty fields are computed from tables by quest difficulty and party level. Reports level-ups found — call level_up for each affected character.",
		"create_quest":        "Create a quest in the campaign: register an offered/accepted quest with its objective, difficulty and optional reward plan. Use when an NPC offers the party a task or the players take one on. The quest then lives in the campaign store and is shown in the injected memory. Rewards: fill rewardPlan for special rewards; empty fields are computed from tables on completion.",
		"append_thread":       "Append an open thread to the campaign ledger: a promise, mystery, debt or unresolved matter that must resurface later (\"promised the mayor\", \"who is the hooded man\", \"owe a favor\"). Open threads are injected into the DM memory every turn, so old hooks stay alive. Close them with resolve_thread when the story resolves them.",
		"search_memory":       "Search the campaign's long-term memory by keywords: transcripts of past in-game days, key events, loot ledger, world state, NPCs, locations, factions, characters, quests and campaign info. Returns matching snippets with the source label (e.g. 'транскрипт, день 3', 'NPC Марта') and the in-game day number. Use when you need a fact but don't know which in-game day it happened in (read_day only works by day number). Read-only.",
		"record_world_change": "Record a durable change to the world's current state — the opposite of an append-only log: it holds what is true NOW, grouped by category. Use for deaths (category «Погибшие»), destroyed/built places, decisions taken, alliance shifts. Prevents the world from contradicting itself (e.g. reviving a dead NPC). Idempotent by text within a category.",
	}
	for name, want := range expected {
		if got := descriptions[name]; got != want {
			t.Errorf("%s description расходится:\n got %q\nwant %q", name, got, want)
		}
	}
}
