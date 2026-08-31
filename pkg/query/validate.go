package query

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// A Report is what one validation pass found: what refuses the load, and what
// is merely advised.
//
// The two lists are separate because they are separate rules with separate
// severities and must not share a setting (P3). Structural is the enforced
// half — identity and grammar; Advisories is the advised half — schema,
// unknown attributes, and relation targets that resolve to nothing. A caller
// decides by asking [Report.OK], never by counting.
type Report struct {
	// Files is how many entity files were walked, including the ones that
	// refuse to load.
	Files int `json:"files"`
	// Entities is how many distinct identities those files hold.
	Entities   int                `json:"entities"`
	Structural []store.Structural `json:"structural"`
	Advisories []store.Advisory   `json:"advisories"`
}

// OK reports whether the graph loads. Advisories do not affect it: a graph
// full of warnings still loads.
func (r *Report) OK() bool { return len(r.Structural) == 0 }

// Counts returns how many findings carry each code, for a summary line.
func (r *Report) Counts() map[string]int {
	out := make(map[string]int)
	for _, s := range r.Structural {
		out[s.Code]++
	}
	for _, a := range r.Advisories {
		out[a.Code]++
	}
	return out
}

// Validate walks every entity file and reports both halves of P3.
//
// It deliberately does not use a load. A load refuses (ADR-0028), and the one
// read that has to work on a corpus that will not load is the one that says
// what is wrong with it. So this walks with [store.ListEntityFiles], derives
// each file's identity from its path, and checks everything itself — which is
// also why it reports every offending file rather than the first.
func Validate(kgDir string) (*Report, error) {
	files, err := store.ListEntityFiles(kgDir)
	if err != nil {
		return nil, fmt.Errorf("walking entities: %w", err)
	}
	rep := &Report{Files: len(files)}

	byID := make(map[string][]string, len(files))
	// The corpus as the path rule sees it: identity → the entity at it, and
	// the file it was actually read from. The two differ for a file kg would
	// not have written, and an advisory that names the wrong file is worse
	// than one that names none.
	held := make(map[string]*store.Entity, len(files))
	from := make(map[string]string, len(files))

	for _, f := range files {
		ident, err := store.IdentityAt(kgDir, f.Path)
		if err != nil {
			if errs, refused := store.LoadRefused(err); refused {
				rep.Structural = append(rep.Structural, errs...)
				continue
			}
			return nil, err
		}
		rep.Structural = append(rep.Structural, store.CheckEntityFile(f.Path, ident, f.Entity)...)
		byID[ident.ID] = append(byID[ident.ID], f.Path)
		e := *f.Entity
		e.ID, e.Type = ident.ID, ident.Type
		held[ident.ID] = &e
		from[ident.ID] = f.Path
	}
	rep.Entities = len(byID)

	rep.Structural = append(rep.Structural, store.Collisions(byID)...)
	store.SortStructural(rep.Structural)

	rep.Advisories = advise(kgDir, held, from)
	return rep, nil
}

// advise runs the content rules. Every one of them is reported and none of
// them blocks: a graph under construction is legitimately incomplete (P3).
func advise(kgDir string, held map[string]*store.Entity, from map[string]string) []store.Advisory {
	var out []store.Advisory

	ids := make([]string, 0, len(held))
	for id := range held {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	schemas, _ := schema.LoadAllSchemas(kgDir)
	typedSchema := make(map[string]*schema.Schema, len(schemas))
	for _, s := range schemas {
		typedSchema[store.ToSnakeCase(s.Type)] = s
	}
	noSchema := make(map[string]int)

	for _, id := range ids {
		e := held[id]
		file := from[id]

		// A relation target that resolves to nothing. Reported, never fatal:
		// it is a property of the corpus at one instant, and OKF §11 forbids a
		// consumer rejecting a bundle over it (ADR-0028).
		rels := make([]string, 0, len(e.Relations))
		for rel := range e.Relations {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for _, rel := range rels {
			targets := append([]string(nil), e.Relations[rel]...)
			sort.Strings(targets)
			for _, t := range targets {
				if _, ok := held[t]; ok {
					continue
				}
				out = append(out, store.Advisory{
					Code: "dangling_relation", ID: id, File: file,
					Field:   "relations." + rel,
					Message: fmt.Sprintf("%s → %q names no entity", rel, t),
				})
			}
		}

		s, ok := typedSchema[e.Type]
		if !ok {
			noSchema[e.Type]++
			continue
		}
		for _, ve := range schema.ValidateEntity(s, e) {
			out = append(out, store.Advisory{
				Code: ve.Code, ID: id, File: file,
				Field: ve.Field, Message: ve.Message,
			})
		}
	}

	// One advisory per type with no schema, not one per entity: on this corpus
	// that difference is 5 lines against 27,000.
	var types []string
	for t := range noSchema {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		out = append(out, store.Advisory{
			Code: "no_schema", Field: t,
			File:    filepath.Join(kgDir, "schema", t+".yaml"),
			Message: fmt.Sprintf("%d entities are of type %q, which has no schema; nothing about their content is checked", noSchema[t], t),
		})
	}
	return out
}
