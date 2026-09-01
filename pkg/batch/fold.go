package batch

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pbleisch/kg/pkg/merge"
	"github.com/pbleisch/kg/pkg/schema"
)

// fold combines the records of one batch that claim one identity into a single
// record, before anything is validated or written.
//
// This is what a document is.  A source is extracted in sections, its subject
// recurs across them, and the sections' records are concatenated — so two
// records of one submission naming one entity is the normal case, not an
// error.  Refusing it threw away the whole document: measured on the one real
// pipeline, three of three multi-batch documents lost 100% of their extraction
// after the model had been paid for, and 48% of the acquisition cache is
// multi-batch.
//
// The record-level `identity_collision` check that did the refusing was never
// the identity invariant ADR-0028 names.  That invariant is about two *files*,
// one of which becomes unreachable by every query kg has; a fold produces one
// file and nothing becomes unreachable.  It was a placeholder standing in for
// a merge rule the boundary did not have — and the proof is that the boundary
// already merged two claims on one identity every time an upsert landed on an
// existing entity, using the rule it declined to apply across records.
//
// What it does not fold: `create` under on-conflict=fail.  A create asserts
// novelty and that assertion is the producer's to keep; those records reach
// validate unchanged and are still refused.  Under merge or skip it folds,
// matching what those policies already mean across time.
//
// The folded record sits at the position of the *first* record claiming the
// identity, and keeps that record's op, so entities still precede the links
// that point at them.
func fold(kgDir string, recs []Record, contributor string, onConflict ConflictPolicy) ([]Record, []Warning) {
	first := make(map[string]int, len(recs))
	count := make(map[string]int, len(recs))
	for i, r := range recs {
		id := foldableID(r, onConflict)
		if id == "" {
			continue
		}
		if _, seen := first[id]; !seen {
			first[id] = i
		}
		count[id]++
	}

	var folded []string
	for id, n := range count {
		if n > 1 {
			folded = append(folded, id)
		}
	}
	if len(folded) == 0 {
		return recs, nil
	}
	sort.Strings(folded)

	kinds := schema.NewKinds(kgDir)
	// Two records of ONE batch are two halves of one contributor's account,
	// so their items concatenate.  Replace-by-contributor runs once, later,
	// when the folded record meets the file — which is exactly why the second
	// batch of a document no longer deletes the first batch's items.
	opts := merge.Options{Contributor: contributor, Mode: merge.Append}

	var warns []Warning
	out := make([]Record, 0, len(recs))
	for i, r := range recs {
		id := foldableID(r, onConflict)
		if id == "" || count[id] < 2 {
			out = append(out, r)
			continue
		}
		if first[id] != i {
			continue // already folded into the first claimant
		}
		for j := i + 1; j < len(recs); j++ {
			if foldableID(recs[j], onConflict) != id {
				continue
			}
			for _, w := range merge.Into(r.Entity, recs[j].Entity, kinds.For(r.Entity.Type), opts) {
				warns = append(warns, Warning{
					Record: r.label(), Index: i, Code: w.Code, Message: w.Message,
				})
			}
		}
		out = append(out, r)
	}

	// One line for the whole fold, not one per identity: a real document
	// folded 174 of them, and 174 lines of stderr is not a report.
	warns = append([]Warning{{
		Code:    "records_folded",
		Message: foldMessage(folded, len(recs), len(out)),
	}}, warns...)
	return out, warns
}

// foldableID reports the identity a record claims, if the record is one the
// fold may combine.  Everything else comes back empty and reaches validate
// unchanged.
func foldableID(r Record, onConflict ConflictPolicy) string {
	if r.Entity == nil || r.Entity.ID == "" {
		return ""
	}
	switch r.Op {
	case OpUpsert:
		return r.Entity.ID
	case OpCreate:
		if onConflict == ConflictFail {
			return ""
		}
		return r.Entity.ID
	}
	return ""
}

// foldMessage says what happened once, naming a few of the identities rather
// than emitting one line per fold: a real document folded 174 of them.
func foldMessage(ids []string, before, after int) string {
	const named = 5
	shown := ids
	if len(shown) > named {
		shown = shown[:named]
	}
	msg := fmt.Sprintf("%d identit%s claimed by more than one record and %d records were folded into %d (%s",
		len(ids), plural(len(ids)), before, after, strings.Join(shown, ", "))
	if len(ids) > named {
		msg += fmt.Sprintf(", and %d more", len(ids)-named)
	}
	return msg + "); scalars took the last claim, multi-valued attributes concatenated, relations unioned"
}

func plural(n int) string {
	if n == 1 {
		return "y was"
	}
	return "ies were"
}
