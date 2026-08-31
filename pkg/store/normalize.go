package store

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// Fix describes a single normalization applied to an entity file.
type Fix struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// NormalizeResult describes what was normalized for a single entity file.
type NormalizeResult struct {
	Path     string `json:"path"`
	EntityID string `json:"entity_id"`
	Fixes    []Fix  `json:"fixes,omitempty"`
	// Unfixable names what is wrong with the file that this pass will not
	// touch: an identity two files claim, or a filename kg would not write.
	// Both need a decision about which file survives, and that is not kg's.
	Unfixable []Structural `json:"unfixable,omitempty"`
}

// NormalizeOptions selects which classes of repair to apply.
//
// The classes are separate settings because they cost different things.
// Identity and Attributes rewrite a record *of* the entity and change no
// knowledge: under path-derived identity (ADR-0029) the file's identity is
// where it sits, so correcting a stale `id:` line moves nothing, and an
// attribute named `--dir` with the value `.kg` is a command-line flag that
// became knowledge. Dangling *deletes an assertion someone made*, which is why
// it is off unless asked for.
type NormalizeOptions struct {
	// Identity rewrites `id:` and `type:` to agree with the file's location.
	Identity bool
	// Attributes drops attributes whose names are not legal.
	Attributes bool
	// Dangling drops relation targets that resolve to no file. Resolve is the
	// caller's: it is given every identity the corpus holds.
	Dangling bool
	// Known is the set of identities the corpus holds, needed only when
	// Dangling is set.
	Known map[string]bool
	// DryRun reports what would change without writing.
	DryRun bool
}

// RepairAll is the default: the two classes that correct a record without
// changing what anyone asserted.
func RepairAll() NormalizeOptions {
	return NormalizeOptions{Identity: true, Attributes: true}
}

// NormalizeEntityFile reads one entity file, applies the selected repairs and
// writes it back to the file it came from.
//
// **The path is authoritative.** This used to treat `id:` as the truth and
// *move the file* to `EntityPath(id)` — which, on a corpus where 265 files
// declare an id that belongs to a different path, deletes one entity and
// overwrites another, 265 times. Nothing here moves a file. Where two files
// claim one identity, the repair reports and refuses to choose.
func NormalizeEntityFile(kgDir, filePath string, opts NormalizeOptions) (*NormalizeResult, error) {
	result := &NormalizeResult{Path: filePath}

	ident, err := IdentityAt(kgDir, filePath)
	if err != nil {
		if errs, refused := LoadRefused(err); refused {
			result.Unfixable = errs
			return result, nil
		}
		return nil, err
	}
	result.EntityID = ident.ID
	if bad := CheckPath(filePath, ident); bad != nil {
		result.Unfixable = append(result.Unfixable, *bad)
		return result, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filePath, err)
	}
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filePath, err)
	}
	if raw == nil {
		raw = map[string]interface{}{}
	}

	normalizeLinksKey(raw, result)
	if opts.Identity {
		adoptIdentity(raw, ident, result)
	}
	if opts.Attributes {
		dropIllegalAttrs(raw, result)
	}
	normalizeRelationKeys(raw, result)
	if opts.Dangling {
		dropDangling(raw, opts.Known, result)
	}

	if len(result.Fixes) == 0 || opts.DryRun {
		return result, nil
	}
	if err := RewriteEntityFile(filePath, entityFromRawMap(raw)); err != nil {
		return nil, fmt.Errorf("writing repaired entity: %w", err)
	}
	return result, nil
}

// normalizeLinksKey renames the "links" YAML key to "relations".
func normalizeLinksKey(raw map[string]interface{}, result *NormalizeResult) {
	links, hasLinks := raw["links"]
	if !hasLinks {
		return
	}

	if rels, hasRels := raw["relations"]; hasRels {
		// Both exist — merge links into relations.
		mergeRelMaps(asStringMapIface(rels), asStringMapIface(links))
	} else {
		raw["relations"] = links
	}
	delete(raw, "links")
	result.Fixes = append(result.Fixes, Fix{
		Field: "links", Message: "renamed 'links' to 'relations'",
	})
}

// adoptIdentity rewrites `id:` and `type:` to the identity the path gives.
func adoptIdentity(raw map[string]interface{}, ident Identity, result *NormalizeResult) {
	if got, _ := raw["id"].(string); got != ident.ID {
		raw["id"] = ident.ID
		if got == "" {
			result.Fixes = append(result.Fixes, Fix{
				Field: "id", Message: "set missing id to " + ident.ID,
			})
		} else {
			result.Fixes = append(result.Fixes, Fix{
				Field:   "id",
				Message: fmt.Sprintf("id %s → %s, to agree with the file's location", got, ident.ID),
			})
		}
	}
	if got, _ := raw["type"].(string); got != ident.Type {
		raw["type"] = ident.Type
		if got == "" {
			result.Fixes = append(result.Fixes, Fix{
				Field: "type", Message: "set missing type to " + ident.Type,
			})
		} else {
			result.Fixes = append(result.Fixes, Fix{
				Field:   "type",
				Message: fmt.Sprintf("type %s → %s, to agree with the directory", got, ident.Type),
			})
		}
	}
}

