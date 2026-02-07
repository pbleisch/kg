package query

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := map[string]Morphism{
		"teammates":  {Pattern: "-> worksOn <- worksOn", Description: "People who work on the same projects"},
		"management": {Pattern: "->* reportsTo", Description: "Full management chain"},
	}

	if err := SaveMorphisms(dir, want); err != nil {
		t.Fatalf("SaveMorphisms: %v", err)
	}

	got, err := LoadMorphisms(dir)
	if err != nil {
		t.Fatalf("LoadMorphisms: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d morphisms, want %d", len(got), len(want))
	}
	for name, wm := range want {
		gm, ok := got[name]
		if !ok {
			t.Errorf("missing morphism %q", name)
			continue
		}
		if gm.Pattern != wm.Pattern || gm.Description != wm.Description {
			t.Errorf("%s: got %+v, want %+v", name, gm, wm)
		}
	}
}

func TestLoadMorphisms_NoFile(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadMorphisms(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil, got %v", m)
	}
}

func TestDefineMorphism(t *testing.T) {
	dir := t.TempDir()

	if err := DefineMorphism(dir, "teammates", "-> worksOn <- worksOn", "co-workers"); err != nil {
		t.Fatalf("DefineMorphism: %v", err)
	}
	if err := DefineMorphism(dir, "mgmt", "->* reportsTo", ""); err != nil {
		t.Fatalf("DefineMorphism: %v", err)
	}

	m, err := LoadMorphisms(dir)
	if err != nil {
		t.Fatalf("LoadMorphisms: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("got %d morphisms, want 2", len(m))
	}
	if m["teammates"].Pattern != "-> worksOn <- worksOn" {
		t.Errorf("teammates pattern = %q", m["teammates"].Pattern)
	}
	if m["mgmt"].Description != "" {
		t.Errorf("mgmt description = %q, want empty", m["mgmt"].Description)
	}
}

func TestDefineMorphism_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := DefineMorphism(dir, "x", "-> y", ""); err != nil {
		t.Fatalf("DefineMorphism: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "morphisms.yaml")); err != nil {
		t.Errorf("morphisms.yaml not created: %v", err)
	}
}

func TestExpandMorphism_Forward(t *testing.T) {
	dir := t.TempDir()
	if err := SaveMorphisms(dir, map[string]Morphism{
		"mgmt": {Pattern: "->* reportsTo"},
	}); err != nil {
		t.Fatalf("SaveMorphisms: %v", err)
	}

	steps, err := ExpandMorphism(dir, "mgmt")
	if err != nil {
		t.Fatalf("ExpandMorphism: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1", len(steps))
	}
	if steps[0].Relation != "reportsTo" || !steps[0].Recursive {
		t.Errorf("step = %+v, want {reportsTo, true}", steps[0])
	}
}

func TestExpandMorphism_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := ExpandMorphism(dir, "nope")
	if err == nil {
		t.Fatal("expected error for missing morphism")
	}
	qe, ok := err.(*QueryError)
	if !ok {
		t.Fatalf("expected *QueryError, got %T", err)
	}
	if qe.Code != "morphism_not_found" {
		t.Errorf("code = %q, want morphism_not_found", qe.Code)
	}
}

func TestParsePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    []PathStep
		wantErr bool
	}{
		{
			name:    "single forward",
			pattern: "-> worksOn",
			want:    []PathStep{{Relation: "worksOn"}},
		},
		{
			name:    "recursive forward",
			pattern: "->* reportsTo",
			want:    []PathStep{{Relation: "reportsTo", Recursive: true}},
		},
		{
			name:    "multi-step forward",
			pattern: "-> reportsTo -> worksOn",
			want:    []PathStep{{Relation: "reportsTo"}, {Relation: "worksOn"}},
		},
		{
			name:    "back direction unsupported",
			pattern: "<- worksOn",
			wantErr: true,
		},
		{
			name:    "back recursive unsupported",
			pattern: "<-* dependsOn",
			wantErr: true,
		},
		{
			name:    "empty pattern",
			pattern: "",
			wantErr: true,
		},
		{
			name:    "missing relation",
			pattern: "->",
			wantErr: true,
		},
		{
			name:    "bad token",
			pattern: "worksOn",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePattern(tt.pattern)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d steps, want %d", len(got), len(tt.want))
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("step[%d] = %+v, want %+v", i, got[i], w)
				}
			}
		})
	}
}
