package command

import (
	"os"
	"path/filepath"
	"testing"
)

// setupKGDir creates a minimal .kg directory in a temp dir and returns its path.
func setupKGDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	kgDir := filepath.Join(dir, ".kg")
	if err := os.MkdirAll(filepath.Join(kgDir, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	return kgDir
}

func TestParseAttrs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "valid pairs",
			args: []string{"name=Charlie Davis", "role=Designer"},
			want: map[string]string{"name": "Charlie Davis", "role": "Designer"},
		},
		{
			name: "empty value",
			args: []string{"tag="},
			want: map[string]string{"tag": ""},
		},
		{
			name: "value with equals",
			args: []string{"expr=a=b"},
			want: map[string]string{"expr": "a=b"},
		},
		{
			name:    "missing equals",
			args:    []string{"badarg"},
			wantErr: true,
		},
		{
			name:    "empty key",
			args:    []string{"=value"},
			wantErr: true,
		},
		{
			name: "empty input",
			args: []string{},
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAttrs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
