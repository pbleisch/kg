package batch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pbleisch/kg/pkg/store"
)

// writeSchema puts a type's schema in the graph.  These tests are about a rule
// the schema decides, so most of them need one.
func writeSchema(t *testing.T, kgDir, typeName, body string) {
	t.Helper()
	dir := filepath.Join(kgDir, "schema")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, typeName+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// conceptSchema declares the two field kinds the rule distinguishes.
const conceptSchema = `type: concept
attributes:
  name:
    type: string
  description:
    type: string
  evidence:
    type: list
  categories:
    type: list
`

func envFrom(t *testing.T, key, contributor string) Envelope {
	t.Helper()
	e, err := NewEnvelopeFrom(Version, key, contributor)
	if err != nil {
		t.Fatalf("NewEnvelopeFrom: %v", err)
	}
	return e
}

func read(t *testing.T, kgDir, id string) *store.Entity {
	t.Helper()
	e, err := store.ReadEntity(kgDir, id)
	if err != nil {
		t.Fatalf("reading %s: %v", id, err)
	}
	return e
}

// items renders an attribute as "source|text" pairs, sorted the way the rule
// leaves them, so a test can state what it expects in one line.
func items(t *testing.T, e *store.Entity, attr string) []string {
	t.Helper()
	var out []string
	for _, it := range store.DecodeItems(e.Attributes[attr]) {
		out = append(out, it.Source+"|"+it.TextString())
	}
	return out
}

func evidence(text, src string) map[string]any {
	return map[string]any{store.ItemTextKey: text, store.ItemSourceKey: src}
}

// The pilot's blocker: two batches of one document name one entity, and the
// whole document was refused after the model had been paid for.  It applies
// now, and both batches' contributions are in the file.
func TestFoldsTwoRecordsClaimingOneIdentity(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)

	res, err := Apply(kgDir, envFrom(t, "doc1", "source:mda"), []Record{
		{ID: "batch0", Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"name":       "Flow",
			"evidence":   []any{"challenge meets skill"},
			"categories": "experience",
		})},
		{ID: "batch1", Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"name":     "Flow",
			"evidence": []any{"the channel between anxiety and boredom"},
		})},
	}, Options{})
	if err != nil {
		t.Fatalf("a document whose batches name one entity must apply: %v", err)
	}
	if res.TotalRecords != 1 {
		t.Errorf("two records claiming one identity must fold into one, got %d", res.TotalRecords)
	}

	e := read(t, kgDir, "concept:flow")
	got := items(t, e, "evidence")
	want := []string{
		"source:mda|challenge meets skill",
		"source:mda|the channel between anxiety and boredom",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("evidence = %v, want %v — the second batch must not delete the first's", got, want)
	}
	// A scalar arriving at a list-declared field is a one-element list.
	if got := items(t, e, "categories"); !reflect.DeepEqual(got, []string{"source:mda|experience"}) {
		t.Errorf("categories = %v, want one attributed item", got)
	}

	var folded bool
	for _, w := range res.Warnings {
		if w.Code == "records_folded" {
			folded = true
			if !strings.Contains(w.Message, "concept:flow") {
				t.Errorf("the fold must name what it folded, got %q", w.Message)
			}
		}
	}
	if !folded {
		t.Error("a fold must be reported, not silent")
	}
}

// P5's own check, for a re-ingest rather than a retry: the ledger cannot be
// what makes this converge, so the second apply carries a different key —
// which is what a re-extraction of one document produces.
func TestReingestingTheSameBatchLeavesTheGraphUnchanged(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)

	recs := []Record{
		{Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"name":        "Flow",
			"description": "the channel between anxiety and boredom",
			"evidence":    []any{"challenge meets skill", "a state of absorption"},
			"categories":  []any{"experience", "psychology"},
		})},
		{Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"evidence": []any{"challenge meets skill"},
		})},
	}

	if _, err := Apply(kgDir, envFrom(t, "ingest.one", "source:mda"), recs, Options{}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first := graphBytes(t, kgDir)

	if _, err := Apply(kgDir, envFrom(t, "ingest.two", "source:mda"), recs, Options{}); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	second := graphBytes(t, kgDir)

	if !reflect.DeepEqual(first, second) {
		t.Errorf("re-ingesting one document must leave the graph unchanged\nfirst:  %v\nsecond: %v", first, second)
	}
}

