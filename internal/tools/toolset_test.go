package tools

import (
	"testing"
)

func TestToolkitsAndSchemas(t *testing.T) {
	deps := Deps{}.withDefaults()

	dm, err := DMToolkit(deps)
	if err != nil {
		t.Fatalf("DMToolkit: %v", err)
	}
	if got := len(dm.Tools()); got != 43 {
		t.Fatalf("DM tools = %d, want 43", got)
	}
	chronicler, err := ChroniclerToolkit(deps)
	if err != nil {
		t.Fatalf("ChroniclerToolkit: %v", err)
	}
	if got := len(chronicler.Tools()); got != len(ChroniclerTools) {
		t.Fatalf("chronicler tools = %d, want %d", got, len(ChroniclerTools))
	}

	dmNames := map[string]bool{}
	for _, item := range dm.Tools() {
		dmNames[item.Name()] = true
		// Схема строится без паники — значит теги валидны.
		if _, err := item.Schemas(nil); err != nil {
			t.Fatalf("Schemas(%s): %v", item.Name(), err)
		}
	}
	for _, name := range ChroniclerTools {
		if !dmNames[name] {
			t.Errorf("chronicler tool %q отсутствует в DM toolkit", name)
		}
	}
	for _, item := range chronicler.Tools() {
		if _, err := item.Schemas(nil); err != nil {
			t.Fatalf("Schemas(%s): %v", item.Name(), err)
		}
	}
}
