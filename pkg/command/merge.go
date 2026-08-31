package command

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// MergeResult describes the outcome of a merge operation.
type MergeResult struct {
	SourceID          string `json:"source_id"`
	TargetID          string `json:"target_id"`
	AttrsUpdated      int    `json:"attrs_updated"`
	LinksAdded        int    `json:"links_added"`
	IncomingRewritten int    `json:"incoming_rewritten"`
}

// MergeOptions configures merge behavior.
type MergeOptions struct {
	PreferSource bool
	DryRun       bool
	CrossType    bool
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

	// Merge attributes.
	if target.Attributes == nil {
		target.Attributes = make(map[string]any)
	}
	for k, v := range source.Attributes {
		if _, exists := target.Attributes[k]; exists && !opts.PreferSource {
			continue // target wins on conflicts
		}
		target.Attributes[k] = v
		result.AttrsUpdated++
	}

	// Union outgoing links from source into target.
	if target.Relations == nil {
		target.Relations = make(map[string][]string)
	}
	for rel, srcTargets := range source.Relations {
		for _, t := range srcTargets {
			if t == targetID {
				continue // skip would-be self-reference
			}
			if !containsString(target.Relations[rel], t) {
				target.Relations[rel] = append(target.Relations[rel], t)
				result.LinksAdded++
			}
		}
	}

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
