package command

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/merge"
	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// MergeResult describes the outcome of a merge operation.
type MergeResult struct {
	SourceID          string          `json:"source_id"`
	TargetID          string          `json:"target_id"`
	AttrsUpdated      int             `json:"attrs_updated"`
	LinksAdded        int             `json:"links_added"`
	IncomingRewritten int             `json:"incoming_rewritten"`
	Warnings          []merge.Warning `json:"warnings,omitempty"`
}

// MergeOptions configures merge behavior.
type MergeOptions struct {
	// Prefer names which side of a *scalar* conflict wins: "" or "source" for
	// the incoming value, "target" for the value already there.
	//
	// The default flipped with ADR-0030.  It used to be target-wins, which
	// made `kg merge` and `upsert` give opposite answers to the same
	// question; kg now holds one merge model and this flag narrows to the one
	// field kind where a preference was ever meaningful.  A multi-valued
	// attribute keeps both accounts and a relation unions, so neither has a
	// side to prefer.
	Prefer    string
	DryRun    bool
	CrossType bool

	// Kinds is an optional per-graph schema cache; nil reads each schema as
	// it is needed.
	Kinds *schema.Kinds
	// Graph is an optional pre-loaded graph. A merge has to rewrite every
	// inbound link to the source, and finding those means reading every
	// entity; a caller merging in a loop passes the graph it already has
	// rather than making each merge walk the corpus again.
	Graph *store.Graph
}

// Merge combines source into target, rewriting all incoming links, then
// deletes the source entity. When DryRun is true, no changes are written.
func Merge(kgDir, sourceID, targetID string, opts MergeOptions) (*MergeResult, error) {
	source, err := store.ReadEntity(kgDir, sourceID)
	if err != nil {
		return nil, fmt.Errorf("reading source %q: %w", sourceID, err)
	}
	target, err := store.ReadEntity(kgDir, targetID)
	if err != nil {
		return nil, fmt.Errorf("reading target %q: %w", targetID, err)
	}

	if source.Type != target.Type && !opts.CrossType {
		return nil, fmt.Errorf("cannot merge %s (%s) into %s (%s): different types (use --cross-type)",
			sourceID, source.Type, targetID, target.Type)
	}

	result := &MergeResult{
		SourceID: sourceID,
		TargetID: targetID,
	}

	// Merge attributes by the one rule (ADR-0030).  A merge is a fold of two
	// claims neither of which is the graph's prior state, so multi-valued
	// attributes concatenate rather than replace: the two entities genuinely
	// were written by different sources and both accounts are true.  Only the
	// scalar rule has a side to prefer, which is why --prefer narrows to it.
	mergedAttrs, warns := merge.Attributes(target.Attributes, source.Attributes,
		kindsFor(kgDir, opts.Kinds, target.Type), merge.Options{
			Mode:           merge.Append,
			PreferExisting: opts.Prefer == "target",
		})
	result.Warnings = warns
	for k, v := range mergedAttrs {
		if old, had := target.Attributes[k]; !had || store.AttrText(old) != store.AttrText(v) {
			result.AttrsUpdated++
		}
	}
	target.Attributes = mergedAttrs

	// Union outgoing links from source into target, skipping the
	// self-reference the merge would otherwise create.
	srcRels := make(map[string][]string, len(source.Relations))
	for rel, targets := range source.Relations {
		for _, t := range targets {
			if t != targetID {
				srcRels[rel] = append(srcRels[rel], t)
			}
		}
	}
	mergedRels, added := merge.Relations(target.Relations, srcRels)
	target.Relations = mergedRels
	result.LinksAdded = added

	// Find all entities that link to source and rewrite those links.
	//
	// Only the referrers are re-read from disk: the caller's graph may be a
	// snapshot from before earlier merges in the same batch, and the file is
	// the authority.
	var candidates []string
	if opts.Graph != nil {
		candidates = opts.Graph.Referrers(sourceID)
	} else {
		g, err := store.LoadGraph(kgDir)
		if err != nil {
			return nil, fmt.Errorf("loading graph for incoming link scan: %w", err)
		}
		candidates = g.Referrers(sourceID)
	}

	var toUpdate []*store.Entity
	for _, refID := range candidates {
		if refID == sourceID || refID == targetID {
			continue
		}
		e, err := store.ReadEntity(kgDir, refID)
		if err != nil {
			continue // entity may have been deleted earlier in the batch
		}
		changed := false
		for relName, targets := range e.Relations {
			relChanged := false
			for i, t := range targets {
				if t == sourceID {
					targets[i] = targetID
					relChanged = true
					result.IncomingRewritten++
				}
			}
			if relChanged {
				e.Relations[relName] = dedupStrings(targets)
				changed = true
			}
		}
		if changed {
			toUpdate = append(toUpdate, e)
		}
	}

	if opts.DryRun {
		return result, nil
	}

	// Write all updated referrers.
	for _, e := range toUpdate {
		if err := store.WriteEntity(kgDir, e); err != nil {
			return nil, fmt.Errorf("updating referrer %s: %w", e.ID, err)
		}
	}

	// Write merged target.
	if err := store.WriteEntity(kgDir, target); err != nil {
		return nil, fmt.Errorf("writing merged target: %w", err)
	}

	// Delete source entity file.
	if err := store.DeleteEntity(kgDir, sourceID); err != nil {
		return nil, fmt.Errorf("deleting source %s: %w", sourceID, err)
	}

	return result, nil
}

func dedupStrings(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}
