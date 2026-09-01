package batch

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/pbleisch/kg/pkg/schema"
	"github.com/pbleisch/kg/pkg/store"
)

// newKey mints a key for a batch that is a fresh submission rather than a
// retry of one.  A single interactive write is exactly that: `kg create x`
// twice is two submissions and the second must still hit the conflict policy,
// where re-applying one manifest file is a retry and must not.
func newKey(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return KeyFor(prefix, "unrepeatable")
	}
	return KeyFor(prefix, hex.EncodeToString(b[:]))
}

func singleton(kgDir string, r Record, opts Options) (*Result, error) {
	env, err := NewEnvelope(Version, newKey(string(r.Op)))
	if err != nil {
		return nil, err
	}
	return Apply(kgDir, env, []Record{r}, opts)
}

// CreateEntity submits one new entity through the write boundary.  With upsert
// set, an existing identity is merged into rather than refused.
func CreateEntity(kgDir, typeName, localID string, attrs map[string]string, upsert bool) (*store.Entity, error) {
	typed, err := schema.CoerceAttrs(kgDir, typeName, attrs)
	if err != nil {
		return nil, err
	}
	id := store.ToSnakeCase(typeName) + ":" + store.ToSnakeCase(localID)
	rec := Record{
		Op: OpCreate,
		Entity: &store.Entity{
			ID:         id,
			Type:       store.ToSnakeCase(typeName),
			Attributes: typed,
		},
	}
	opts := Options{}
	if upsert {
		rec.Op = OpUpsert
	}
	if _, err := singleton(kgDir, rec, opts); err != nil {
		return nil, err
	}
	return store.ReadEntity(kgDir, rec.Entity.ID)
}

// SetAttrs submits an attribute update through the write boundary.
func SetAttrs(kgDir, id string, attrs map[string]string) (*store.Entity, error) {
	target := store.NormalizeEntityID(id)
	typeName, _, err := store.ParseEntityID(target)
	if err != nil {
		return nil, err
	}
	typed, cerr := schema.CoerceAttrs(kgDir, typeName, attrs)
	if cerr != nil {
		return nil, cerr
	}
	rec := Record{Op: OpSet, Target: target, Attributes: typed}
	if _, err := singleton(kgDir, rec, Options{}); err != nil {
		return nil, err
	}
	return store.ReadEntity(kgDir, target)
}

// LinkRef submits one relation through the write boundary.  Both ends must
// already exist: the boundary does not mint the entities it is pointed at.
func LinkRef(kgDir, from, rel, to string) error {
	_, err := singleton(kgDir, Record{Op: OpLink, From: from, Rel: rel, To: to}, Options{})
	return err
}

// UnlinkRef removes one relation through the write boundary.
func UnlinkRef(kgDir, from, rel, to string) error {
	_, err := singleton(kgDir, Record{Op: OpUnlink, From: from, Rel: rel, To: to}, Options{})
	return err
}

// UnsetAttrs removes attributes from an entity through the write boundary.
//
// It is the only way kg has to remove an attribute, and it is what removes an
// item no contributor will ever replace: a multi-valued attribute written with
// no contributor carries no source, so replace-by-contributor never touches it
// and a hand-authored edit survives every re-ingest until this says otherwise
// (ADR-0030).
func UnsetAttrs(kgDir, id string, names []string) (*store.Entity, error) {
	target := store.NormalizeEntityID(id)
	if _, err := singleton(kgDir, Record{Op: OpUnset, Target: target, Unset: names}, Options{}); err != nil {
		return nil, err
	}
	return store.ReadEntity(kgDir, target)
}

// DeleteRef removes one entity through the write boundary.
func DeleteRef(kgDir, id string) error {
	_, err := singleton(kgDir, Record{Op: OpDelete, Target: id}, Options{})
	return err
}
