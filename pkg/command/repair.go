package command

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// RepairOptions selects which classes of structural damage to repair.
//
// They are separate settings because they cost different things, and P3
// forbids one setting covering rules of different severity. Identity and
// Attributes are on by default: neither changes what anyone asserted. Dangling
// deletes an assertion and is off unless asked for.
type RepairOptions struct {
	Identity   bool
	Attributes bool
	Dangling   bool
	DryRun     bool
}

// DefaultRepair is what `kg validate --fix` does: correct the record, change
// no knowledge.
func DefaultRepair() RepairOptions {
	return RepairOptions{Identity: true, Attributes: true}
}

// A RepairResult reports what a repair pass did, or would do.
type RepairResult struct {
	// Files is how many entity files were walked.
	Files int `json:"files"`
	// Changed is how many were rewritten (or would be, under DryRun).
	Changed int                      `json:"changed"`
	Results []*store.NormalizeResult `json:"results,omitempty"`
	// Unfixable names what the pass will not touch. Every entry needs a
	// decision about which of two files survives, and kg does not make it.
	Unfixable []store.Structural `json:"unfixable,omitempty"`
}

// Repair walks the corpus and rewrites each file in place.
//
// It walks with [store.ListEntityFiles] and writes back to the file it read,
// which is the only safe shape for a whole-corpus rewrite: writing to the path
// an entity's `id:` implies skips one file and overwrites another on exactly
// the corpus this exists to fix (ADR-0029).
func Repair(kgDir string, opts RepairOptions) (*RepairResult, error) {
	files, err := store.ListEntityFiles(kgDir)
	if err != nil {
		return nil, fmt.Errorf("walking entities: %w", err)
	}
	res := &RepairResult{Files: len(files)}

	nopts := store.NormalizeOptions{
		Identity:   opts.Identity,
		Attributes: opts.Attributes,
		Dangling:   opts.Dangling,
		DryRun:     opts.DryRun,
	}
	byID := make(map[string][]string, len(files))
	if opts.Dangling {
		// What a target may resolve to is every identity the corpus holds,
		// derived from the paths — not from the `id:` lines, which are the
		// thing being repaired.
		nopts.Known = make(map[string]bool, len(files))
		for _, f := range files {
			if ident, err := store.IdentityAt(kgDir, f.Path); err == nil {
				nopts.Known[ident.ID] = true
			}
		}
	}

	for _, f := range files {
		if ident, err := store.IdentityAt(kgDir, f.Path); err == nil {
			byID[ident.ID] = append(byID[ident.ID], f.Path)
		}
		r, err := store.NormalizeEntityFile(kgDir, f.Path, nopts)
		if err != nil {
			return nil, err
		}
		if len(r.Unfixable) > 0 {
			res.Unfixable = append(res.Unfixable, r.Unfixable...)
		}
		if len(r.Fixes) == 0 {
			continue
		}
		res.Changed++
		res.Results = append(res.Results, r)
	}

	// An identity two files claim is unfixable by rule: repairing it means
	// deciding which file is the entity and which is a duplicate, and that is
	// adjudication, not normalization.
	res.Unfixable = append(res.Unfixable, store.Collisions(byID)...)
	store.SortStructural(res.Unfixable)
	return res, nil
}
