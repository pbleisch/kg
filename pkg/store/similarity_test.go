package store

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name    string
		a, b    []float64
		want    float64
		wantErr bool
	}{
		{
			name: "identical vectors",
			a:    []float64{1, 0, 0},
			b:    []float64{1, 0, 0},
			want: 1.0,
		},
		{
			name: "orthogonal vectors",
			a:    []float64{1, 0},
			b:    []float64{0, 1},
			want: 0.0,
		},
		{
			name: "opposite vectors",
			a:    []float64{1, 0},
			b:    []float64{-1, 0},
			want: -1.0,
		},
		{
			name: "zero vector",
			a:    []float64{0, 0},
			b:    []float64{1, 0},
			want: 0.0,
		},
		{
			name:    "dimension mismatch",
			a:       []float64{1, 2},
			b:       []float64{1},
			wantErr: true,
		},
		{
			name: "general case",
			a:    []float64{1, 2, 3},
			b:    []float64{4, 5, 6},
			want: 0.9746,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CosineSimilarity(tt.a, tt.b)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CosineSimilarity error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && math.Abs(got-tt.want) > 1e-3 {
				t.Errorf("CosineSimilarity = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDotProduct(t *testing.T) {
	got, err := DotProduct([]float64{1, 2, 3}, []float64{4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	if got != 32 {
		t.Errorf("DotProduct = %v, want 32", got)
	}

	_, err = DotProduct([]float64{1, 2}, []float64{1})
	if err == nil {
		t.Error("expected dimension mismatch error")
	}
}

func TestExtractVector(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   []float64
		wantOK bool
	}{
		{
			name:   "native float64 slice",
			input:  []float64{1.0, 2.0},
			want:   []float64{1.0, 2.0},
			wantOK: true,
		},
		{
			name:   "[]any from JSON unmarshal",
			input:  []any{float64(1.5), float64(2.5)},
			want:   []float64{1.5, 2.5},
			wantOK: true,
		},
		{
			name:   "[]any with int",
			input:  []any{int(1), int(2)},
			want:   []float64{1, 2},
			wantOK: true,
		},
		{
			name:   "string — not a vector",
			input:  "hello",
			wantOK: false,
		},
		{
			name:   "[]any with string element",
			input:  []any{"x", "y"},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractVector(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ExtractVector ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if len(got) != len(tt.want) {
					t.Fatalf("len = %d, want %d", len(got), len(tt.want))
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("[%d] = %v, want %v", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestSortResults(t *testing.T) {
	results := []SimilarityResult{
		{Score: 0.5},
		{Score: 0.9},
		{Score: 0.1},
	}
	SortResults(results)
	if results[0].Score != 0.9 || results[1].Score != 0.5 || results[2].Score != 0.1 {
		t.Errorf("unexpected order: %v", results)
	}
}