// dropIllegalAttrs removes attributes whose names are not legal names.
func dropIllegalAttrs(raw map[string]interface{}, result *NormalizeResult) {
	attrs, ok := raw["attributes"].(map[string]interface{})
	if !ok {
		return
	}
	for _, name := range IllegalAttributeNames(attrs) {
		val := attrs[name]
		delete(attrs, name)
		result.Fixes = append(result.Fixes, Fix{
			Field:   "attributes." + name,
			Message: fmt.Sprintf("dropped illegal attribute %q (was %v)", name, val),
		})
	}
}

// normalizeRelationKeys ensures relation key names are snake_case. Relation
// *targets* are deliberately left alone: a target is an identity, and rewriting
// one is a guess about which file was meant.
func normalizeRelationKeys(raw map[string]interface{}, result *NormalizeResult) {
	rels := asStringMapIface(raw["relations"])
	if rels == nil {
		return
	}
	var names []string
	for name := range rels {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, relName := range names {
		normRel := ToSnakeCase(relName)
		if normRel == relName {
			continue
		}
		targets := rels[relName]
		delete(rels, relName)
		if existing, ok := rels[normRel]; ok {
			merged := map[string]interface{}{normRel: existing}
			mergeRelMaps(merged, map[string]interface{}{normRel: targets})
			rels[normRel] = merged[normRel]
		} else {
			rels[normRel] = targets
		}
		result.Fixes = append(result.Fixes, Fix{
			Field:   "relations." + relName,
			Message: fmt.Sprintf("normalized relation key: %s → %s", relName, normRel),
		})
	}
}

// dropDangling removes relation targets that name nothing the corpus holds.
func dropDangling(raw map[string]interface{}, known map[string]bool, result *NormalizeResult) {
	rels := asStringMapIface(raw["relations"])
	if rels == nil {
		return
	}
	var names []string
	for name := range rels {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, relName := range names {
		list, ok := rels[relName].([]interface{})
		if !ok {
			continue
		}
		kept := make([]interface{}, 0, len(list))
		for _, t := range list {
			s, ok := t.(string)
			if ok && !known[s] {
				result.Fixes = append(result.Fixes, Fix{
					Field:   "relations." + relName,
					Message: fmt.Sprintf("dropped target %q, which names no entity", s),
				})
				continue
			}
			kept = append(kept, t)
		}
		if len(kept) == 0 {
			delete(rels, relName)
			continue
		}
		rels[relName] = kept
	}
	if len(rels) == 0 {
		delete(raw, "relations")
	}
}

// entityFromRawMap constructs an Entity from a raw YAML map.
func entityFromRawMap(raw map[string]interface{}) *Entity {
	e := &Entity{}
	if id, ok := raw["id"].(string); ok {
		e.ID = id
	}
	if t, ok := raw["type"].(string); ok {
		e.Type = t
	}

	if attrs, ok := raw["attributes"].(map[string]interface{}); ok {
		e.Attributes = make(map[string]any, len(attrs))
		for k, v := range attrs {
			e.Attributes[k] = v
		}
	}

	rels := asStringMapIface(raw["relations"])
	if rels != nil {
		e.Relations = make(map[string][]string)
		for k, v := range rels {
			if targets, ok := v.([]interface{}); ok {
				for _, t := range targets {
					if s, ok := t.(string); ok {
						e.Relations[k] = append(e.Relations[k], s)
					}
				}
			}
		}
	}

	return e
}

func asStringMapIface(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

// mergeRelMaps merges src relation entries into dst, deduplicating targets.
func mergeRelMaps(dst, src map[string]interface{}) {
	if dst == nil || src == nil {
		return
	}
	for k, v := range src {
		existing, ok := dst[k]
		if !ok {
			dst[k] = v
			continue
		}
		existList, _ := existing.([]interface{})
		srcList, _ := v.([]interface{})
		for _, t := range srcList {
			if !containsIface(existList, t) {
				existList = append(existList, t)
			}
		}
		dst[k] = existList
	}
}

func containsIface(list []interface{}, val interface{}) bool {
	for _, v := range list {
		if v == val {
			return true
		}
	}
	return false
}
