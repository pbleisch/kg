package store

import (
	"os"
	"path/filepath"
	"testing"
)

// --- EmbedManifest round-trip ---

func TestLoadEmbedManifest_Empty(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadEmbedManifest(dir)
	if err != nil {
		t.Fatalf("LoadEmbedManifest: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("expected version 1, got %d", m.Version)
	}
	if len(m.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(m.Entries))
	}
}

func TestSaveLoadEmbedManifest_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := &EmbedManifest{
		Version: 1,
		Entries: map[string]EmbedEntrySet{
			"person:alice": {
				"name": EmbedEntry{Checksum: "abc123", Dim: 384},
			},
		},
	}
	if err := SaveEmbedManifest(dir, m); err != nil {
		t.Fatalf("SaveEmbedManifest: %v", err)
	}

	loaded, err := LoadEmbedManifest(dir)
	if err != nil {
		t.Fatalf("LoadEmbedManifest: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("version: got %d, want 1", loaded.Version)
	}
	entry, ok := loaded.Entries["person:alice"]["name"]
	if !ok {
		t.Fatal("missing entry for person:alice/name")
	}
	if entry.Checksum != "abc123" || entry.Dim != 384 {
		t.Errorf("entry mismatch: %+v", entry)
	}
}

func TestLoadEmbedManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".cache")
	os.MkdirAll(cacheDir, 0o755)
	os.WriteFile(filepath.Join(cacheDir, "embed-manifest.json"), []byte("{invalid"), 0o644)

	_, err := LoadEmbedManifest(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadEmbedManifest_NilEntries(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".cache")
	os.MkdirAll(cacheDir, 0o755)
	os.WriteFile(filepath.Join(cacheDir, "embed-manifest.json"), []byte(`{"version":1}`), 0o644)

	m, err := LoadEmbedManifest(dir)
	if err != nil {
		t.Fatalf("LoadEmbedManifest: %v", err)
	}
	if m.Entries == nil {
		t.Fatal("expected non-nil Entries map")
	}
}

// --- TextChecksum ---

func TestTextChecksum_Deterministic(t *testing.T) {
	a := TextChecksum("hello world")
	b := TextChecksum("hello world")
	if a != b {
		t.Fatalf("checksums differ for same input: %s vs %s", a, b)
	}
}

func TestTextChecksum_Differs(t *testing.T) {
	a := TextChecksum("hello")
	b := TextChecksum("world")
	if a == b {
		t.Fatal("expected different checksums for different inputs")
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

func setupEmbedDir(t *testing.T, entities []*Entity, embedCmd string, fields []string, batch bool) string {
	t.Helper()
	dir := t.TempDir()
	for _, e := range entities {
		if err := WriteEntity(dir, e); err != nil {
			t.Fatalf("WriteEntity(%s): %v", e.ID, err)
		}
	}

	// Build YAML config with embedding section.
	fieldList := ""
	for i, f := range fields {
		if i > 0 {
			fieldList += ", "
		}
		fieldList += f
	}
	batchStr := "false"
	if batch {
		batchStr = "true"
	}
	configYAML := "backend: ladybug\nembedding:\n  command: \"" + embedCmd + "\"\n  fields: [" + fieldList + "]\n  batch: " + batchStr + "\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configYAML), 0o644)
	return dir
}

func TestEmbed_NoConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := Embed(dir, false, false)
	if err == nil {
		t.Fatal("expected error when no embedding configured")
	}
}

func TestEmbed_DryRun(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": "Bob"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	result, err := Embed(dir, false, true)
	if err != nil {
		t.Fatalf("Embed dry-run: %v", err)
	}
	if result.Embedded != 2 {
		t.Errorf("dry-run expected 2 to embed, got %d", result.Embedded)
	}

	// Verify no _embedding attrs were actually written.
	e, _ := ReadEntity(dir, "person:alice")
	if _, ok := e.Attributes["name_embedding"]; ok {
		t.Error("dry-run should not write embeddings")
	}
}

func TestEmbed_SingleMode(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": "Bob"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	result, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if result.Embedded != 2 {
		t.Errorf("expected 2 embedded, got %d", result.Embedded)
	}
	if result.Errors != 0 {
		t.Errorf("expected 0 errors, got %d: %v", result.Errors, result.Warnings)
	}

	// Verify embedding attribute was written.
	e, err := ReadEntity(dir, "person:alice")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if _, ok := e.Attributes["name_embedding"]; !ok {
		t.Error("expected name_embedding attribute")
	}

	// Verify manifest was updated.
	m, _ := LoadEmbedManifest(dir)
	if _, ok := m.Entries["person:alice"]["name"]; !ok {
		t.Error("expected manifest entry for person:alice/name")
	}
}

func TestEmbed_BatchMode(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockBatchScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": "Bob"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, true)

	result, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("Embed batch: %v", err)
	}
	if result.Embedded != 2 {
		t.Errorf("expected 2 embedded, got %d", result.Embedded)
	}

	// Verify both entities got embeddings.
	for _, id := range []string{"person:alice", "person:bob"} {
		e, _ := ReadEntity(dir, id)
		if _, ok := e.Attributes["name_embedding"]; !ok {
			t.Errorf("%s: missing name_embedding", id)
		}
	}
}

