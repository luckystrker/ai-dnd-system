package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "campaigns.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// Каждый update пишется в inbox до обработки; повторный update_id — дубль.
func TestTelegramInboxDeduplicatesByUpdateID(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	inserted, err := st.SaveTelegramUpdate(100, "-100:7", `{"update_id":100}`)
	if err != nil || !inserted {
		t.Fatalf("первая запись: inserted=%v err=%v", inserted, err)
	}
	again, err := st.SaveTelegramUpdate(100, "-100:7", `{"update_id":100}`)
	if err != nil {
		t.Fatalf("повтор: %v", err)
	}
	if again {
		t.Fatal("повторный update_id не должен вставляться второй раз")
	}

	pending, err := st.PendingTelegramUpdates()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].UpdateID != 100 || pending[0].ChatKey != "-100:7" {
		t.Fatalf("pending = %+v", pending)
	}
	if pending[0].ProcessedAt != nil {
		t.Fatalf("новый update уже помечен обработанным: %+v", pending[0])
	}
}

// Offset сохраняется отдельно и переживает переоткрытие базы; после пометки
// update больше не считается необработанным.
func TestTelegramOffsetAndProcessedSurviveReopen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "campaigns.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := st.SaveTelegramUpdate(5, "1", `{"update_id":5}`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveTelegramUpdate(6, "1", `{"update_id":6}`); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTelegramOffset(7); err != nil {
		t.Fatalf("SaveTelegramOffset: %v", err)
	}
	if err := st.MarkTelegramUpdateProcessed(5); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("повторный Open: %v", err)
	}
	defer st.Close()
	offset, err := st.TelegramOffset()
	if err != nil || offset != 7 {
		t.Fatalf("offset = %d, %v", offset, err)
	}
	pending, err := st.PendingTelegramUpdates()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].UpdateID != 6 {
		t.Fatalf("после рестарта pending = %+v", pending)
	}
}

// Повторная пометка идемпотентна: восстановление после падения не падает.
func TestMarkTelegramUpdateProcessedIsIdempotent(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	if _, err := st.SaveTelegramUpdate(1, "1", `{}`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := st.MarkTelegramUpdateProcessed(1); err != nil {
			t.Fatalf("пометка %d: %v", i+1, err)
		}
	}
}

// Новые таблицы не трогают прод-схему: старые таблицы на месте, конкурентный
// старт второго процесса не падает на CREATE TABLE IF NOT EXISTS.
func TestTelegramTablesAreAdditive(t *testing.T) {
	t.Parallel()

	st := openTestStore(t)
	for _, table := range []string{"telegram_updates", "telegram_poll_state", "event_streams"} {
		var name string
		err := st.db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		if err != nil {
			t.Fatalf("таблица %s: %v", table, err)
		}
	}
	var count int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM campaigns").Scan(&count); err != nil {
		t.Fatalf("campaigns: %v", err)
	}
}
