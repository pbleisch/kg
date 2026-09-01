package batch

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/merge"
	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// A Result reports what a batch did.  It is the library's answer; the CLI adds
// only presentation (P6).
type Result struct {
	Version        int            `json:"version"`
	IdempotencyKey string         `json:"idempotency_key"`
	Replayed       bool           `json:"replayed"`
	DryRun         bool           `json:"dry_run"`
	TotalRecords   int            `json:"total_records"`
	Counts         map[string]int `json:"counts"`
	RecordIDs      []string       `json:"record_ids"`
	Created        int            `json:"created"`
	Updated        int            `json:"updated"`
	Linked         int            `json:"linked"`
	Deleted        int            `json:"deleted"`
	Skipped        int            `json:"skipped"`
	Errors         []RecordError  `json:"errors,omitempty"`
	Warnings       []Warning      `json:"warnings,omitempty"`
	// Merges details each merge record: which identity folded into which and
	// how much moved. A count of one says a merge happened; this says what it
	// did, which is what `kg merge` prints and what a manifest merge used to
	// report as nothing at all.
	Merges []MergeDetail `json:"merges,omitempty"`
}

// A MergeDetail is what one merge record moved.
type MergeDetail struct {
	SourceID          string `json:"source_id"`
	TargetID          string `json:"target_id"`
	AttrsUpdated      int    `json:"attrs_updated"`
	LinksAdded        int    `json:"links_added"`
	IncomingRewritten int    `json:"incoming_rewritten"`
}

// mergeOptions states, in one place, how a record becomes a merge. Both the
// dry-run preview and the real apply build their options here so the two
// cannot drift.
func mergeOptions(r Record, graph *store.Graph, kinds *schema.Kinds, dryRun bool) command.MergeOptions {
	return command.MergeOptions{
		Prefer:    r.Prefer,
		CrossType: !r.RequireSameType,
		Graph:     graph,
		Kinds:     kinds,
		DryRun:    dryRun,
	}
}

func detailOf(mr *command.MergeResult) MergeDetail {
	return MergeDetail{
		SourceID:          mr.SourceID,
		TargetID:          mr.TargetID,
		AttrsUpdated:      mr.AttrsUpdated,
		LinksAdded:        mr.LinksAdded,
		IncomingRewritten: mr.IncomingRewritten,
	}
}

