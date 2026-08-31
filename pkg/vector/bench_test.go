package vector

import (
	"os"
	"testing"
)

// BenchmarkSearchCorpus times a brute-force scan over a real sidecar.
//
// It is skipped unless KG_BENCH_SIDECAR names a .kg/.cache directory, because
// the number that matters is the one measured on a real corpus rather than on
// synthetic vectors. ADR-0026 records the run on the 27,158-entity GDKB corpus
// and the budget it justifies; this is how to reproduce it:
//
//	KG_BENCH_SIDECAR=/path/to/.kg/.cache go test ./pkg/vector -bench SearchCorpus -benchtime 20x
func BenchmarkSearchCorpus(b *testing.B) {
	dir := os.Getenv("KG_BENCH_SIDECAR")
	if dir == "" {
		b.Skip("set KG_BENCH_SIDECAR to a .kg/.cache directory holding a vector sidecar")
	}
	s, err := Open(dir, "")
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if s.Len() == 0 {
		b.Skip("sidecar is empty")
	}
	query := make([]float64, s.Dim())
	copy(query, s.Row(0))

	// One filtered type, as `kg similar` issues it.
	typeName := s.Index().Entries[0].Type
	field := s.Index().Entries[0].Field
	b.ReportMetric(float64(s.Len()), "rows")
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.Search(Query{Vector: query, Type: typeName, Fields: []string{field}, TopN: 10}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOpenCorpus times mapping a real sidecar in, which is the fixed cost
// `kg similar` pays before it can scan anything.
func BenchmarkOpenCorpus(b *testing.B) {
	dir := os.Getenv("KG_BENCH_SIDECAR")
	if dir == "" {
		b.Skip("set KG_BENCH_SIDECAR to a .kg/.cache directory holding a vector sidecar")
	}
	for b.Loop() {
		s, err := Open(dir, "")
		if err != nil {
			b.Fatal(err)
		}
		s.Close()
	}
}
