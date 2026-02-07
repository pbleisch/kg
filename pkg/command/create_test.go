package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
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

func TestCreate(t *testing.T) {
	kgDir := setupKGDir(t)

	e, err := Create(kgDir, "Person", "charlie", map[string]string{
		"name": "Charlie Davis",
		"role": "Designer",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if e.ID != "person:charlie" {
		t.Errorf("ID = %q, want %q", e.ID, "person:charlie")
	}
	if e.Type != "Person" {
		t.Errorf("Type = %q, want %q", e.Type, "Person")
	}
	if e.Attributes["name"] != "Charlie Davis" {
		t.Errorf("name = %v, want %q", e.Attributes["name"], "Charlie Davis")
	}
}

func TestCreateReadBack(t *testing.T) {
	kgDir := setupKGDir(t)

	_, err := Create(kgDir, "Person", "alice", map[string]string{"name": "Alice"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.ReadEntity(kgDir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity failed: %v", err)
	}
	if got.Attributes["name"] != "Alice" {
		t.Errorf("name = %v, want %q", got.Attributes["name"], "Alice")
	}
}

func TestCreateDuplicate(t *testing.T) {
	kgDir := setupKGDir(t)

	_, err := Create(kgDir, "Person", "bob", map[string]string{"name": "Bob"})
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	_, err = Create(kgDir, "Person", "bob", map[string]string{"name": "Bob2"})
	if err == nil {
		t.Fatal("expected error for duplicate entity, got nil")
	}
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
