package batch

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/store"
)

// An Op names what a record does to the graph.
type Op string

const (
	OpCreate Op = "create"
	OpUpsert Op = "upsert"
	OpSet    Op = "set"
	OpLink   Op = "link"
	OpUnlink Op = "unlink"
	OpDelete Op = "delete"
	OpMerge  Op = "merge"
)

// A Record is one typed write.  Which fields matter depends on Op; [Apply]
// refuses a record whose fields do not match its Op rather than guessing.
type Record struct {
	// ID labels the record in results and errors.  Optional; when empty the
	// entity id, target or source is used instead.
	ID string

	Op Op

	Entity     *store.Entity  // create, upsert
	Target     string         // set, delete, merge (destination)
	From       string         // link, unlink, merge (source)
	Rel        string         // link, unlink
	To         string         // link, unlink
	Prefer     string         // merge: "source" keeps the source's values
	Attributes map[string]any // set
}

// label is what errors and results call this record.
func (r Record) label() string {
	switch {
	case r.ID != "":
		return r.ID
	case r.Entity != nil && r.Entity.ID != "":
		return string(r.Op) + " " + r.Entity.ID
	case r.Target != "":
		return string(r.Op) + " " + r.Target
	case r.From != "":
		return string(r.Op) + " " + r.From
	default:
		return string(r.Op)
	}
}

// entityID reports the identity a record declares, if it declares one.
func (r Record) entityID() string {
	if r.Entity != nil {
		return r.Entity.ID
	}
	return ""
}

// clone copies the record deeply enough that normalizing a batch does not edit
// the entity a caller still holds.
func (r Record) clone() Record {
	if r.Entity != nil {
		e := *r.Entity
		if r.Entity.Attributes != nil {
			e.Attributes = make(map[string]any, len(r.Entity.Attributes))
			for k, v := range r.Entity.Attributes {
				e.Attributes[k] = v
			}
		}
		if r.Entity.Relations != nil {
			e.Relations = make(map[string][]string, len(r.Entity.Relations))
			for rel, targets := range r.Entity.Relations {
				e.Relations[rel] = append([]string(nil), targets...)
			}
		}
		r.Entity = &e
	}
	if r.Attributes != nil {
		attrs := make(map[string]any, len(r.Attributes))
		for k, v := range r.Attributes {
			attrs[k] = v
		}
		r.Attributes = attrs
	}
	return r
}

// normalize applies the store's snake_case rules to every identifier in the
// record.  Normalization lives here rather than in a caller: P7 leaves the CLI
// none of its own, and a Go caller has no parser that would have done it.
func (r *Record) normalize() {
	r.Op = Op(strings.ToLower(strings.TrimSpace(string(r.Op))))
	if r.Entity != nil {
		r.Entity.ID = store.NormalizeEntityID(r.Entity.ID)
		r.Entity.Type = store.ToSnakeCase(r.Entity.Type)
		if r.Entity.Relations != nil {
			// Two raw relation names can normalize to one, so targets merge
			// rather than overwrite, and duplicates are dropped so the same
			// records always produce the same file.
			normed := make(map[string][]string, len(r.Entity.Relations))
			for rel, targets := range r.Entity.Relations {
				nRel := store.NormalizeRelation(rel)
				for _, t := range targets {
					nt := store.NormalizeEntityID(t)
					if !containsString(normed[nRel], nt) {
						normed[nRel] = append(normed[nRel], nt)
					}
				}
			}
			r.Entity.Relations = normed
		}
	}
	if r.Target != "" {
		r.Target = store.NormalizeEntityID(r.Target)
	}
	if r.From != "" {
		r.From = store.NormalizeEntityID(r.From)
	}
	if r.To != "" {
		r.To = store.NormalizeEntityID(r.To)
	}
	if r.Rel != "" {
		r.Rel = store.NormalizeRelation(r.Rel)
	}
}

// A ConflictPolicy says what to do when a create hits an existing identity.
type ConflictPolicy string

const (
	ConflictFail  ConflictPolicy = "fail"
	ConflictSkip  ConflictPolicy = "skip"
	ConflictMerge ConflictPolicy = "merge"
	ConflictDedup ConflictPolicy = "dedup"
)

// An ErrorPolicy says what to do when a record fails while being applied.
// Only [ErrorFail] keeps the batch atomic; the other two buy progress with it.
type ErrorPolicy string

const (
	ErrorFail ErrorPolicy = "fail"
	ErrorWarn ErrorPolicy = "warn"
	ErrorSkip ErrorPolicy = "skip"
)

// Options are the knobs a producer may turn.  The zero value is the safe one:
// conflicts fail, errors fail, nothing is written that was not asked for.
type Options struct {
	OnConflict     ConflictPolicy
	OnError        ErrorPolicy
	DedupFields    []string
	DedupThreshold float64
	DryRun         bool
}

func (o *Options) normalize() error {
	if o.OnConflict == "" {
		o.OnConflict = ConflictFail
	}
	if o.OnError == "" {
		o.OnError = ErrorFail
	}
	switch o.OnConflict {
	case ConflictFail, ConflictSkip, ConflictMerge, ConflictDedup:
	default:
		return fmt.Errorf("batch: invalid on-conflict %q (fail, skip, merge, dedup)", o.OnConflict)
	}
	switch o.OnError {
	case ErrorFail, ErrorWarn, ErrorSkip:
	default:
		return fmt.Errorf("batch: invalid on-error %q (fail, warn, skip)", o.OnError)
	}
	fields := o.DedupFields[:0:0]
	for _, f := range o.DedupFields {
		if f = strings.TrimSpace(f); f != "" {
			fields = append(fields, f)
		}
	}
	sort.Strings(fields)
	o.DedupFields = fields
	if o.OnConflict == ConflictDedup && len(o.DedupFields) == 0 {
		return fmt.Errorf("batch: on-conflict dedup needs at least one dedup field")
	}
	if o.DedupThreshold == 0 {
		o.DedupThreshold = 0.85
	}
	if o.DedupThreshold < 0 || o.DedupThreshold > 1 {
		return fmt.Errorf("batch: dedup threshold %v is outside 0.0–1.0", o.DedupThreshold)
	}
	return nil
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
