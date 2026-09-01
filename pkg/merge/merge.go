// Package merge holds kg's one answer to "two claims on one identity have to
// become one entity".
//
// The rule is stated here and nowhere else.  Both verbs that combine two
// claims call it: `upsert` — through [command.UpsertEntity] and the write
// boundary's fold — and `kg merge`, through [command.Merge].  They used to
// disagree, in opposite directions: an upsert took the incoming value and a
// merge kept the target's, so the same two values survived differently
// depending on which verb a producer happened to use.  ADR-0030 settles that
// on upsert's answer, and this package is where "settles" is kept honest.
//
// The rule, by field kind:
//
//   - a **scalar attribute** takes the incoming value, and overwriting a
//     *different* value is reported.  One slot holds one value; last-wins is at
//     least defined, and the warning turns a silent loss into a visible one,
//     which is what P3 says a content-class problem gets.
//
//   - a **multi-valued attribute** — one the type's schema declares
//     `type: list` — is replaced *by contributor*: the items this batch's
//     contributor wrote before are dropped, this batch's items are appended,
//     and every other contributor's items stand.  Items carrying no source at
//     all are never dropped by anyone; only `unset` removes them.
//
//   - a **relation** unions, unchanged — and unchanged for a reason rather
//     than by omission.  A relation item's identity *is* its target, so
//     re-submitting one is a no-op by construction.  Attribute values carry no
//     identity, which is the whole reason they need a contributor before a
//     union can be idempotent.
//
// Replace-by-contributor is the only rule that converges under a re-ingest
// without a ledger, and the ledger does not cover a re-ingest: its key is a
// hash of the manifest bytes, so a re-extraction of one document is a new key
// and applies again.  P5's own check — apply the same batch twice and the
// graph is unchanged the second time — is therefore this package's check too.
package merge