// Replace-by-contributor: a re-ingest rewrites its own account and leaves
// every other contributor's standing.  This is the reason the unit of reset is
// the contributor and not the field.
func TestUpsertReplacesOnlyItsOwnContributorsItems(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)

	apply := func(key, contributor string, texts ...string) {
		t.Helper()
		vals := make([]any, len(texts))
		for i, s := range texts {
			vals[i] = s
		}
		_, err := Apply(kgDir, envFrom(t, key, contributor), []Record{{
			Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
				"name": "Flow", "evidence": vals,
			}),
		}}, Options{})
		if err != nil {
			t.Fatalf("apply %s: %v", key, err)
		}
	}

	apply("a1", "source:mda", "mda's first reading")
	apply("b1", "source:csik", "csik's reading")
	// source:mda is re-extracted and says something different.
	apply("a2", "source:mda", "mda's corrected reading")

	got := items(t, read(t, kgDir, "concept:flow"), "evidence")
	want := []string{
		"source:csik|csik's reading",
		"source:mda|mda's corrected reading",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("evidence = %v, want %v — a contributor replaces its own items and nobody else's", got, want)
	}
}

// A hand-authored item names no source, so no contributor's write can ever
// replace it.  Only `unset` removes it — which is the whole reason `unset`
// exists (ADR-0030).
func TestUnattributedItemSurvivesReingestAndOnlyUnsetRemovesIt(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)

	// Someone types it by hand: no contributor on the envelope.
	res, err := Apply(kgDir, env(t, "byhand"), []Record{{
		Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"name": "Flow", "evidence": []any{"a note somebody typed"},
		}),
	}}, Options{})
	if err != nil {
		t.Fatalf("a batch with no contributor must write, not refuse: %v", err)
	}
	var warned bool
	for _, w := range res.Warnings {
		if w.Code == "unattributed_items" {
			warned = true
		}
	}
	if !warned {
		t.Error("a batch with no contributor must warn that its items are permanent")
	}

	// Two re-ingests by a real contributor, and the hand-authored note stands.
	for _, key := range []string{"i1", "i2"} {
		if _, err := Apply(kgDir, envFrom(t, key, "source:mda"), []Record{{
			Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
				"evidence": []any{"mda's reading"},
			}),
		}}, Options{}); err != nil {
			t.Fatalf("re-ingest %s: %v", key, err)
		}
	}
	got := items(t, read(t, kgDir, "concept:flow"), "evidence")
	want := []string{"|a note somebody typed", "source:mda|mda's reading"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence = %v, want %v — an unattributed item is never replaced", got, want)
	}

	// And unset is what removes it.
	if _, err := UnsetAttrs(kgDir, "concept:flow", []string{"evidence"}); err != nil {
		t.Fatalf("unset: %v", err)
	}
	if _, ok := read(t, kgDir, "concept:flow").Attributes["evidence"]; ok {
		t.Error("unset must remove the attribute")
	}
	// Saying it twice says it once.
	if _, err := UnsetAttrs(kgDir, "concept:flow", []string{"evidence"}); err != nil {
		t.Errorf("unsetting what is not there must not fail: %v", err)
	}
}

