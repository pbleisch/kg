package batch

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pbleisch/kg/pkg/command"
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
		Warnings:       warns,
	}
	for _, r := range recs {
		res.Counts[string(r.Op)]++
		res.RecordIDs = append(res.RecordIDs, r.label())
	}

	if opts.DryRun {
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
			skipped, err = applyEntity(kgDir, r, opts)
			switch {
			case err != nil, skipped:
			default:
				res.Created++
			}
			if skipped {
				res.Skipped++
			}
		case OpSet:
			err = applySet(kgDir, r)
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
			_, err = command.Merge(kgDir, r.From, r.Target, command.MergeOptions{
				PreferSource: r.Prefer == "source",
				CrossType:    true,
				Graph:        graph,
			})
			if err == nil {
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
		case OpSet, OpDelete:
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
func applyEntity(kgDir string, r Record, opts Options) (skipped bool, err error) {
	if r.Op == OpUpsert {
		if _, err := command.UpsertEntity(kgDir, r.Entity); err != nil {
			return false, fmt.Errorf("upserting %q: %w", r.Entity.ID, err)
		}
		return false, nil
	}
	return applyCreate(kgDir, r, opts.OnConflict)
}

func applyCreate(kgDir string, r Record, onConflict ConflictPolicy) (skipped bool, err error) {
	existing, readErr := store.ReadEntity(kgDir, r.Entity.ID)
	if readErr == nil {
		switch onConflict {
		case ConflictFail:
			return false, fmt.Errorf("entity %q already exists", r.Entity.ID)
		case ConflictSkip:
			return true, nil
		case ConflictMerge:
			if existing.Attributes == nil {
				existing.Attributes = make(map[string]any)
			}
			for k, v := range r.Entity.Attributes {
				existing.Attributes[k] = v
			}
			for rel, targets := range r.Entity.Relations {
				if existing.Relations == nil {
					existing.Relations = make(map[string][]string)
				}
				for _, t := range targets {
					if !containsString(existing.Relations[rel], t) {
						existing.Relations[rel] = append(existing.Relations[rel], t)
					}
				}
			}
			if err := store.WriteEntity(kgDir, existing); err != nil {
				return false, fmt.Errorf("writing merged entity %q: %w", r.Entity.ID, err)
			}
			return false, nil
		}
	}
	if err := store.WriteEntity(kgDir, r.Entity); err != nil {
		return false, fmt.Errorf("creating entity %q: %w", r.Entity.ID, err)
	}
	return false, nil
}

func applySet(kgDir string, r Record) error {
	e, err := store.ReadEntity(kgDir, r.Target)
	if err != nil {
		return fmt.Errorf("reading target %q: %w", r.Target, err)
	}
	if e.Attributes == nil {
		e.Attributes = make(map[string]any, len(r.Attributes))
	}
	for k, v := range r.Attributes {
		e.Attributes[k] = v
	}
	if err := store.WriteEntity(kgDir, e); err != nil {
		return fmt.Errorf("writing target %q: %w", r.Target, err)
	}
	return nil
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