import (
	"fmt"
	"sort"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// A Warning is advisory: reported, never blocking.  Content is advised (P3).
// The write boundary translates these into its own Warning type rather than
// this package importing it, which would be a cycle.
type Warning struct {
	Code    string
	Field   string
	Message string
}

// A Mode says how the items of a multi-valued attribute combine.
type Mode int

const (
	// Replace is the write boundary's rule: the incoming contributor replaces
	// its own previous items and leaves every other contributor's standing.
	// This is the mode that makes a re-ingest converge.
	Replace Mode = iota

	// Append concatenates instead.  It is the mode for two claims that are
	// both *incoming* — two records of one batch folded together, or two
	// entities being merged by `kg merge` — because neither is the graph's
	// prior state and both accounts are equally true.
	Append
)

// Options configure one merge.
type Options struct {
	// Contributor is the entity id of the source this write speaks for, or ""
	// when it speaks for nobody.  A write with no contributor writes items
	// carrying no source: permanent, never replaced, removable only by
	// `unset`.  That is what makes a hand-authored edit survive a re-ingest.
	Contributor string

	// Mode says how multi-valued attributes combine.
	Mode Mode

	// PreferExisting flips the scalar rule so the value already there wins.
	// It is the only preference that was ever meaningful, and it is
	// meaningful only here: a list keeps both accounts, and a relation unions.
	PreferExisting bool
}

// Scalar is the one rule for a single-slot attribute.
//
// The incoming value wins.  Overwriting a *different* value is reported — the
// loss is real, 1,610 entities in the one real corpus carry two or more
// sources and exactly one description, and nothing recorded what the other
// documents had to say.
func Scalar(field string, existing, incoming any, opts Options) (any, *Warning) {
	if opts.PreferExisting && existing != nil {
		return existing, nil
	}
	if existing == nil || sameScalar(existing, incoming) {
		return incoming, nil
	}
	return incoming, &Warning{
		Code:  "attribute_overwritten",
		Field: "attributes." + field,
		Message: fmt.Sprintf("%s: %q replaced %q; a scalar holds one value and the incoming one wins",
			field, store.AttrText(incoming), store.AttrText(existing)),
	}
}

// Items is the one rule for a multi-valued attribute.
//
// In [Replace] mode the contributor's own previous items are dropped and this
// write's items take their place; every other contributor's items, and every
// item with no source at all, stand.  In [Append] mode nothing is dropped.
// Either way items are deduplicated on (source, text) and ordered by the same
// pair, so a re-ingest converges and two rebuilds of one corpus are diffable
// against each other.
func Items(existing, incoming []store.Item, opts Options) []store.Item {
	out := make([]store.Item, 0, len(existing)+len(incoming))
	for _, it := range existing {
		if opts.Mode == Replace && opts.Contributor != "" && it.Source == opts.Contributor {
			continue // this contributor is rewriting its own account
		}
		out = append(out, it)
	}
	for _, it := range incoming {
		if it.Source == "" {
			// An item that names no source of its own belongs to the write's
			// contributor.  With no contributor it stays unattributed, which
			// is what makes `kg set` by hand permanent.
			it.Source = opts.Contributor
		}
		out = append(out, it)
	}
	return dedupeItems(out)
}

// Attributes merges one incoming attribute map into an existing one and
// returns the result.  Neither input is modified.
func Attributes(existing, incoming map[string]any, kinds schema.FieldKinds, opts Options) (map[string]any, []Warning) {
	out := make(map[string]any, len(existing)+len(incoming))
	for k, v := range existing {
		out[k] = v
	}
	var warns []Warning

	// Deterministic order, so the warnings a batch reports do not depend on
	// map iteration (P6).
	for _, k := range sortedKeys(incoming) {
		v := incoming[k]
		if kinds.IsList(k) {
			incomingItems := store.DecodeItems(v)
			out[k] = store.EncodeItems(Items(store.DecodeItems(out[k]), incomingItems, opts))
			// The warning belongs to the write that lands the items, not to
			// an intermediate combination of two incoming claims, and it is
			// worth saying only when items really are landing unattributed.
			if opts.Mode == Replace && opts.Contributor == "" && anyUnattributed(incomingItems) {
				warns = append(warns, Warning{
					Code:  "unattributed_items",
					Field: "attributes." + k,
					Message: fmt.Sprintf(
						"%s is multi-valued and this write names no contributor: its items carry no source, so no later write replaces them and only `unset` removes them",
						k),
				})
			}
			continue
		}
		val, w := Scalar(k, out[k], v, opts)
		out[k] = val
		if w != nil {
			warns = append(warns, *w)
		}
	}
	return out, warns
}

// Relations unions incoming targets into existing ones and returns the result
// with the number of targets added.  Neither input is modified.
func Relations(existing, incoming map[string][]string) (map[string][]string, int) {
	out := make(map[string][]string, len(existing)+len(incoming))
	for rel, targets := range existing {
		out[rel] = append([]string(nil), targets...)
	}
	added := 0
	for _, rel := range sortedRelKeys(incoming) {
		for _, t := range incoming[rel] {
			if !contains(out[rel], t) {
				out[rel] = append(out[rel], t)
				added++
			}
		}
	}
	return out, added
}

// Into folds src into dst by field kind.  dst is modified in place and is the
// entity that gets written.
func Into(dst, src *store.Entity, kinds schema.FieldKinds, opts Options) []Warning {
	attrs, warns := Attributes(dst.Attributes, src.Attributes, kinds, opts)
	dst.Attributes = attrs
	rels, _ := Relations(dst.Relations, src.Relations)
	if len(rels) > 0 {
		dst.Relations = rels
	}
	return warns
}

// dedupeItems drops items equal on (source, text) and orders what is left by
// the same pair.
func dedupeItems(items []store.Item) []store.Item {
	seen := make(map[string]struct{}, len(items))
	out := make([]store.Item, 0, len(items))
	for _, it := range items {
		k := it.SortKey()
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, it)
	}
	store.SortItems(out)
	return out
}

// sameScalar compares two values by their text projection rather than with ==,
// which panics the moment either side is a slice or a map — which, now that a
// field can be multi-valued, either side can be.
func sameScalar(a, b any) bool {
	return store.AttrText(a) == store.AttrText(b)
}

// anyUnattributed reports whether any item names no source of its own.
func anyUnattributed(items []store.Item) bool {
	for _, it := range items {
		if it.Source == "" {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedRelKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
