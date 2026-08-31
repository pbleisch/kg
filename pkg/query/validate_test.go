package query

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seed writes a file verbatim, without going through the write boundary,
// because every violation these tests seed is one the boundary refuses.
func seed(t *testing.T, kgDir, typeName, file, body string) string {
	t.Helper()
	dir := filepath.Join(kgDir, "entities", typeName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func codes(t *testing.T, kgDir string) (structural, advisory map[string]int, rep *Report) {
	t.Helper()
	rep, err := Validate(kgDir)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	structural, advisory = map[string]int{}, map[string]int{}
	for _, s := range rep.Structural {
		structural[s.Code]++
	}
	for _, a := range rep.Advisories {
		advisory[a.Code]++
	}
	return structural, advisory, rep
}

// Two files claiming one identity fail the load, naming both. Under
// path-derived identity that means two paths deriving one identity, which is
// what the two encodings of one name do (ADR-0029).
func TestValidate_IdentityCollisionNamesBothFiles(t *testing.T) {
	kg := t.TempDir()
	a := seed(t, kg, "source", "m%C3%A4yra.yaml", "id: source:mäyra\ntype: source\n")
	b := seed(t, kg, "source", "mäyra.yaml", "id: source:mäyra\ntype: source\n")

	rep, err := Validate(kg)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rep.OK() {
		t.Fatal("a corpus with one identity in two files loaded")
	}
	var named []string
	for _, s := range rep.Structural {
		named = append(named, s.Files...)
	}
	joined := strings.Join(named, " ")
	if !strings.Contains(joined, filepath.Base(a)) || !strings.Contains(joined, filepath.Base(b)) {
		t.Errorf("the refusal named %v, want both %q and %q", named, a, b)
	}
}

// Two files that both derive the same identity through the same encoding — a
// collision the filesystem itself cannot produce with a flat layout, so it is
// seeded through a nested directory.
func TestValidate_CollisionNamesEveryClaimant(t *testing.T) {
	kg := t.TempDir()
	seed(t, kg, "person", "alice.yaml", "id: person:alice\ntype: person\n")
	nested := filepath.Join(kg, "entities", "person", "extra")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "alice.yaml"), []byte("id: person:alice\ntype: person\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	structural, _, rep := codes(t, kg)
	if rep.OK() {
		t.Fatal("a nested duplicate loaded")
	}
	if structural["bad_path"] != 1 {
		t.Errorf("codes = %v, want one bad_path", structural)
	}
}

// A file whose declared identity disagrees with its location fails, and the
// message says both identities so a repair knows which way to go.
func TestValidate_IDDisagreementFails(t *testing.T) {
	kg := t.TempDir()
	seed(t, kg, "person", "alice.yaml", "id: person:alicia\ntype: person\n")

	structural, _, rep := codes(t, kg)
	if rep.OK() {
		t.Fatal("a file whose id disagrees with its path loaded")
	}
	if structural["id_disagreement"] != 1 {
		t.Fatalf("codes = %v, want one id_disagreement", structural)
	}
	msg := rep.Structural[0].Message
	if !strings.Contains(msg, "person:alicia") || !strings.Contains(msg, "person:alice") {
		t.Errorf("message = %q, want both identities named", msg)
	}
}

func TestValidate_TypeDisagreementFails(t *testing.T) {
	kg := t.TempDir()
	seed(t, kg, "person", "alice.yaml", "id: person:alice\ntype: robot\n")

	structural, _, rep := codes(t, kg)
	if rep.OK() || structural["type_disagreement"] != 1 {
		t.Fatalf("codes = %v, want one type_disagreement", structural)
	}
}

// The name grammar is structural, at the load as at the write boundary: an
// attribute named "--dir" is a command-line flag that became knowledge.
func TestValidate_IllegalAttributeFails(t *testing.T) {
	kg := t.TempDir()
	seed(t, kg, "person", "alice.yaml", "id: person:alice\ntype: person\nattributes:\n  --dir: .kg\n")

	structural, _, rep := codes(t, kg)
	if rep.OK() || structural["illegal_attribute"] != 1 {
		t.Fatalf("codes = %v, want one illegal_attribute", structural)
	}
}

// A relation pointing at nothing is REPORTED, and the graph still loads. This
// is the rule ADR-0028 settled: it is a property of the corpus at one instant,
// not of the file holding the edge, and OKF §11 forbids rejecting over it.
func TestValidate_DanglingTargetIsReportedNotRefused(t *testing.T) {
	kg := t.TempDir()
	seed(t, kg, "person", "alice.yaml", "id: person:alice\ntype: person\nrelations:\n  knows:\n    - person:nobody\n")

	structural, advisory, rep := codes(t, kg)
	if !rep.OK() {
		t.Fatalf("a dangling target refused the load: %v", rep.Structural)
	}
	if len(structural) != 0 {
		t.Errorf("structural = %v, want none", structural)
	}
	if advisory["dangling_relation"] != 1 {
		t.Fatalf("advisories = %v, want one dangling_relation", advisory)
	}
	if !strings.Contains(rep.Advisories[0].Message, "person:nobody") {
		t.Errorf("message = %q, want the target named", rep.Advisories[0].Message)
	}
}

// A graph full of warnings still loads.
func TestValidate_WarningsDoNotBlock(t *testing.T) {
	kg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(kg, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kg, "schema", "person.yaml"),
		[]byte("type: person\nattributes:\n  name:\n    type: string\n    required: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "bob", "carol"} {
		seed(t, kg, "person", name+".yaml",
			"id: person:"+name+"\ntype: person\nattributes:\n  nickname: x\nrelations:\n  knows:\n    - person:nobody\n")
	}

	structural, advisory, rep := codes(t, kg)
	if !rep.OK() {
		t.Fatalf("a graph of warnings refused: %v", structural)
	}
	if advisory["unknown_attribute"] != 3 || advisory["missing_attribute"] != 3 || advisory["dangling_relation"] != 3 {
		t.Errorf("advisories = %v, want 3 of each", advisory)
	}
	if rep.Entities != 3 {
		t.Errorf("Entities = %d, want 3", rep.Entities)
	}
}

// A type with no schema is one warning, not one per entity.
func TestValidate_NoSchemaIsOneWarningPerType(t *testing.T) {
	kg := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d"} {
		seed(t, kg, "widget", name+".yaml", "id: widget:"+name+"\ntype: widget\n")
	}
	_, advisory, rep := codes(t, kg)
	if !rep.OK() {
		t.Fatal("a type with no schema refused the load")
	}
	if advisory["no_schema"] != 1 {
		t.Errorf("advisories = %v, want exactly one no_schema", advisory)
	}
}

// Validate reports every offending file, not the first: a corpus is repaired
// in one pass or not at all.
func TestValidate_ReportsEveryOffender(t *testing.T) {
	kg := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		seed(t, kg, "person", name+".yaml", "id: person:wrong_"+name+"\ntype: person\n")
	}
	structural, _, _ := codes(t, kg)
	if structural["id_disagreement"] != 3 {
		t.Errorf("codes = %v, want three id_disagreement", structural)
	}
}

// The same corpus answers with the same findings in the same order (P6).
func TestValidate_IsDeterministic(t *testing.T) {
	kg := t.TempDir()
	for _, name := range []string{"c", "a", "b"} {
		seed(t, kg, "person", name+".yaml", "id: person:x\ntype: nope\nattributes:\n  --dir: .kg\n")
	}
	first, err := Validate(kg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Validate(kg)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Structural) != len(second.Structural) {
		t.Fatalf("counts differ: %d vs %d", len(first.Structural), len(second.Structural))
	}
	for i := range first.Structural {
		a, b := first.Structural[i], second.Structural[i]
		if a.Code != b.Code || a.ID != b.ID || a.Message != b.Message ||
			strings.Join(a.Files, "|") != strings.Join(b.Files, "|") {
			t.Errorf("finding %d differs:\n %+v\n %+v", i, a, b)
		}
	}
}
