package evalgame

import (
	"testing"

	"github.com/luckystrker/ago/eval"
)

// Счётчик гарантии цитат бросков сводится по повторам, кейсам и прогону:
// в отчёте видно и суммарно, и на кейс, что модель пропускает цитату сама
// (PLAN 40).
func TestBuildReportAggregatesQuoteGuarantee(t *testing.T) {
	t.Parallel()

	plans := []Plan{
		{Base: "rolls-01", Name: "rolls-01", Case: eval.Case{Name: "rolls-01"}},
		{Base: "rolls-02", Name: "rolls-02", Case: eval.Case{Name: "rolls-02"}},
	}
	results := []RepeatReport{
		{Passed: true, QuoteAppends: 2, QuoteParts: []string{"d20 = 11", "DC 12"}},
		{Passed: true, QuoteAppends: 1, QuoteParts: []string{"d20 = 11", "итого 15"}},
		{Passed: true},
	}
	metaFor := func(string) CaseMeta { return CaseMeta{Category: "rolls"} }

	report := BuildReport(plans, results, DefaultSlice, 2, metaFor)
	if got := report.Cases[0].QuoteAppends; got != 3 {
		t.Fatalf("кейс 1: дописок = %d, want 3", got)
	}
	want := []string{"d20 = 11", "DC 12", "итого 15"}
	if len(report.Cases[0].QuoteParts) != len(want) {
		t.Fatalf("кейс 1: компоненты = %v, want %v", report.Cases[0].QuoteParts, want)
	}
	for index := range want {
		if report.Cases[0].QuoteParts[index] != want[index] {
			t.Fatalf("кейс 1: компоненты = %v, want %v", report.Cases[0].QuoteParts, want)
		}
	}
	if got := report.Cases[1].QuoteAppends; got != 0 {
		t.Fatalf("кейс 2: дописок = %d, want 0", got)
	}
	if report.QuoteAppends != 3 || report.QuoteCases != 1 {
		t.Fatalf("итог = %d дописок в %d кейсах, want 3 в 1", report.QuoteAppends, report.QuoteCases)
	}
	if len(report.QuoteParts) != len(want) {
		t.Fatalf("итоговые компоненты = %v, want %v", report.QuoteParts, want)
	}
}