// The scalar rule: the incoming value wins, and the loss is reported rather
// than silent.  1,610 entities in the real corpus carry two or more sources
// and one description, and nothing recorded what the others said.
func TestScalarOverwriteIsReported(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)

	if _, err := Apply(kgDir, envFrom(t, "b1", "source:a"), []Record{{
		Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"name": "Flow", "description": "from batch 1",
		}),
	}}, Options{}); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(kgDir, envFrom(t, "b2", "source:b"), []Record{{
		Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"description": "from batch 2",
		}),
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, kgDir, "concept:flow").Attributes["description"]; got != "from batch 2" {
		t.Errorf("description = %v, want the incoming value", got)
	}
	var warned bool
	for _, w := range res.Warnings {
		if w.Code == "attribute_overwritten" && strings.Contains(w.Message, "from batch 1") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("overwriting a different scalar must be reported, got %v", res.Warnings)
	}

	// Rewriting the same value is not a loss and must not be reported.
	res, err = Apply(kgDir, envFrom(t, "b3", "source:b"), []Record{{
		Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"description": "from batch 2",
		}),
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range res.Warnings {
		if w.Code == "attribute_overwritten" {
			t.Errorf("rewriting the same value is not an overwrite, got %q", w.Message)
		}
	}
}

// A string is never split on a separator.  "design,mechanic" is one item
// holding that string, which is exactly what that value means today; splitting
// it would shred every value that legitimately contains a comma.
func TestScalarAtListFieldIsOneItemAndIsNeverSplit(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)

	if _, err := Apply(kgDir, envFrom(t, "k", "source:a"), []Record{{
		Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"name":       "Flow",
			"categories": "design,mechanic",
			"evidence":   "one sentence, with a comma in it",
		}),
	}}, Options{}); err != nil {
		t.Fatal(err)
	}
	e := read(t, kgDir, "concept:flow")
	if got := items(t, e, "categories"); !reflect.DeepEqual(got, []string{"source:a|design,mechanic"}) {
		t.Errorf("categories = %v, want one item holding the whole string", got)
	}
	if got := items(t, e, "evidence"); !reflect.DeepEqual(got, []string{"source:a|one sentence, with a comma in it"}) {
		t.Errorf("evidence = %v, want one item holding the whole string", got)
	}
}

// An item that already names its own source keeps it: a producer may attribute
// an item to a document other than the one the batch speaks for.
func TestItemKeepsItsOwnSource(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)

	if _, err := Apply(kgDir, envFrom(t, "k", "source:mda"), []Record{{
		Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"name": "Flow",
			"evidence": []any{
				evidence("quoted from elsewhere", "source:csik"),
				"observed here",
			},
		}),
	}}, Options{}); err != nil {
		t.Fatal(err)
	}
	got := items(t, read(t, kgDir, "concept:flow"), "evidence")
	want := []string{"source:csik|quoted from elsewhere", "source:mda|observed here"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("evidence = %v, want %v", got, want)
	}
}

// A list under a scalar-declared field is stored and reported, never refused:
// cardinality is content (P3, ADR-0028).
func TestListAtScalarFieldIsStoredAndWarned(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)

	res, err := Apply(kgDir, envFrom(t, "k", "source:a"), []Record{{
		Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"name": []any{"Flow", "Flow State"},
		}),
	}}, Options{})
	if err != nil {
		t.Fatalf("cardinality must not refuse a batch: %v", err)
	}
	if _, ok := read(t, kgDir, "concept:flow").Attributes["name"].([]any); !ok {
		t.Error("the value must be stored as written")
	}
	var warned bool
	for _, w := range res.Warnings {
		if strings.Contains(w.Message, "expected type string") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("a list at a scalar field must be reported, got %v", res.Warnings)
	}
}

