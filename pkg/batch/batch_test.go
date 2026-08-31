package batch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

func setupKG(t *testing.T) string {
	t.Helper()
	kgDir := filepath.Join(t.TempDir(), ".kg")
	if err := os.MkdirAll(filepath.Join(kgDir, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	return kgDir
}

func env(t *testing.T, key string) Envelope {
	t.Helper()
	e, err := NewEnvelope(Version, key)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	return e
}

func entity(id, typeName string, attrs map[string]any) *store.Entity {
	return &store.Entity{ID: id, Type: typeName, Attributes: attrs}
}

// graphBytes reads every entity file under kgDir, so two graphs can be
// compared byte for byte.
func graphBytes(t *testing.T, kgDir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join(kgDir, "entities")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		out[rel] = string(data)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walking graph: %v", err)
	}
	return out
}

func codes(errs []RecordError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Code
	}
	return out
}

func TestEnvelopeIsRequired(t *testing.T) {
	kgDir := setupKG(t)
	if _, err := Apply(kgDir, Envelope{}, nil, Options{}); err == nil {
		t.Fatal("a zero envelope must be refused")
	}
	if _, err := NewEnvelope(2, "k"); err == nil {
		t.Fatal("an unsupported version must be refused")
	}
	if _, err := NewEnvelope(Version, ""); err == nil {
		t.Fatal("a missing idempotency key must be refused")
	}
	if _, err := NewEnvelope(Version, "has/slash"); err == nil {
		t.Fatal("a key that is not filesystem-safe must be refused")
	}
}

// --dir=.kg was an attribute on 2,118 entities.  It must now be refused at the
// boundary, naming the field, with nothing written.
func TestRefusesIllegalAttributeName(t *testing.T) {
	kgDir := setupKG(t)
	_, err := Apply(kgDir, env(t, "k1"), []Record{{
		Op:     OpUpsert,
		Entity: entity("concept:flow_state", "concept", map[string]any{"name": "Flow", "--dir": ".kg"}),
	}}, Options{})
	errs, refused := Refused(err)
	if !refused {
		t.Fatalf("expected a structural refusal, got %v", err)
	}
	if got := codes(errs); len(got) != 1 || got[0] != "illegal_attribute" {
		t.Fatalf("codes = %v, want [illegal_attribute]", got)
	}
	if !strings.Contains(errs[0].Error(), "--dir") {
		t.Errorf("the refusal must name the offending field, got %q", errs[0].Error())
	}
	if len(graphBytes(t, kgDir)) != 0 {
		t.Error("a refused batch must write nothing")
	}
}

func TestRefusesDanglingRelationTarget(t *testing.T) {
	kgDir := setupKG(t)
	_, err := Apply(kgDir, env(t, "k2"), []Record{
		{Op: OpUpsert, Entity: entity("concept:flow_state", "concept", map[string]any{"name": "Flow"})},
		{Op: OpLink, From: "concept:flow_state", Rel: "enables", To: "concept:never_declared"},
	}, Options{})
	errs, refused := Refused(err)
	if !refused {
		t.Fatalf("expected a structural refusal, got %v", err)
	}
	if got := codes(errs); len(got) != 1 || got[0] != "dangling_relation" {
		t.Fatalf("codes = %v, want [dangling_relation]", got)
	}
	if len(graphBytes(t, kgDir)) != 0 {
		t.Error("a refused batch must write nothing, including the valid records")
	}
}

func TestRefusesIdentityCollisionNamingBoth(t *testing.T) {
	kgDir := setupKG(t)
	// Two records claiming one identity — including via normalization, which
	// folds "Concept:Flow-State" onto the same file.
	_, err := Apply(kgDir, env(t, "k3"), []Record{
		{ID: "first", Op: OpUpsert,
			Entity: entity("concept:flow_state", "concept", map[string]any{"name": "Flow"})},
		{ID: "second", Op: OpUpsert,
			Entity: entity("Concept:Flow-State", "Concept", map[string]any{"name": "Flow again"})},
	}, Options{})
	errs, refused := Refused(err)
	if !refused {
		t.Fatalf("expected a structural refusal, got %v", err)
	}
	if got := codes(errs); len(got) != 1 || got[0] != "identity_collision" {
		t.Fatalf("codes = %v, want [identity_collision]", got)
	}
	msg := errs[0].Error()
	for _, want := range []string{"first", "second"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must name both records, got %q", msg)
		}
	}
	if len(graphBytes(t, kgDir)) != 0 {
		t.Error("a refused batch must write nothing")
	}
}

