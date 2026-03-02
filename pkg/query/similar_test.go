package query

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVector(t *testing.T) {
	tests := []struct {
		input   string
		want    []float64
		wantErr bool
	}{
		{"1.0,2.0,3.0", []float64{1, 2, 3}, false},
		{"0.1, 0.2, 0.3", []float64{0.1, 0.2, 0.3}, false},
		{"", nil, true},
		{"a,b,c", nil, true},
		{"-1.5,0,1.5", []float64{-1.5, 0, 1.5}, false},
	}
	for _, tt := range tests {
		got, err := ParseVector(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseVector(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if err == nil {
			if len(got) != len(tt.want) {
				t.Errorf("ParseVector(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
				continue
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseVector(%q)[%d] = %v, want %v", tt.input, i, got[i], tt.want[i])
				}
			}
		}
	}
}

func TestLoadVectorFile(t *testing.T) {
	dir := t.TempDir()

	// Comma-separated single line.
	p1 := filepath.Join(dir, "vec1.txt")
	os.WriteFile(p1, []byte("1.0,2.0,3.0"), 0o644)
	v1, err := LoadVectorFile(p1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v1) != 3 || v1[0] != 1.0 {
		t.Errorf("unexpected vector: %v", v1)
	}

	// One float per line.
	p2 := filepath.Join(dir, "vec2.txt")
	os.WriteFile(p2, []byte("4.0\n5.0\n6.0\n"), 0o644)
	v2, err := LoadVectorFile(p2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v2) != 3 || v2[0] != 4.0 {
		t.Errorf("unexpected vector: %v", v2)
	}

	// Empty file.
	p3 := filepath.Join(dir, "empty.txt")
	os.WriteFile(p3, []byte(""), 0o644)
	_, err = LoadVectorFile(p3)
	if err == nil {
		t.Error("expected error for empty file")
	}

	// Non-existent file.
	_, err = LoadVectorFile(filepath.Join(dir, "nope.txt"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}
