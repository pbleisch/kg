package schema

import (
	"github.com/pbleisch/kg/pkg/store"
)

// TypeList is the declared attribute type that makes a field multi-valued.
//
// It has been in the schema language, and declared on real fields, since
// before anything implemented it.  What it now decides is not only how a value
// is validated but **what a write does** to it — whether a second document's
// items are kept beside the first's or destroy them (ADR-0030).  That makes
// `.kg/schema/` load-bearing: it is content, nothing is refused on it, but a
// rewrite has to carry it.
const TypeList = "list"

// FieldKinds answers, for one entity type, the only schema question a write
// asks: is this attribute multi-valued?
//
// The zero value answers "no" for everything, which is the right answer for a
// type with no schema: an undeclared field is a scalar, and content is advised
// rather than enforced (P3).
type FieldKinds struct {
	lists map[string]bool
}

// IsList reports whether the schema declares attr multi-valued.
func (k FieldKinds) IsList(attr string) bool { return k.lists[attr] }

// Any reports whether the type declares any multi-valued attribute.
func (k FieldKinds) Any() bool { return len(k.lists) > 0 }

// KindsOf reads one schema's field kinds.  A type with no schema, or a schema
// that will not parse, yields the zero FieldKinds rather than an error: a
// missing schema is a legitimate state, not a failure of the write.
func KindsOf(s *Schema) FieldKinds {
	if s == nil {
		return FieldKinds{}
	}
	var lists map[string]bool
	for name, def := range s.Attributes {
		if def.Type == TypeList {
			if lists == nil {
				lists = map[string]bool{}
			}
			lists[name] = true
		}
	}
	return FieldKinds{lists: lists}
}

// Kinds answers the same question for every type in one graph, reading each
// schema file at most once.
//
// A batch that writes 1,558 records over 1,298 identities — a real document —
// asks this question once per attribute per record.  Reading the schema file
// each time would make the fold quadratic in nothing useful.
type Kinds struct {
	kgDir string
	byTyp map[string]FieldKinds
}

// NewKinds builds a per-graph cache.  A nil *Kinds is usable and reads the
// schema on every call, so a caller with one entity need not build one.
func NewKinds(kgDir string) *Kinds {
	return &Kinds{kgDir: kgDir, byTyp: map[string]FieldKinds{}}
}

// For returns the field kinds for one entity type.
func (k *Kinds) For(typeName string) FieldKinds {
	if k == nil {
		return LoadFieldKinds("", typeName)
	}
	norm := store.ToSnakeCase(typeName)
	if fk, ok := k.byTyp[norm]; ok {
		return fk
	}
	fk := LoadFieldKinds(k.kgDir, norm)
	k.byTyp[norm] = fk
	return fk
}

// LoadFieldKinds reads one type's schema and reports its field kinds.
func LoadFieldKinds(kgDir, typeName string) FieldKinds {
	if kgDir == "" {
		return FieldKinds{}
	}
	s, err := LoadSchema(kgDir, typeName)
	if err != nil {
		return FieldKinds{}
	}
	return KindsOf(s)
}