// The unset op's own structural rules.
func TestUnsetValidation(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)
	if _, err := Apply(kgDir, env(t, "seed"), []Record{{
		Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{"name": "Flow"}),
	}}, Options{}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		rec  Record
		code string
	}{
		{"no target", Record{Op: OpUnset, Unset: []string{"name"}}, "missing_target"},
		{"no attributes", Record{Op: OpUnset, Target: "concept:flow"}, "empty_unset"},
		{"illegal name", Record{Op: OpUnset, Target: "concept:flow", Unset: []string{"--dir"}}, "illegal_attribute"},
		{"missing target", Record{Op: OpUnset, Target: "concept:gone", Unset: []string{"name"}}, "unresolved_target"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Apply(kgDir, env(t, "u-"+strings.ReplaceAll(tc.name, " ", "-")), []Record{tc.rec}, Options{})
			errs, refused := Refused(err)
			if !refused {
				t.Fatalf("expected a refusal, got %v", err)
			}
			if got := codes(errs); len(got) != 1 || got[0] != tc.code {
				t.Fatalf("codes = %v, want [%s]", got, tc.code)
			}
		})
	}
}

// `unset` then `upsert` in one manifest is wipe-then-write, and it is atomic
// under the default --on-error=fail.
func TestUnsetThenUpsertInOneBatch(t *testing.T) {
	kgDir := setupKG(t)
	writeSchema(t, kgDir, "concept", conceptSchema)

	if _, err := Apply(kgDir, envFrom(t, "s1", "source:a"), []Record{{
		Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"name": "Flow", "evidence": []any{"an old reading"},
		}),
	}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(kgDir, envFrom(t, "s2", "source:b"), []Record{
		{Op: OpUnset, Target: "concept:flow", Unset: []string{"evidence"}},
		{Op: OpUpsert, Entity: entity("concept:flow", "concept", map[string]any{
			"evidence": []any{"the only reading now"},
		})},
	}, Options{}); err != nil {
		t.Fatal(err)
	}
	got := items(t, read(t, kgDir, "concept:flow"), "evidence")
	if !reflect.DeepEqual(got, []string{"source:b|the only reading now"}) {
		t.Errorf("evidence = %v, want only the new item", got)
	}
}

// A create under on-conflict=merge folds, matching what that policy already
// means across time.
func TestCreateFoldsUnderMergeAndSkip(t *testing.T) {
	for _, policy := range []ConflictPolicy{ConflictMerge, ConflictSkip} {
		t.Run(string(policy), func(t *testing.T) {
			kgDir := setupKG(t)
			writeSchema(t, kgDir, "concept", conceptSchema)
			res, err := Apply(kgDir, envFrom(t, "k", "source:a"), []Record{
				{Op: OpCreate, Entity: entity("concept:flow", "concept", map[string]any{"name": "Flow"})},
				{Op: OpCreate, Entity: entity("concept:flow", "concept", map[string]any{
					"evidence": []any{"a second section's reading"},
				})},
			}, Options{OnConflict: policy})
			if err != nil {
				t.Fatalf("create must fold under on-conflict=%s: %v", policy, err)
			}
			if res.TotalRecords != 1 {
				t.Fatalf("records = %d, want 1 after the fold", res.TotalRecords)
			}
			e := read(t, kgDir, "concept:flow")
			if e.Attributes["name"] != "Flow" {
				t.Errorf("name = %v, want the first record's", e.Attributes["name"])
			}
			if got := items(t, e, "evidence"); len(got) != 1 {
				t.Errorf("evidence = %v, want the second record's item", got)
			}
		})
	}
}

// The contributor is grammar: a malformed one is refused at the envelope,
// before any record is looked at.
func TestContributorMustBeAnEntityID(t *testing.T) {
	if _, err := NewEnvelopeFrom(Version, "k", "not-an-id"); err == nil {
		t.Error("a contributor that is not type:local must be refused")
	}
	e, err := NewEnvelopeFrom(Version, "k", "Source:MDA Paper")
	if err != nil {
		t.Fatalf("a well-formed contributor must be accepted: %v", err)
	}
	if e.Contributor() != "source:mda_paper" {
		t.Errorf("contributor = %q, want it normalized like every other id", e.Contributor())
	}
}