func TestRefusesTypeDisagreement(t *testing.T) {
	kgDir := setupKG(t)
	_, err := Apply(kgDir, env(t, "k4"), []Record{{
		Op:     OpUpsert,
		Entity: entity("concept:flow_state", "game", map[string]any{"name": "Flow"}),
	}}, Options{})
	errs, refused := Refused(err)
	if !refused {
		t.Fatalf("expected a structural refusal, got %v", err)
	}
	if got := codes(errs); len(got) != 1 || got[0] != "type_disagreement" {
		t.Fatalf("codes = %v, want [type_disagreement]", got)
	}
}

func TestRefusesUnknownManifestField(t *testing.T) {
	_, _, err := DecodeManifest([]byte("version: 1\nactions: []\nsurprise: yes\n"))
	if err == nil {
		t.Fatal("a manifest key the shape does not declare must be refused")
	}
}

// Applying the same batch twice leaves the graph byte-identical the second time.
func TestApplyTwiceIsIdempotent(t *testing.T) {
	kgDir := setupKG(t)
	records := []Record{
		{Op: OpUpsert, Entity: entity("concept:flow_state", "concept", map[string]any{"name": "Flow"})},
		{Op: OpUpsert, Entity: entity("game:pac_man", "game", map[string]any{"name": "Pac-Man"})},
		{Op: OpLink, From: "game:pac_man", Rel: "exemplifies", To: "concept:flow_state"},
	}
	if _, err := Apply(kgDir, env(t, "same.key"), records, Options{}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first := graphBytes(t, kgDir)

	res, err := Apply(kgDir, env(t, "same.key"), records, Options{})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !res.Replayed {
		t.Error("the second apply under one key must report a replay")
	}
	if diff := len(first) != len(graphBytes(t, kgDir)); diff {
		t.Fatal("the graph changed on replay")
	}
	for path, want := range first {
		if got := graphBytes(t, kgDir)[path]; got != want {
			t.Errorf("%s changed on replay", path)
		}
	}

	// Even under a fresh key — no ledger to lean on — the records converge.
	if _, err := Apply(kgDir, env(t, "other.key"), records, Options{OnConflict: ConflictSkip}); err != nil {
		t.Fatalf("third apply: %v", err)
	}
	for path, want := range first {
		if got := graphBytes(t, kgDir)[path]; got != want {
			t.Errorf("%s changed when the same records were re-applied under a new key", path)
		}
	}
}

// on-error fail makes the batch one transaction: a record that fails part-way
// puts the whole graph back.
func TestFailedBatchRollsBack(t *testing.T) {
	kgDir := setupKG(t)
	if _, err := Apply(kgDir, env(t, "seed"), []Record{
		{Op: OpUpsert, Entity: entity("concept:existing", "concept", map[string]any{"name": "Existing"})},
	}, Options{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before := graphBytes(t, kgDir)

	// The delete resolves structurally (nothing checks a delete target's
	// existence up front) but fails when applied, aborting the batch.
	_, err := Apply(kgDir, env(t, "doomed"), []Record{
		{Op: OpUpsert, Entity: entity("concept:added", "concept", map[string]any{"name": "Added"})},
		{Op: OpDelete, Target: "concept:not_there"},
	}, Options{OnError: ErrorFail})
	if err == nil {
		t.Fatal("expected the batch to fail")
	}
	after := graphBytes(t, kgDir)
	if len(after) != len(before) {
		t.Fatalf("graph has %d files after rollback, want %d", len(after), len(before))
	}
	for path, want := range before {
		if after[path] != want {
			t.Errorf("%s was not restored", path)
		}
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	kgDir := setupKG(t)
	res, err := Apply(kgDir, env(t, "dry"), []Record{
		{Op: OpUpsert, Entity: entity("concept:flow_state", "concept", map[string]any{"name": "Flow"})},
	}, Options{DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if res.Counts["upsert"] != 1 {
		t.Errorf("counts = %v, want one upsert", res.Counts)
	}
	if len(graphBytes(t, kgDir)) != 0 {
		t.Error("a dry run must write nothing")
	}
}

// The same records applied through the library and through a manifest leave a
// byte-identical graph.
func TestLibraryAndManifestAgree(t *testing.T) {
	viaLib := setupKG(t)
	viaManifest := setupKG(t)

	records := []Record{
		{Op: OpUpsert, Entity: entity("concept:flow_state", "concept", map[string]any{"name": "Flow"})},
		{Op: OpUpsert, Entity: entity("game:pac_man", "game", map[string]any{"name": "Pac-Man"})},
		{Op: OpLink, From: "game:pac_man", Rel: "exemplifies", To: "concept:flow_state"},
	}
	if _, err := Apply(viaLib, env(t, "lib"), records, Options{}); err != nil {
		t.Fatalf("library apply: %v", err)
	}

	manifest := `version: 1
idempotency_key: manifest
actions:
  - action: upsert
    entity: {id: "concept:flow_state", type: concept, attributes: {name: Flow}}
  - action: upsert
    entity: {id: "game:pac_man", type: game, attributes: {name: Pac-Man}}
  - action: link
    from: "game:pac_man"
    rel: exemplifies
    to: "concept:flow_state"
`
	menv, mrecords, err := DecodeManifest([]byte(manifest))
	if err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	if _, err := Apply(viaManifest, menv, mrecords, Options{}); err != nil {
		t.Fatalf("manifest apply: %v", err)
	}

	lib, man := graphBytes(t, viaLib), graphBytes(t, viaManifest)
	if len(lib) != len(man) {
		t.Fatalf("library wrote %d files, manifest wrote %d", len(lib), len(man))
	}
	for path, want := range lib {
		if man[path] != want {
			t.Errorf("%s differs:\nlibrary:\n%s\nmanifest:\n%s", path, want, man[path])
		}
	}
}

func TestSetRefusesUnresolvedTarget(t *testing.T) {
	kgDir := setupKG(t)
	_, err := Apply(kgDir, env(t, "k5"), []Record{
		{Op: OpSet, Target: "concept:ghost", Attributes: map[string]any{"name": "Ghost"}},
	}, Options{})
	if _, refused := Refused(err); !refused {
		t.Fatalf("expected a structural refusal, got %v", err)
	}
}

func TestContentProblemsAreWarnings(t *testing.T) {
	kgDir := setupKG(t)
	if err := os.MkdirAll(filepath.Join(kgDir, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	schemaYAML := "type: concept\nattributes:\n  name:\n    type: string\n    required: true\n"
	if err := os.WriteFile(filepath.Join(kgDir, "schema", "concept.yaml"), []byte(schemaYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(kgDir, env(t, "k6"), []Record{
		{Op: OpUpsert, Entity: entity("concept:unnamed", "concept", map[string]any{"note": "no name"})},
	}, Options{})
	if err != nil {
		t.Fatalf("a content problem must not block: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Error("a missing required attribute should be reported as a warning")
	}
	if _, err := store.ReadEntity(kgDir, "concept:unnamed"); err != nil {
		t.Error("a graph full of warnings still writes")
	}
}

func TestProducersEnterThroughTheBoundary(t *testing.T) {
	kgDir := setupKG(t)
	if _, err := CreateEntity(kgDir, "Person", "charlie", map[string]string{"name": "Charlie"}, false); err != nil {
		t.Fatalf("CreateEntity: %v", err)
	}
	if _, err := CreateEntity(kgDir, "Person", "charlie", map[string]string{"name": "Again"}, false); err == nil {
		t.Error("a second create of one identity must still be refused")
	}
	if _, err := CreateEntity(kgDir, "Person", "dana", map[string]string{"--dir": ".kg"}, false); err == nil {
		t.Error("an illegal attribute name must be refused through the producer too")
	}
	if err := LinkRef(kgDir, "person:charlie", "worksOn", "project:atlas"); err == nil {
		t.Error("a link to an entity that does not exist must be refused")
	}
	if _, err := SetAttrs(kgDir, "person:charlie", map[string]string{"role": "Designer"}); err != nil {
		t.Fatalf("SetAttrs: %v", err)
	}
	e, err := store.ReadEntity(kgDir, "person:charlie")
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if e.Attributes["role"] != "Designer" {
		t.Errorf("role = %v, want Designer", e.Attributes["role"])
	}
	if e.Attributes["name"] != "Charlie" {
		t.Errorf("name = %v, want Charlie (set must preserve)", e.Attributes["name"])
	}
}

func TestCoercesNumericAttributeFromSchema(t *testing.T) {
	kgDir := setupKG(t)
	if err := os.MkdirAll(filepath.Join(kgDir, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	schemaYAML := "type: source\nattributes:\n  year:\n    type: number\n    required: false\n"
	if err := os.WriteFile(filepath.Join(kgDir, "schema", "source.yaml"), []byte(schemaYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := CreateEntity(kgDir, "Source", "myid", map[string]string{"year": "2004"}, false)
	if err != nil {
		t.Fatalf("CreateEntity: %v", err)
	}
	switch v := e.Attributes["year"].(type) {
	case int, int64, float64:
		_ = v
	default:
		t.Errorf("year has type %T, want numeric", e.Attributes["year"])
	}
}
