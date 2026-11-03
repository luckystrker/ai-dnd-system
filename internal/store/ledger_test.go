package store

import (
	"strconv"
	"strings"
	"testing"
)

func TestLedgerFormatting(t *testing.T) {
	store, slug := journalFixture(t)

	if err := store.AppendLedgerRow(slug, LedgerRow{
		Day: 3, Type: LedgerFound, ItemOrGold: "серебряный кинжал",
		By: ptr("Дэн"), Note: ptr("в сундуке"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := store.ReadLedgerRaw(slug)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "- [День 3] найдено: серебряный кинжал (Дэн) — в сундуке") {
		t.Fatalf("raw = %q", raw)
	}
	if err := store.AppendLedgerRow(slug, LedgerRow{Day: 5, Type: LedgerSpent, ItemOrGold: "20 золотых", Note: ptr("на ночлег")}, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ = store.ReadLedgerRaw(slug)
	if !strings.Contains(raw, "- [День 5] потрачено: 20 золотых — на ночлег") {
		t.Fatalf("raw = %q", raw)
	}
}

func TestLedgerDedupAndEmpty(t *testing.T) {
	store, slug := journalFixture(t)

	if err := store.AppendLedgerRow(slug, LedgerRow{Day: 1, Type: LedgerFound, ItemOrGold: "50 золотых"}, ptr("g1")); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLedgerRow(slug, LedgerRow{Day: 1, Type: LedgerFound, ItemOrGold: "50 золотых"}, ptr("g1")); err != nil {
		t.Fatal(err)
	}
	lines, _ := store.ReadLedger(slug, nil, 0)
	if len(lines) != 1 {
		t.Fatalf("дедуп по eventId: %v", lines)
	}
	if err := store.AppendLedgerRow(slug, LedgerRow{Day: 1, Type: LedgerFound, ItemOrGold: "10 золотых"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLedgerRow(slug, LedgerRow{Day: 1, Type: LedgerFound, ItemOrGold: "10 золотых"}, nil); err != nil {
		t.Fatal(err)
	}
	lines, _ = store.ReadLedger(slug, nil, 0)
	if len(lines) != 3 {
		t.Fatalf("без eventId дубли разрешены: %v", lines)
	}
	if _, err := store.ReadLedgerRaw("no-such-campaign"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLedgerRow(slug, LedgerRow{Day: 1, Type: LedgerFound, ItemOrGold: "   "}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestLedgerFiltersAndTail(t *testing.T) {
	store, slug := journalFixture(t)

	_ = store.AppendLedgerRow(slug, LedgerRow{Day: 1, Type: LedgerFound, ItemOrGold: "кинжал"}, nil)
	_ = store.AppendLedgerRow(slug, LedgerRow{Day: 3, Type: LedgerFound, ItemOrGold: "меч"}, nil)
	_ = store.AppendLedgerRow(slug, LedgerRow{Day: 1, Type: LedgerSpent, ItemOrGold: "еда"}, nil)

	byDay, _ := store.ReadLedger(slug, &LedgerFilter{Day: ptr(3)}, 0)
	if len(byDay) != 1 || !strings.Contains(byDay[0], "меч") {
		t.Fatalf("фильтр по дню = %v", byDay)
	}
	found, _ := store.ReadLedger(slug, &LedgerFilter{Type: ptr(LedgerFound)}, 0)
	if len(found) != 2 {
		t.Fatalf("фильтр по типу = %v", found)
	}
	spent, _ := store.ReadLedger(slug, &LedgerFilter{Type: ptr(LedgerSpent)}, 0)
	if len(spent) != 1 {
		t.Fatalf("spent = %v", spent)
	}

	for i := 4; i <= 7; i++ {
		_ = store.AppendLedgerRow(slug, LedgerRow{Day: i, Type: LedgerFound, ItemOrGold: "предмет " + strconv.Itoa(i)}, nil)
	}
	tail, _ := store.ReadLedger(slug, nil, 2)
	if len(tail) != 2 || !strings.Contains(tail[1], "предмет 7") {
		t.Fatalf("tail = %v", tail)
	}
}
