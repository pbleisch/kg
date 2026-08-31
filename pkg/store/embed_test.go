package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pbleisch/kg/pkg/vector"
)

// --- TextChecksum ---

func TestTextChecksum_Deterministic(t *testing.T) {
	if TextChecksum("hello") != TextChecksum("hello") {
		t.Error("TextChecksum is not deterministic")
	}
}

func TestTextChecksum_Differs(t *testing.T) {
	if TextChecksum("hello") == TextChecksum("world") {
		t.Error("distinct texts produced the same checksum")
	}
}

// --- shellSplit ---

func TestShellSplit(t *testing.T) {
	tests := []struct {
		input string
		want  []string
		err   bool
	}{
		{"python3 embed.py", []string{"python3", "embed.py"}, false},
		{`python3 "my script.py"`, []string{"python3", "my script.py"}, false},
		{"single", []string{"single"}, false},
		{`"unterminated`, nil, true},
		{"", nil, true},
		{"  spaced  args  ", []string{"spaced", "args"}, false},
	}
	for _, tt := range tests {
		got, err := shellSplit(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("shellSplit(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("shellSplit(%q): %v", tt.input, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("shellSplit(%q): got %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("shellSplit(%q)[%d]: got %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

// --- RunEmbedSingle ---

func writeMockEmbedScript(t *testing.T, dir string) string {
	t.Helper()
	// Script reads JSON stdin, ignores it, outputs a fixed 3-dim vector.
	script := `#!/bin/sh
cat > /dev/null
echo '[0.1, 0.2, 0.3]'
`
	p := filepath.Join(dir, "mock_embed.sh")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeMockBatchScript(t *testing.T, dir string) string {
	t.Helper()
	// Script reads JSON stdin, counts items, outputs that many vectors.
	// Uses python for JSON manipulation since sh can't easily do it.
	script := `#!/bin/sh
INPUT=$(cat)
# Count items by counting "entity_id" occurrences
COUNT=$(echo "$INPUT" | grep -o '"entity_id"' | wc -l | tr -d ' ')
# Build vectors array
VECTORS="["
for i in $(seq 1 $COUNT); do
  if [ "$i" -gt 1 ]; then VECTORS="$VECTORS,"; fi
  VECTORS="$VECTORS[0.$i, 0.$i, 0.$i]"
done
VECTORS="$VECTORS]"
echo "{\"vectors\": $VECTORS}"
`
	p := filepath.Join(dir, "mock_batch.sh")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeMockFailScript(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
echo "something went wrong" >&2
exit 1
`
	p := filepath.Join(dir, "mock_fail.sh")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunEmbedSingle(t *testing.T) {
	dir := t.TempDir()
	cmd := writeMockEmbedScript(t, dir)

	vec, err := RunEmbedSingle(cmd, EmbedInput{
		Text:     "Alice Smith",
		Field:    "name",
		EntityID: "person:alice",
		Type:     "person",
	})
	if err != nil {
		t.Fatalf("RunEmbedSingle: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3-dim vector, got %d", len(vec))
	}
	if vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Errorf("unexpected vector: %v", vec)
	}
}

func TestRunEmbedSingle_Failure(t *testing.T) {
	dir := t.TempDir()
	cmd := writeMockFailScript(t, dir)

	_, err := RunEmbedSingle(cmd, EmbedInput{Text: "test"})
	if err == nil {
		t.Fatal("expected error from failing embed command")
	}
}

func TestRunEmbedBatch(t *testing.T) {
	dir := t.TempDir()
	cmd := writeMockBatchScript(t, dir)

	inputs := []EmbedInput{
		{Text: "Alice", Field: "name", EntityID: "person:alice", Type: "person"},
		{Text: "Bob", Field: "name", EntityID: "person:bob", Type: "person"},
	}
	vectors, err := RunEmbedBatch(cmd, inputs)
	if err != nil {
		t.Fatalf("RunEmbedBatch: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	for i, v := range vectors {
		if len(v) != 3 {
			t.Errorf("vector[%d]: expected 3-dim, got %d", i, len(v))
		}
	}
}

func TestRunEmbedBatch_Failure(t *testing.T) {
	dir := t.TempDir()
	cmd := writeMockFailScript(t, dir)

	_, err := RunEmbedBatch(cmd, []EmbedInput{{Text: "test"}})
	if err == nil {
		t.Fatal("expected error from failing embed command")
	}
}

// --- Embed() ---

// setupEmbedDir writes entities plus a per-checkout embedder.yaml naming an
// absolute command, which is the only shape ResolveEmbedder accepts.
func setupEmbedDir(t *testing.T, entities []*Entity, embedCmd string, fields []string, batch bool) string {
	t.Helper()
	dir := t.TempDir()
	for _, e := range entities {
		if err := WriteEntity(dir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", e.ID, err)
		}
	}
	writeEmbedder(t, dir, embedCmd, fields, batch)
	return dir
}

func writeEmbedder(t *testing.T, kgDir, cmd string, fields []string, batch bool) {
	t.Helper()
	batchStr := "false"
	if batch {
		batchStr = "true"
	}
	content := "command: \"" + cmd + "\"\nfields: [" + strings.Join(fields, ", ") + "]\nbatch: " + batchStr + "\n"
	if err := os.WriteFile(EmbedderPath(kgDir), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// openSidecar opens the sidecar a test just built, failing the test if it is
// not there.
func openSidecar(t *testing.T, kgDir string) *vector.Store {
	t.Helper()
	emb, err := ResolveEmbedder(kgDir)
	if err != nil {
		t.Fatalf("ResolveEmbedder: %v", err)
	}
	vs, err := vector.Open(CacheDir(kgDir), emb.Digest())
	if err != nil {
		t.Fatalf("opening sidecar: %v", err)
	}
	t.Cleanup(func() { vs.Close() })
	return vs
}

func TestEmbed_NoEmbedderConfigured(t *testing.T) {
	dir := t.TempDir()
	_, err := Embed(dir, false, false)
	var missing *ErrNoEmbedder
	if !errors.As(err, &missing) {
		t.Fatalf("Embed returned %v, want *ErrNoEmbedder", err)
	}
}

// TestEmbed_LeavesEntityFilesAlone is the property this whole change exists
// for: embedding writes derived data and touches no entity file.
func TestEmbed_LeavesEntityFilesAlone(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	e := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	dir := setupEmbedDir(t, []*Entity{e}, cmd, []string{"name"}, false)

	path := EntityPath(dir, "person:alice")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	res, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if res.Embedded != 1 {
		t.Errorf("Embedded = %d, want 1", res.Embedded)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("embedding rewrote the entity file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if strings.Contains(string(after), "_embedding") {
		t.Error("entity file carries an embedding attribute")
	}

	vs := openSidecar(t, dir)
	got, ok := vs.Vector("person:alice", "name")
	if !ok {
		t.Fatal("sidecar has no vector for person:alice/name")
	}
	if len(got) != 3 || got[0] != 0.1 {
		t.Errorf("sidecar vector = %v, want [0.1 0.2 0.3]", got)
	}
}

func TestEmbed_DryRunWritesNothing(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	e := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	dir := setupEmbedDir(t, []*Entity{e}, cmd, []string{"name"}, false)

	res, err := Embed(dir, false, true)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if res.Embedded != 1 {
		t.Errorf("dry run reported %d to embed, want 1", res.Embedded)
	}
	if _, err := os.Stat(vector.BinPath(CacheDir(dir))); !os.IsNotExist(err) {
		t.Error("dry run wrote a sidecar")
	}
}

func TestEmbed_BatchMode(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockBatchScript(t, tmp)
	dir := setupEmbedDir(t, []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": "Bob"}},
	}, cmd, []string{"name"}, true)

	res, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if res.Embedded != 2 {
		t.Fatalf("Embedded = %d, want 2", res.Embedded)
	}
	vs := openSidecar(t, dir)
	if vs.Len() != 2 {
		t.Errorf("sidecar holds %d rows, want 2", vs.Len())
	}
}

func TestEmbed_CarriesUnchanged(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	e := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	dir := setupEmbedDir(t, []*Entity{e}, cmd, []string{"name"}, false)

	if _, err := Embed(dir, false, false); err != nil {
		t.Fatal(err)
	}
	res, err := Embed(dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 0 {
		t.Errorf("Embedded = %d on an unchanged graph, want 0", res.Embedded)
	}
	if res.Carried != 1 {
		t.Errorf("Carried = %d, want 1", res.Carried)
	}
}

func TestEmbed_ReembedsChanged(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	e := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	dir := setupEmbedDir(t, []*Entity{e}, cmd, []string{"name"}, false)

	if _, err := Embed(dir, false, false); err != nil {
		t.Fatal(err)
	}
	e.Attributes["name"] = "Alicia"
	if err := WriteEntity(dir, e); err != nil {
		t.Fatal(err)
	}
	res, err := Embed(dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 1 || res.Carried != 0 {
		t.Errorf("Embedded=%d Carried=%d, want 1 and 0 after a text change", res.Embedded, res.Carried)
	}
}

func TestEmbed_RebuildReembeds(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	e := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	dir := setupEmbedDir(t, []*Entity{e}, cmd, []string{"name"}, false)

	if _, err := Embed(dir, false, false); err != nil {
		t.Fatal(err)
	}
	res, err := Embed(dir, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 1 {
		t.Errorf("Embedded = %d under --rebuild, want 1", res.Embedded)
	}
}

func TestEmbed_DropsRemovedEntities(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	dir := setupEmbedDir(t, []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": "Bob"}},
	}, cmd, []string{"name"}, false)

	if _, err := Embed(dir, false, false); err != nil {
		t.Fatal(err)
	}
	if err := DeleteEntity(dir, "person:bob"); err != nil {
		t.Fatal(err)
	}
	res, err := Embed(dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Dropped != 1 {
		t.Errorf("Dropped = %d after deleting an entity, want 1", res.Dropped)
	}
	vs := openSidecar(t, dir)
	if _, ok := vs.Vector("person:bob", "name"); ok {
		t.Error("sidecar still holds a vector for the deleted entity")
	}
}

func TestEmbed_SkipsMissingAndEmptyFields(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	dir := setupEmbedDir(t, []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"role": "engineer"}},
		{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": ""}},
	}, cmd, []string{"name"}, false)

	res, err := Embed(dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 0 {
		t.Errorf("Embedded = %d, want 0 when no entity has the field", res.Embedded)
	}
}

func TestEmbed_SpecificEntityIDs(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	alice := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	bob := &Entity{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": "Bob"}}
	dir := setupEmbedDir(t, []*Entity{alice, bob}, cmd, []string{"name"}, false)

	if _, err := Embed(dir, false, false); err != nil {
		t.Fatal(err)
	}
	// Both texts change, but only alice is named.
	alice.Attributes["name"] = "Alicia"
	bob.Attributes["name"] = "Robert"
	for _, e := range []*Entity{alice, bob} {
		if err := WriteEntity(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Embed(dir, false, false, "person:alice")
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 1 {
		t.Errorf("Embedded = %d, want 1 when one entity is named", res.Embedded)
	}
	// Bob keeps a vector so `similar` still finds him, but keeps his old
	// checksum so the next whole-graph pass re-embeds him.
	vs := openSidecar(t, dir)
	e, ok := vs.Entry("person:bob", "name")
	if !ok {
		t.Fatal("bob lost his vector")
	}
	if e.Checksum == TextChecksum("Robert") {
		t.Error("bob's checksum was advanced without re-embedding him")
	}
}

func TestEmbed_MultipleFields(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockBatchScript(t, tmp)
	dir := setupEmbedDir(t, []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice", "bio": "engineer"}},
	}, cmd, []string{"name", "bio"}, true)

	res, err := Embed(dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 2 {
		t.Fatalf("Embedded = %d, want 2 (one per field)", res.Embedded)
	}
	vs := openSidecar(t, dir)
	for _, f := range []string{"name", "bio"} {
		if _, ok := vs.Vector("person:alice", f); !ok {
			t.Errorf("no sidecar vector for field %q", f)
		}
	}
}

func TestEmbed_CommandFailure(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockFailScript(t, tmp)
	dir := setupEmbedDir(t, []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
	}, cmd, []string{"name"}, false)

	res, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("single-mode failure should be reported per entity, not fatal: %v", err)
	}
	if res.Errors != 1 {
		t.Errorf("Errors = %d, want 1", res.Errors)
	}
	if len(res.Warnings) == 0 {
		t.Error("a failed embed produced no warning")
	}
}

// TestEmbed_ChangingEmbedderInvalidates is P2's second check made executable:
// changing the embedding model changes not one entity file, and the vectors it
// invalidates are all in one derived file.
func TestEmbed_ChangingEmbedderInvalidates(t *testing.T) {
	tmp := t.TempDir()
	first := writeMockEmbedScript(t, tmp)
	e := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}}
	dir := setupEmbedDir(t, []*Entity{e}, first, []string{"name"}, false)

	if _, err := Embed(dir, false, false); err != nil {
		t.Fatal(err)
	}
	entityBefore, err := os.ReadFile(EntityPath(dir, "person:alice"))
	if err != nil {
		t.Fatal(err)
	}

	// A different embedder, same text.
	second := filepath.Join(tmp, "other_embed.sh")
	if err := os.WriteFile(second, []byte("#!/bin/sh\ncat > /dev/null\necho '[0.9, 0.8, 0.7]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeEmbedder(t, dir, second, []string{"name"}, false)

	res, err := Embed(dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 1 {
		t.Errorf("Embedded = %d after an embedder change, want 1: the checksum matched but the vectors are not comparable", res.Embedded)
	}
	entityAfter, err := os.ReadFile(EntityPath(dir, "person:alice"))
	if err != nil {
		t.Fatal(err)
	}
	if string(entityBefore) != string(entityAfter) {
		t.Error("changing the embedder changed an entity file")
	}
	vs := openSidecar(t, dir)
	got, _ := vs.Vector("person:alice", "name")
	if got[0] != 0.9 {
		t.Errorf("sidecar vector = %v, want the new embedder's output", got)
	}
}

// --- StripLegacyEmbeddings ---

func TestStripLegacyEmbeddings(t *testing.T) {
	dir := t.TempDir()
	withVec := &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{
		"name":           "Alice",
		"name_embedding": []any{0.1, 0.2, 0.3},
	}}
	plain := &Entity{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": "Bob"}}
	for _, e := range []*Entity{withVec, plain} {
		if err := WriteEntity(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	bobBefore, err := os.ReadFile(EntityPath(dir, "person:bob"))
	if err != nil {
		t.Fatal(err)
	}

	changed, _, err := StripLegacyEmbeddings(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Errorf("dry run reported %d changed, want 1", changed)
	}
	if data, _ := os.ReadFile(EntityPath(dir, "person:alice")); !strings.Contains(string(data), "name_embedding") {
		t.Error("dry run rewrote a file")
	}

	changed, _, err = StripLegacyEmbeddings(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Errorf("changed = %d, want 1", changed)
	}
	data, err := os.ReadFile(EntityPath(dir, "person:alice"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "name_embedding") {
		t.Error("embedding attribute survived the strip")
	}
	if !strings.Contains(string(data), "Alice") {
		t.Error("the strip took content with it")
	}
	bobAfter, err := os.ReadFile(EntityPath(dir, "person:bob"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bobBefore) != string(bobAfter) {
		t.Error("the strip rewrote a file that had no embedding")
	}
}

// TestMigrateLegacyVectors moves vectors from the files into the sidecar
// without a model run, which is what makes splitting a corpus cheap.
func TestMigrateLegacyVectors(t *testing.T) {
	dir := t.TempDir()
	e := &Entity{ID: "game:tetris", Type: "game", Attributes: map[string]any{
		"name":                  "Tetris",
		"description":           "falling blocks",
		"name_embedding":        []any{0.1, 0.2, 0.3},
		"description_embedding": []any{0.4, 0.5, 0.6},
	}}
	if err := WriteEntity(dir, e); err != nil {
		t.Fatal(err)
	}

	dry, err := MigrateLegacyVectors(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Harvested != 2 || dry.Files != 1 || dry.Dim != 3 {
		t.Errorf("dry run: Harvested=%d Files=%d Dim=%d, want 2, 1, 3", dry.Harvested, dry.Files, dry.Dim)
	}
	if _, err := os.Stat(vector.BinPath(CacheDir(dir))); !os.IsNotExist(err) {
		t.Error("a dry run wrote a sidecar")
	}

	res, err := MigrateLegacyVectors(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Harvested != 2 {
		t.Fatalf("Harvested = %d, want 2", res.Harvested)
	}
	if res.Bytes <= 0 {
		t.Errorf("Bytes = %d, want the files to have shrunk", res.Bytes)
	}

	data, err := os.ReadFile(EntityPath(dir, "game:tetris"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "_embedding") {
		t.Error("a vector survived in the entity file")
	}

	vs, err := vector.Open(CacheDir(dir), "")
	if err != nil {
		t.Fatalf("opening the migrated sidecar: %v", err)
	}
	defer vs.Close()
	got, ok := vs.Vector("game:tetris", "description")
	if !ok {
		t.Fatal("the harvested vector is not in the sidecar")
	}
	if got[0] != 0.4 {
		t.Errorf("harvested vector = %v, want [0.4 0.5 0.6]", got)
	}
	// The checksum ties the vector to the text it came from, so the next sync
	// carries it rather than paying for it again.
	entry, _ := vs.Entry("game:tetris", "description")
	if entry.Checksum != TextChecksum("falling blocks") {
		t.Error("the harvested vector was not tied to its source text")
	}
}

// TestMigrateLegacyVectors_IdPathMismatch is a regression from the real
// corpus: hundreds of files there declare an id that does not match where they
// sit. A migration that writes to the path the id implies skips those files and
// overwrites unrelated ones.
func TestMigrateLegacyVectors_IdPathMismatch(t *testing.T) {
	dir := t.TempDir()
	misplaced := filepath.Join(dir, "entities", "game", "wrong_name.yaml")
	if err := os.MkdirAll(filepath.Dir(misplaced), 0o755); err != nil {
		t.Fatal(err)
	}
	// The file is at .../wrong_name.yaml but calls itself game:right_name.
	content := "id: game:right_name\ntype: game\nattributes:\n    name: Right\n    name_embedding:\n        - 0.1\n        - 0.2\n"
	if err := os.WriteFile(misplaced, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := MigrateLegacyVectors(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Harvested != 1 {
		t.Errorf("Harvested = %d, want 1", res.Harvested)
	}

	data, err := os.ReadFile(misplaced)
	if err != nil {
		t.Fatalf("the misplaced file was not rewritten in place: %v", err)
	}
	if strings.Contains(string(data), "_embedding") {
		t.Error("the vector survived in the misplaced file: the migration wrote to the id's path, not the file's")
	}
	if _, err := os.Stat(filepath.Join(dir, "entities", "game", "right_name.yaml")); err == nil {
		t.Error("the migration created a second file at the id's path instead of rewriting the one it read")
	}
}

// TestEmbed_NamedSetDoesNotWalkTheCorpus: naming entities must cost those
// entities, not the corpus. A write goes through here, so if this walks
// everything then every `kg create` on a large graph pays for it.
func TestEmbed_NamedSetDoesNotWalkTheCorpus(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	var seed []*Entity
	for i := range 20 {
		seed = append(seed, &Entity{
			ID:         fmt.Sprintf("person:p%02d", i),
			Type:       "person",
			Attributes: map[string]any{"name": fmt.Sprintf("Person %d", i)},
		})
	}
	dir := setupEmbedDir(t, seed, cmd, []string{"name"}, false)
	if _, err := Embed(dir, false, false); err != nil {
		t.Fatal(err)
	}

	// Make one entity unreadable from disk in a way a full walk would notice
	// and a targeted read would not: a file that does not parse.
	broken := filepath.Join(dir, "entities", "person", "broken.yaml")
	if err := os.WriteFile(broken, []byte("id: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Change one entity and name it. A targeted pass must not have looked at
	// the malformed file at all.
	seed[3].Attributes["name"] = "Renamed"
	if err := WriteEntity(dir, seed[3]); err != nil {
		t.Fatal(err)
	}
	res, err := Embed(dir, false, false, seed[3].ID)
	if err != nil {
		t.Fatalf("targeted embed: %v", err)
	}
	if res.Embedded != 1 {
		t.Errorf("Embedded = %d, want 1", res.Embedded)
	}
	if res.Carried != 19 {
		t.Errorf("Carried = %d, want the other 19 rows", res.Carried)
	}
	if res.Dropped != 0 {
		t.Errorf("Dropped = %d, want 0", res.Dropped)
	}

	vs := openSidecar(t, dir)
	if vs.Len() != 20 {
		t.Errorf("sidecar holds %d rows, want 20 — a targeted pass must leave a complete sidecar", vs.Len())
	}
}

// A named entity that has been deleted loses its rows, and nothing else does.
func TestEmbed_NamedSetDropsDeleted(t *testing.T) {
	tmp := t.TempDir()
	cmd := writeMockEmbedScript(t, tmp)
	dir := setupEmbedDir(t, []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": "Bob"}},
	}, cmd, []string{"name"}, false)
	if _, err := Embed(dir, false, false); err != nil {
		t.Fatal(err)
	}
	if err := DeleteEntity(dir, "person:bob"); err != nil {
		t.Fatal(err)
	}
	res, err := Embed(dir, false, false, "person:bob")
	if err != nil {
		t.Fatal(err)
	}
	if res.Dropped != 1 {
		t.Errorf("Dropped = %d, want 1", res.Dropped)
	}
	vs := openSidecar(t, dir)
	if _, ok := vs.Vector("person:bob", "name"); ok {
		t.Error("the deleted entity kept its vector")
	}
	if _, ok := vs.Vector("person:alice", "name"); !ok {
		t.Error("an unrelated entity lost its vector")
	}
}