func TestEmbed_SkipsUnchanged(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	// First embed.
	r1, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	if r1.Embedded != 1 {
		t.Fatalf("first: expected 1 embedded, got %d", r1.Embedded)
	}

	// Second embed — entity unchanged, should skip.
	r2, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("second Embed: %v", err)
	}
	if r2.Embedded != 0 {
		t.Errorf("second: expected 0 embedded, got %d", r2.Embedded)
	}
	if r2.Skipped != 1 {
		t.Errorf("second: expected 1 skipped, got %d", r2.Skipped)
	}
}

func TestEmbed_RebuildReembeds(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	// First embed.
	Embed(dir, false, false)

	// Rebuild should re-embed even though content hasn't changed.
	r, err := Embed(dir, true, false)
	if err != nil {
		t.Fatalf("rebuild Embed: %v", err)
	}
	if r.Embedded != 1 {
		t.Errorf("rebuild: expected 1 embedded, got %d", r.Embedded)
	}
}

func TestEmbed_SkipsMissingField(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"age": "30"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	result, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if result.Embedded != 0 {
		t.Errorf("expected 0 embedded (field missing), got %d", result.Embedded)
	}
}

func TestEmbed_SkipsEmptyField(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": ""}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	result, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if result.Embedded != 0 {
		t.Errorf("expected 0 embedded (empty field), got %d", result.Embedded)
	}
}

func TestEmbed_SpecificEntityIDs(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
		{ID: "person:bob", Type: "person", Attributes: map[string]any{"name": "Bob"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	result, err := Embed(dir, false, false, "person:alice")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if result.Embedded != 1 {
		t.Errorf("expected 1 embedded, got %d", result.Embedded)
	}

	// Only alice should have embedding.
	alice, _ := ReadEntity(dir, "person:alice")
	if _, ok := alice.Attributes["name_embedding"]; !ok {
		t.Error("alice: missing name_embedding")
	}
	bob, _ := ReadEntity(dir, "person:bob")
	if _, ok := bob.Attributes["name_embedding"]; ok {
		t.Error("bob: should not have name_embedding")
	}
}

func TestEmbed_MultipleFields(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{
			"name":        "Alice",
			"description": "A person named Alice",
		}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name", "description"}, false)

	result, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if result.Embedded != 2 {
		t.Errorf("expected 2 embedded (2 fields), got %d", result.Embedded)
	}

	e, _ := ReadEntity(dir, "person:alice")
	if _, ok := e.Attributes["name_embedding"]; !ok {
		t.Error("missing name_embedding")
	}
	if _, ok := e.Attributes["description_embedding"]; !ok {
		t.Error("missing description_embedding")
	}
}

func TestEmbed_CommandFailure(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockFailScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	result, err := Embed(dir, false, false)
	if err != nil {
		t.Fatalf("Embed should not return top-level error for per-entity failures: %v", err)
	}
	if result.Errors != 1 {
		t.Errorf("expected 1 error, got %d", result.Errors)
	}
	if result.Embedded != 0 {
		t.Errorf("expected 0 embedded, got %d", result.Embedded)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warnings for failed embed")
	}
}

// --- syncEmbed ---

func TestSyncEmbed_NoConfig(t *testing.T) {
	dir := t.TempDir()
	got, _ := syncEmbed(dir, false)
	if got != 0 {
		t.Errorf("expected 0 embedded without config, got %d", got)
	}
}

func TestSyncEmbed_WithEntities(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	got, _ := syncEmbed(dir, false)
	if got != 1 {
		t.Errorf("expected 1 embedded, got %d", got)
	}
}

func TestSyncEmbed_ReturnsZeroOnFailure(t *testing.T) {
	dir := t.TempDir()
	// Config with a non-existent command.
	configYAML := "embedding:\n  command: /nonexistent/embed\n  fields: [name]\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configYAML), 0o644)
	os.MkdirAll(filepath.Join(dir, "entities", "person"), 0o755)
	WriteEntity(dir, &Entity{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}})

	got, _ := syncEmbed(dir, false)
	if got != 0 {
		t.Errorf("expected 0 on command failure, got %d", got)
	}
}

// --- Sync integration ---

func TestSyncWithResult_IncludesEmbedded(t *testing.T) {
	scriptDir := t.TempDir()
	cmd := writeMockEmbedScript(t, scriptDir)

	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
	}
	dir := setupEmbedDir(t, entities, cmd, []string{"name"}, false)

	result, err := SyncWithResult(dir)
	if err != nil {
		t.Fatalf("SyncWithResult: %v", err)
	}
	if result.Synced < 1 {
		t.Errorf("expected at least 1 synced, got %d", result.Synced)
	}
	if result.Embedded != 1 {
		t.Errorf("expected 1 embedded, got %d", result.Embedded)
	}
}

func TestSyncWithResult_NoEmbeddingConfig(t *testing.T) {
	entities := []*Entity{
		{ID: "person:alice", Type: "person", Attributes: map[string]any{"name": "Alice"}},
	}
	dir := setupKGDir(t, entities)

	result, err := SyncWithResult(dir)
	if err != nil {
		t.Fatalf("SyncWithResult: %v", err)
	}
	if result.Embedded != 0 {
		t.Errorf("expected 0 embedded without config, got %d", result.Embedded)
	}
}
