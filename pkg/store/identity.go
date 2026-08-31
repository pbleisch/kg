package store

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

// Identity is what a file's location says the entity is.
//
// The path is the identity (ADR-0029): a file at
// <kgDir>/entities/<type>/<local>.yaml is <type>:<local>, with <local>
// percent-decoded — the inverse of the encoding [EntityPath] applies. The
// `id:` and `type:` fields in the file are a redundant echo kept so that a
// file read on its own still says what it is (P1); when they disagree with the
// path the load refuses, and when they are absent the path supplies them.
type Identity struct {
	ID    string
	Type  string
	Local string
}

// IdentityAt derives the identity of the entity file at path.
//
// It returns a *LoadError when the path is not one an entity can have: not
// under <kgDir>/entities/<type>/, nested deeper than one directory, or named
// with an escape sequence that does not decode.
func IdentityAt(kgDir, path string) (Identity, error) {
	entitiesDir := filepath.Join(kgDir, "entities")
	rel, err := filepath.Rel(entitiesDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return Identity{}, refuse(Structural{
			Code:  "bad_path",
			Files: []string{path},
			Message: fmt.Sprintf("file is not under %s, so it has no identity",
				entitiesDir),
		})
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 {
		return Identity{}, refuse(Structural{
			Code:  "bad_path",
			Files: []string{path},
			Message: fmt.Sprintf("entity files live at entities/<type>/<local-id>.yaml; %q is %d level(s) deep",
				filepath.ToSlash(rel), len(parts)-1),
		})
	}
	typeName := parts[0]
	base := strings.TrimSuffix(parts[1], ".yaml")
	local, err := url.PathUnescape(base)
	if err != nil {
		return Identity{}, refuse(Structural{
			Code:    "bad_path",
			Files:   []string{path},
			Message: fmt.Sprintf("filename %q is not a decodable local id: %v", parts[1], err),
		})
	}
	if typeName == "" || local == "" {
		return Identity{}, refuse(Structural{
			Code:    "bad_path",
			Files:   []string{path},
			Message: "an entity path needs a non-empty type directory and filename",
		})
	}
	return Identity{ID: typeName + ":" + local, Type: typeName, Local: local}, nil
}

// A Structural is one reason a load refused.  Structure is enforced (P3): the
// identity rules and the grammar rules, and nothing else — see ADR-0028 for
// what is in each class and why a dangling relation target is in neither.
type Structural struct {
	Code    string   `json:"code"`
	ID      string   `json:"id,omitempty"`
	Files   []string `json:"files"`
	Field   string   `json:"field,omitempty"`
	Message string   `json:"message"`
}

func (s Structural) Error() string {
	where := strings.Join(s.Files, " and ")
	if where == "" {
		return s.Message
	}
	return where + ": " + s.Message
}

// An Advisory is content: reported, never blocking.  An unknown attribute, a
// schema violation, a relation target that resolves to nothing.  A graph full
// of advisories still loads.
type Advisory struct {
	Code    string `json:"code"`
	ID      string `json:"id,omitempty"`
	File    string `json:"file,omitempty"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (a Advisory) String() string {
	if a.ID != "" {
		return a.ID + ": " + a.Message
	}
	return a.Message
}

// A LoadError reports a corpus that will not load, naming every offending
// file.  It is what every read returns on a corpus that breaks P3's enforced
// half; [ListEntityFiles] is the one read that does not, because the repair
// has to be able to walk what it is repairing.
type LoadError struct {
	Errors []Structural
}

func (e *LoadError) Error() string {
	if len(e.Errors) == 1 {
		return "graph refused: " + e.Errors[0].Error()
	}
	msgs := make([]string, 0, len(e.Errors))
	for i, s := range e.Errors {
		if i == 3 {
			msgs = append(msgs, fmt.Sprintf("… and %d more", len(e.Errors)-3))
			break
		}
		msgs = append(msgs, s.Error())
	}
	return fmt.Sprintf("graph refused: %d structural error(s): %s",
		len(e.Errors), strings.Join(msgs, "; "))
}

func refuse(errs ...Structural) error { return &LoadError{Errors: errs} }

// LoadRefused reports whether err is, or wraps, a structural refusal, and
// returns the reasons.  Callers branch on it rather than on the message
// (ADR-0018).
func LoadRefused(err error) ([]Structural, bool) {
	var le *LoadError
	if !errors.As(err, &le) {
		return nil, false
	}
	return le.Errors, true
}

// CheckPath reports whether a file sits where kg would write its identity, and
// what is wrong when it does not.
//
// A name kg would have encoded differently is unreachable by a point read,
// which computes the path from the identity. It is also how one identity ends
// up in two files without the filesystem noticing: "mäyrä.yaml" and
// "m%C3%A4yr%C3%A4.yaml" are two paths and one entity. Repairing it means
// renaming a file and deciding which of the two survives, so nothing in kg
// does it automatically.
func CheckPath(path string, id Identity) *Structural {
	base := filepath.Base(path)
	want := encodeLocalID(id.Local) + ".yaml"
	if base == want {
		return nil
	}
	return &Structural{
		Code: "non_canonical_path", ID: id.ID, Files: []string{path},
		Message: fmt.Sprintf("identity %q is stored at %q but kg writes and reads it at %q, so no point read reaches this file",
			id.ID, base, want),
	}
}

// CheckEntityFile applies the rules that need one file and its own path: the
// file sits where kg would write it, the declared identity agrees with the
// derived one, and the file's names are legal.  Collisions need more than one
// file and are checked by the walk.
func CheckEntityFile(path string, id Identity, e *Entity) []Structural {
	var out []Structural
	if s := CheckPath(path, id); s != nil {
		out = append(out, *s)
	}
	if e.ID != "" && e.ID != id.ID {
		out = append(out, Structural{
			Code: "id_disagreement", ID: id.ID, Files: []string{path}, Field: "id",
			Message: fmt.Sprintf("declares id %q but its location says %q; the path is the identity (ADR-0029)",
				e.ID, id.ID),
		})
	}
	if e.Type != "" && e.Type != id.Type {
		out = append(out, Structural{
			Code: "type_disagreement", ID: id.ID, Files: []string{path}, Field: "type",
			Message: fmt.Sprintf("declares type %q but is stored under %q; the directory is the type (ADR-0029)",
				e.Type, id.Type),
		})
	}
	if bad := IllegalAttributeNames(e.Attributes); len(bad) > 0 {
		quoted := make([]string, len(bad))
		for i, b := range bad {
			quoted[i] = fmt.Sprintf("%q", b)
		}
		out = append(out, Structural{
			Code: "illegal_attribute", ID: id.ID, Files: []string{path}, Field: "attributes",
			Message: fmt.Sprintf("illegal attribute name(s): %s (must match [a-z][a-z0-9_]*)",
				strings.Join(quoted, ", ")),
		})
	}
	for rel := range e.Relations {
		if rel == "" {
			out = append(out, Structural{
				Code: "bad_relation", ID: id.ID, Files: []string{path}, Field: "relations",
				Message: "a relation with an empty name names nothing",
			})
		}
	}
	return out
}

// adopt fills an entity's identity in from its path.  A file may leave `id:`
// and `type:` out; the path still says what it is, and no caller should have
// to know which of the two it got.
func adopt(e *Entity, id Identity) {
	e.ID = id.ID
	e.Type = id.Type
}

// Collisions returns one Structural per identity claimed by more than one
// file, naming every file that claims it.
func Collisions(byID map[string][]string) []Structural {
	var ids []string
	for id, paths := range byID {
		if len(paths) > 1 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]Structural, 0, len(ids))
	for _, id := range ids {
		paths := append([]string(nil), byID[id]...)
		sort.Strings(paths)
		out = append(out, Structural{
			Code: "identity_collision", ID: id, Files: paths,
			Message: fmt.Sprintf("identity %q is claimed by %d files; only one of them is reachable",
				id, len(paths)),
		})
	}
	return out
}

// SortStructural orders refusals by file then code, so two runs over unchanged
// files report the same bytes in the same order (P6).
func SortStructural(errs []Structural) {
	sort.SliceStable(errs, func(i, j int) bool {
		a, b := errs[i], errs[j]
		af, bf := "", ""
		if len(a.Files) > 0 {
			af = a.Files[0]
		}
		if len(b.Files) > 0 {
			bf = b.Files[0]
		}
		if af != bf {
			return af < bf
		}
		return a.Code < b.Code
	})
}