// Apply is kg's write boundary.  Every write in this repository comes through
// it, carrying an envelope, an idempotency key and typed records.
//
// It refuses the whole batch, writing nothing, when a record breaks a
// structural rule — the returned error is a [*RefusedError] naming each field
// and record.  Content problems come back in Result.Warnings and do not block.
//
// Applying the same batch twice under the same key applies it once: the second
// call returns the first call's result with Replayed set.
func Apply(kgDir string, env Envelope, records []Record, opts Options) (*Result, error) {
	if err := env.check(); err != nil {
		return nil, err
	}
	if err := opts.normalize(); err != nil {
		return nil, err
	}
	if kgDir == "" {
		return nil, fmt.Errorf("batch: no graph directory")
	}

	recs := make([]Record, len(records))
	for i, r := range records {
		recs[i] = r.clone()
		recs[i].normalize()
	}

	// Records claiming one identity become one record before anything else
	// looks at them.  It is one rule rather than a special case for batches:
	// the same merge that runs when an upsert lands on an existing file runs
	// here, across the records of one submission (ADR-0030).
	recs, foldWarns := fold(kgDir, recs, env.Contributor(), opts.OnConflict)

	errs, warns := validate(kgDir, recs)
	if len(errs) > 0 {
		return nil, &RefusedError{Errors: errs}
	}

	res := &Result{
		Version:        env.Version(),
		IdempotencyKey: env.IdempotencyKey(),
		DryRun:         opts.DryRun,
		TotalRecords:   len(recs),
		Counts:         map[string]int{},
		RecordIDs:      make([]string, 0, len(recs)),
		Warnings:       append(foldWarns, warns...),
	}
	for _, r := range recs {
		res.Counts[string(r.Op)]++
		res.RecordIDs = append(res.RecordIDs, r.label())
	}

	if opts.DryRun {
		// A dry-run still says what each merge would move. It writes nothing
		// — command.Merge under DryRun returns before every write — but it
		// does read the graph, because "what would this rewrite" cannot be
		// answered without knowing who points at the source. Each preview is
		// measured against the corpus as it stands, so two merges in one
		// batch that feed each other are each reported against the state
		// before either ran.
		if hasMerge(recs) {
			graph, err := store.LoadGraph(kgDir)
			if err != nil {
				return nil, fmt.Errorf("a merge needs the whole graph: %w", err)
			}
			kinds := schema.NewKinds(kgDir)
			for _, r := range recs {
				if r.Op != OpMerge {
					continue
				}
				mr, err := command.Merge(kgDir, r.From, r.Target, mergeOptions(r, graph, kinds, true))
				if err != nil {
					return nil, fmt.Errorf("record %s: %w", r.label(), err)
				}
				res.Merges = append(res.Merges, detailOf(mr))
				res.Warnings = append(res.Warnings, translate(r, mr.Warnings)...)
			}
		}
		return res, nil
	}

	if prior := readLedger(kgDir, env.IdempotencyKey()); prior != nil {
		prior.Replayed = true
		return prior, nil
	}

	// A vector a record carried must not reach a file: an entity holds what
	// someone authored, and 384 floats are not that (P2). This is the boundary
	// that was letting them through.
	if n := stripDerivedAttrs(recs); n > 0 {
		res.Warnings = append(res.Warnings, Warning{
			Code:    "derived_attribute",
			Message: fmt.Sprintf("%d embedding attribute(s) were not written: vectors live in .kg/.cache/, not in entity files", n),
		})
	}

	// A merge has to find every entity linking to its source. Load the graph
	// once for the batch rather than once per merge.
	//
	// A corpus that does not load refuses the batch: a merge rewrites inbound
	// links, and doing that against a graph missing the 265 files it refused
	// would rewrite some of them and silently not others (P3, ADR-0028).
	var graph *store.Graph
	if hasMerge(recs) {
		var err error
		graph, err = store.LoadGraph(kgDir)
		if err != nil {
			return nil, fmt.Errorf("a merge needs the whole graph: %w", err)
		}
	}

	// One batch, one transaction: the journal remembers what every touched
	// file held so an abort can put the graph back.  on-error warn and skip
	// deliberately trade that atomicity for progress and do not roll back.
	journal := store.BeginJournal(kgDir)

	// One schema read per type for the whole batch.  A real document is 1,558
	// records over 1,298 identities; reading a schema per record would make
	// the merge rule's one question the most expensive thing in the write.
	kinds := schema.NewKinds(kgDir)

	abort := func(err error) (*Result, error) {
		rbErr := journal.Rollback()
		if rbErr != nil {
			return nil, fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return nil, err
	}

	for _, r := range recs {
		var err error
		switch r.Op {
		case OpCreate, OpUpsert:
			var skipped bool
			var mw []merge.Warning
			skipped, mw, err = applyEntity(kgDir, r, opts, command.UpsertOptions{
				Contributor: env.Contributor(), Kinds: kinds,
			})
			res.Warnings = append(res.Warnings, translate(r, mw)...)
			switch {
			case err != nil, skipped:
			default:
				res.Created++
			}
			if skipped {
				res.Skipped++
			}
		case OpSet:
			var mw []merge.Warning
			mw, err = applySet(kgDir, r, kinds, env.Contributor())
			res.Warnings = append(res.Warnings, translate(r, mw)...)
			if err == nil {
				res.Updated++
			}
		case OpUnset:
			err = applyUnset(kgDir, r)
			if err == nil {
				res.Updated++
			}
		case OpLink:
			err = command.Link(kgDir, r.From, r.Rel, r.To)
			if err == nil {
				res.Linked++
			}
		case OpUnlink:
			err = command.Unlink(kgDir, r.From, r.Rel, r.To)
			if err == nil {
				res.Linked++
			}
		case OpDelete:
			err = command.Delete(kgDir, r.Target)
			if err == nil {
				res.Deleted++
			}
		case OpMerge:
			var mr *command.MergeResult
			mr, err = command.Merge(kgDir, r.From, r.Target, mergeOptions(r, graph, kinds, false))
			if err == nil {
				res.Warnings = append(res.Warnings, translate(r, mr.Warnings)...)
				res.Merges = append(res.Merges, detailOf(mr))
				res.Updated++
			}
		}

		if err != nil {
			re := RecordError{Record: r.label(), Code: "apply_failed", Message: err.Error()}
			if opts.OnError == ErrorFail {
				return abort(fmt.Errorf("record %s: %w", r.label(), err))
			}
			res.Errors = append(res.Errors, re)
		}
	}

	journal.Close()

	if err := writeLedger(kgDir, env.IdempotencyKey(), res); err != nil {
		res.Warnings = append(res.Warnings, Warning{
			Code:    "ledger",
			Message: fmt.Sprintf("batch applied but its idempotency key was not recorded: %v", err),
		})
	}
	// Bring the vector sidecar level with what the batch wrote. Only the
	// touched entities are re-embedded; the rest are carried across. A graph
	// with no embedder configured has no sidecar to maintain, which is a
	// state, not a failure.
	if err := refreshVectors(kgDir, recs); err != nil {
		res.Warnings = append(res.Warnings, Warning{
			Code:    "vectors",
			Message: fmt.Sprintf("entities written but the vector sidecar was not updated (run 'kg sync'): %v", err),
		})
	}
	return res, nil
}

// hasMerge reports whether a batch contains an operation that rewrites
// inbound links, and therefore needs the whole graph.
func hasMerge(recs []Record) bool {
	for _, r := range recs {
		if r.Op == OpMerge {
			return true
		}
	}
	return false
}

// refreshVectors re-embeds the entities a batch touched. A graph with no
// embedder is skipped silently: there is nothing derived to bring level.
func refreshVectors(kgDir string, recs []Record) error {
	emb, err := store.ResolveEmbedder(kgDir)
	if err != nil {
		var missing *store.ErrNoEmbedder
		if errors.As(err, &missing) {
			return nil
		}
		return err
	}
	// Deletes and merges are named too: an entity that is gone must lose its
	// rows, or `kg similar` keeps returning it.
	var ids []string
	for _, r := range recs {
		switch r.Op {
		case OpCreate, OpUpsert:
			if r.Entity != nil {
				ids = append(ids, r.Entity.ID)
			}
		case OpSet, OpUnset, OpDelete:
			ids = append(ids, r.Target)
		case OpMerge:
			ids = append(ids, r.From, r.Target)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	_, err = store.EmbedWith(kgDir, emb, false, false, ids...)
	return err
}

// applyEntity writes a create or upsert.  `skipped` says the conflict policy
// left an existing entity alone.
func applyEntity(kgDir string, r Record, opts Options, uo command.UpsertOptions) (skipped bool, warns []merge.Warning, err error) {
	if r.Op == OpUpsert {
		_, warns, err := command.UpsertEntity(kgDir, r.Entity, uo)
		if err != nil {
			return false, warns, fmt.Errorf("upserting %q: %w", r.Entity.ID, err)
		}
		return false, warns, nil
	}
	return applyCreate(kgDir, r, opts.OnConflict, uo)
}

// applyCreate writes a create under its conflict policy.
//
// The merge branch used to spell out its own attribute and relation rules,
// which were upsert's rules written a second time.  It calls the one rule now
// (ADR-0030): two copies of a merge rule are two merge rules the moment either
// is edited.
func applyCreate(kgDir string, r Record, onConflict ConflictPolicy, uo command.UpsertOptions) (skipped bool, warns []merge.Warning, err error) {
	if _, readErr := store.ReadEntity(kgDir, r.Entity.ID); readErr == nil {
		switch onConflict {
		case ConflictFail:
			return false, nil, fmt.Errorf("entity %q already exists", r.Entity.ID)
		case ConflictSkip:
			return true, nil, nil
		case ConflictMerge:
			_, warns, err := command.UpsertEntity(kgDir, r.Entity, uo)
			if err != nil {
				return false, warns, fmt.Errorf("writing merged entity %q: %w", r.Entity.ID, err)
			}
			return false, warns, nil
		}
	}
	_, warns, err = command.UpsertEntity(kgDir, r.Entity, uo)
	if err != nil {
		return false, warns, fmt.Errorf("creating entity %q: %w", r.Entity.ID, err)
	}
	return false, warns, nil
}

// applySet assigns attributes.  `set` is an assignment, not a merge: it is the
// verb that says "this field now holds this", and `unset` is its inverse.
// What it does report is the one loss that is not obvious from the verb —
// replacing a multi-valued attribute that held other contributors' items.
func applySet(kgDir string, r Record, kinds *schema.Kinds, contributor string) ([]merge.Warning, error) {
	e, err := store.ReadEntity(kgDir, r.Target)
	if err != nil {
		return nil, fmt.Errorf("reading target %q: %w", r.Target, err)
	}
	if e.Attributes == nil {
		e.Attributes = make(map[string]any, len(r.Attributes))
	}
	fk := kinds.For(e.Type)
	var warns []merge.Warning
	for _, k := range sortedAttrKeys(r.Attributes) {
		if fk.IsList(k) {
			if n := attributedItems(e.Attributes[k]); n > 0 {
				warns = append(warns, merge.Warning{
					Code:  "attribution_discarded",
					Field: "attributes." + k,
					Message: fmt.Sprintf(
						"set replaced %s, discarding %d attributed item(s); `upsert` keeps other contributors' items, `set` does not",
						k, n),
				})
			}
			// The items an assignment writes still carry the batch's
			// contributor: whose account this is belongs to the batch, not to
			// the verb.  They are stamped, deduplicated and ordered exactly as
			// an upsert would leave them, so `set` and `upsert` cannot produce
			// two shapes of the same field.
			e.Attributes[k] = store.EncodeItems(merge.Items(nil, store.DecodeItems(r.Attributes[k]),
				merge.Options{Contributor: contributor, Mode: merge.Replace}))
			continue
		}
		e.Attributes[k] = r.Attributes[k]
	}
	if err := store.WriteEntity(kgDir, e); err != nil {
		return warns, fmt.Errorf("writing target %q: %w", r.Target, err)
	}
	return warns, nil
}

// applyUnset removes named attributes.  Removing one that is not there is not
// an error: unset says what the entity must not carry, and applying it twice
// says it twice.
func applyUnset(kgDir string, r Record) error {
	e, err := store.ReadEntity(kgDir, r.Target)
	if err != nil {
		return fmt.Errorf("reading target %q: %w", r.Target, err)
	}
	for _, name := range r.Unset {
		delete(e.Attributes, name)
	}
	if err := store.WriteEntity(kgDir, e); err != nil {
		return fmt.Errorf("writing target %q: %w", r.Target, err)
	}
	return nil
}

// attributedItems counts the items of a stored value that name a source.
func attributedItems(v any) int {
	n := 0
	for _, it := range store.DecodeItems(v) {
		if it.Source != "" {
			n++
		}
	}
	return n
}

func sortedAttrKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// translate carries a merge warning across the package boundary.  pkg/merge
// cannot import this package — that is the cycle — so the boundary restates
// the warning in its own type rather than the rule in its own code.
func translate(r Record, ws []merge.Warning) []Warning {
	out := make([]Warning, 0, len(ws))
	for _, w := range ws {
		out = append(out, Warning{Record: r.label(), Code: w.Code, Message: w.Message})
	}
	return out
}

// KeyFor derives a stable idempotency key from a prefix and a content hash.
// Producers that have no natural key can use it; the boundary does not invent
// one for them.
func KeyFor(prefix, contentHash string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
				return r
			case r == '-', r == '_', r == '.':
				return r
			}
			return '_'
		}, s)
	}
	key := clean(prefix) + ":" + clean(contentHash)
	if len(key) > maxKeyLen {
		key = key[:maxKeyLen]
	}
	return key
}

// stripDerivedAttrs removes embedding attributes from every record's entity
// and reports how many it removed.
//
// It runs before the first write, so a producer may still supply a vector and
// find it silently dropped rather than stored.
func stripDerivedAttrs(recs []Record) int {
	n := 0
	for _, r := range recs {
		if r.Entity == nil {
			continue
		}
		for k := range r.Entity.Attributes {
			if strings.HasSuffix(k, "_embedding") {
				delete(r.Entity.Attributes, k)
				n++
			}
		}
	}
	return n
}
