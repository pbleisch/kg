package command

import (
	"fmt"
	"strings"

	"github.com/pbleisch/kg/pkg/merge"
	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// UpsertOptions configure one upsert.
type UpsertOptions struct {
	// Contributor is the entity id of the source this write speaks for.  It
	// is the unit of reset: a multi-valued attribute drops the items this
	// contributor wrote before and keeps everyone else's.  Empty means the
	// write speaks for nobody, and its items are never replaced.
	Contributor string

	// Kinds is an optional per-graph schema cache.  A caller upserting in a
	// loop passes one so each type's schema is read once; nil reads it per
	// call.
	Kinds *schema.Kinds
}

// UpsertEntity creates a new entity or merges into an existing one.
//
// The merge rule is not stated here.  It is [merge.Into] — scalars take the
// incoming value and report an overwrite, multi-valued attributes replace this
// contributor's own items and keep every other one's, relations union — and
// `kg merge` calls the same function, so the two verbs cannot drift apart
// again (ADR-0030).
func UpsertEntity(kgDir string, e *store.Entity, opts UpsertOptions) (*store.Entity, []merge.Warning, error) {
	// Normalize IDs and type before persisting.
	e.ID = store.NormalizeEntityID(e.ID)
	e.Type = store.ToSnakeCase(e.Type)
	for rel, targets := range e.Relations {
		normRel := store.NormalizeRelation(rel)
		for i, t := range targets {
			targets[i] = store.NormalizeEntityID(t)
		}
		if normRel != rel {
			delete(e.Relations, rel)
			e.Relations[normRel] = targets
		}
	}

	kinds := kindsFor(kgDir, opts.Kinds, e.Type)
	mopts := merge.Options{Contributor: opts.Contributor, Mode: merge.Replace}

	existing, readErr := store.ReadEntity(kgDir, e.ID)
	if readErr != nil {
		// Nothing to merge into, but the incoming items still have to be
		// stamped with the contributor and ordered the way a merge would
		// leave them, or the first write and the second differ for no reason.
		warns := stampNew(e, kinds, mopts)
		if err := store.WriteEntity(kgDir, e); err != nil {
			return nil, warns, fmt.Errorf("writing entity: %w", err)
		}
		return e, warns, nil
	}

	warns := merge.Into(existing, e, kinds, mopts)
	if err := store.WriteEntity(kgDir, existing); err != nil {
		return nil, warns, fmt.Errorf("writing entity: %w", err)
	}
	return existing, warns, nil
}

// stampNew applies the multi-valued rule to an entity with nothing to merge
// into, so that `upsert` twice and `upsert` once leave the same bytes.
func stampNew(e *store.Entity, kinds schema.FieldKinds, opts merge.Options) []merge.Warning {
	if len(e.Attributes) == 0 || !kinds.Any() {
		return nil
	}
	attrs, warns := merge.Attributes(nil, e.Attributes, kinds, opts)
	e.Attributes = attrs
	return warns
}

// kindsFor answers the schema's one question, building a cache when the caller
// did not pass one.  A caller in a loop passes its own; a caller with one
// entity should not have to.
func kindsFor(kgDir string, cache *schema.Kinds, typeName string) schema.FieldKinds {
	if cache == nil {
		cache = schema.NewKinds(kgDir)
	}
	return cache.For(typeName)
}

// ParseAttrs converts ["key=value", ...] into a map. Each argument is split
// on the first '=' sign.
func ParseAttrs(args []string) (map[string]string, error) {
	m := make(map[string]string, len(args))
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid attribute %q: expected key=value", a)
		}
		m[k] = v
	}
	return m, nil
}
