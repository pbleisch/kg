package batch

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/command"
	"github.com/pbleisch/kg/pkg/store"
	"github.com/pbleisch/kg/pkg/vector"
)

// A Dedup records one entity folded into an existing one.
type Dedup struct {
	From string `json:"from"`
	To   string `json:"to"`
}

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
	Deduped        int            `json:"deduped"`
	Skipped        int            `json:"skipped"`
	Dedups         []Dedup        `json:"dedups,omitempty"`
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

	// Dedup needs a vector for each incoming entity. Compute them all in one
	// call to the embedding command, and keep them here rather than on the
	// records: a vector is derived data and must not reach an entity file (P2).
	var queryVecs map[int]map[string][]float64
	if opts.OnConflict == ConflictDedup {
		queryVecs = preEmbed(kgDir, recs, opts.DedupFields)
	}

	// Whatever vectors the records carried have been harvested. They must not
	// reach a file: an entity holds what someone authored, and 384 floats are
	// not that (P2). This is the boundary that was letting them through.
	if n := stripDerivedAttrs(recs); n > 0 {
		res.Warnings = append(res.Warnings, Warning{
			Code:    "derived_attribute",
			Message: fmt.Sprintf("%d embedding attribute(s) were used for dedup and not written: vectors live in .kg/.cache/, not in entity files", n),
		})
	}

	// The dedup candidate set is the vector sidecar as it stands when the
	// batch starts. It does not see entities this batch creates — the index it
	// replaced did not either, because sync was suppressed for the batch — so
	// two near-identical new entities in one batch both land, and the next
	// `kg dedup` adjudicates them.
	var sidecar *vector.Store
	if opts.OnConflict == ConflictDedup {
		if s, err := vector.Open(store.CacheDir(kgDir), ""); err == nil {
			sidecar = s
			defer sidecar.Close()
		}
	}

	// A merge has to find every entity linking to its source. Load the graph
	// once for the batch rather than once per merge.
	var graph *store.Graph
	if opts.OnConflict == ConflictDedup || hasMerge(recs) {
		graph, _ = store.LoadGraph(kgDir)
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

	remap := make(map[string]string)
	for i, r := range recs {
		applyRemap(&r, remap)

		var err error
		switch r.Op {
		case OpCreate, OpUpsert:
			var into string
			var skipped bool
			into, skipped, err = applyEntity(kgDir, r, opts, queryVecs[i], sidecar, graph)
			switch {
			case err != nil, skipped:
			case into != "":
				remap[r.Entity.ID] = into
				res.Deduped++
				res.Dedups = append(res.Dedups, Dedup{From: r.Entity.ID, To: into})
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

	sort.SliceStable(res.Dedups, func(a, b int) bool { return res.Dedups[a].From < res.Dedups[b].From })
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

// applyRemap rewrites a record's references through the dedup remap table.
func applyRemap(r *Record, remap map[string]string) {
	if len(remap) == 0 {
		return
	}
	for _, p := range []*string{&r.From, &r.To, &r.Target} {
		if mapped, ok := remap[*p]; ok {
			*p = mapped
		}
	}
	if r.Entity != nil {
		for rel, targets := range r.Entity.Relations {
			for i, t := range targets {
				if mapped, ok := remap[t]; ok {
					targets[i] = mapped
				}
			}
			r.Entity.Relations[rel] = targets
		}
	}
}

// applyEntity writes a create or upsert.  `into` names the entity this one was
// folded into, when dedup found a match; `skipped` says the conflict policy
// left an existing entity alone.
func applyEntity(kgDir string, r Record, opts Options, queryVecs map[string][]float64, sidecar *vector.Store, graph *store.Graph) (into string, skipped bool, err error) {
	if opts.OnConflict == ConflictDedup {
		into, err = applyDedup(kgDir, r, opts, queryVecs, sidecar, graph)
		return into, false, err
	}
	if r.Op == OpUpsert {
		if _, err := command.UpsertEntity(kgDir, r.Entity); err != nil {
			return "", false, fmt.Errorf("upserting %q: %w", r.Entity.ID, err)
		}
		return "", false, nil
	}
	skipped, err = applyCreate(kgDir, r, opts.OnConflict)
	return "", skipped, err
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

// preEmbed computes every dedup query vector the batch needs in one call to
// the embedding command rather than one subprocess per entity.
//
// The vectors are returned, keyed by record index and field. They used to be
// written onto the record as a "<field>_embedding" attribute, which meant a
// dedup batch wrote 384 floats into every entity file it created — the exact
// thing P2 forbids, arriving through the one boundary that was supposed to
// stop it.
func preEmbed(kgDir string, recs []Record, fields []string) map[int]map[string][]float64 {
	out := make(map[int]map[string][]float64)

	// A missing embedder is not a failure: records that carry their own
	// vectors still dedup, and records that do not simply land. It is a
	// degraded dedup, not a refused batch.
	emb, embErr := store.ResolveEmbedder(kgDir)

	type target struct {
		rec   int
		field string
	}
	var inputs []store.EmbedInput
	var targets []target
	for i, r := range recs {
		if r.Op != OpCreate && r.Op != OpUpsert {
			continue
		}
		if r.Entity == nil {
			continue
		}
		for _, field := range fields {
			// A record that already carries a vector supplies its own.
			if val, ok := r.Entity.Attributes[field+"_embedding"]; ok {
				if vec, ok := store.ExtractVector(val); ok && len(vec) > 0 {
					if out[i] == nil {
						out[i] = make(map[string][]float64)
					}
					out[i][field] = vec
					continue
				}
			}
			text, ok := r.Entity.Attributes[field]
			if !ok {
				continue
			}
			textStr := fmt.Sprintf("%v", text)
			if textStr == "" {
				continue
			}
			inputs = append(inputs, store.EmbedInput{
				Text: textStr, Field: field, EntityID: r.Entity.ID, Type: r.Entity.Type,
			})
			targets = append(targets, target{rec: i, field: field})
		}
	}
	if len(inputs) == 0 || embErr != nil {
		return out
	}
	vectors, err := store.RunEmbedBatch(emb.Command, inputs)
	if err != nil || len(vectors) != len(inputs) {
		return out
	}
	for i, t := range targets {
		if out[t.rec] == nil {
			out[t.rec] = make(map[string][]float64)
		}
		out[t.rec][t.field] = vectors[i]
	}
	return out
}

// applyDedup writes a create or upsert, folding it into a similar existing
// entity when one is close enough.
func applyDedup(kgDir string, r Record, opts Options, queryVecs map[string][]float64, sidecar *vector.Store, graph *store.Graph) (string, error) {
	// queryVecs came from preEmbed, which harvested both what the record
	// carried and what the embedder produced for the rest.
	if len(queryVecs) == 0 {
		return "", applyCreateOrUpsert(kgDir, r)
	}

	bestScore, bestID := 0.0, ""
	if sidecar != nil {
		for _, field := range opts.DedupFields {
			qv, ok := queryVecs[field]
			if !ok {
				continue
			}
			matches, err := sidecar.Search(vector.Query{
				Vector: qv, Type: r.Entity.Type, Fields: []string{field}, TopN: 1,
			})
			if err != nil || len(matches) == 0 {
				continue
			}
			if matches[0].Score > bestScore {
				bestScore, bestID = matches[0].Score, matches[0].ID
			}
		}
	} else {
		// No sidecar: fall back to whatever vectors the entity files still
		// carry. Corpora written before vectors moved out keep working.
		existing, err := store.ListEntitiesByType(kgDir, r.Entity.Type)
		if err != nil {
			return "", applyCreateOrUpsert(kgDir, r)
		}
		for _, e := range existing {
			if e.ID == r.Entity.ID {
				continue
			}
			for _, field := range opts.DedupFields {
				qv, ok := queryVecs[field]
				if !ok {
					continue
				}
				val, ok := e.Attributes[field+"_embedding"]
				if !ok {
					continue
				}
				vec, ok := store.ExtractVector(val)
				if !ok || len(vec) == 0 {
					continue
				}
				score, err := store.CosineSimilarity(qv, vec)
				if err == nil && score > bestScore {
					bestScore, bestID = score, e.ID
				}
			}
		}
	}

	if bestScore >= opts.DedupThreshold && bestID != "" && bestID != r.Entity.ID {
		if err := store.WriteEntity(kgDir, r.Entity); err != nil {
			return "", fmt.Errorf("writing entity before merge: %w", err)
		}
		if _, err := command.Merge(kgDir, r.Entity.ID, bestID, command.MergeOptions{
			CrossType: true, Graph: graph,
		}); err != nil {
			return "", fmt.Errorf("dedup merge %s → %s: %w", r.Entity.ID, bestID, err)
		}
		return bestID, nil
	}
	return "", applyCreateOrUpsert(kgDir, r)
}

func applyCreateOrUpsert(kgDir string, r Record) error {
	if r.Op == OpUpsert {
		if _, err := command.UpsertEntity(kgDir, r.Entity); err != nil {
			return fmt.Errorf("upserting %q: %w", r.Entity.ID, err)
		}
		return nil
	}
	if err := store.WriteEntity(kgDir, r.Entity); err != nil {
		return fmt.Errorf("creating entity %q: %w", r.Entity.ID, err)
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
// It runs after preEmbed has taken what dedup needs and before the first
// write, so a producer may still supply a vector — it is used, and then it is
// not persisted.
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
