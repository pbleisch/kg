package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// BoltDB bucket names used for quad-like indexing.
var (
	// bucketSPO stores quads keyed by subject: key="subject\x00predicate\x00object", value="1"
	bucketSPO = []byte("spo")
	// bucketOPS stores a reverse index: key="object\x00predicate\x00subject", value="1"
	bucketOPS = []byte("ops")
)

const sep = "\x00"

// boltIndex wraps a BoltDB store providing quad-like graph index operations.
type boltIndex struct {
	db *bolt.DB
}

// OpenIndex opens or creates an index at <kgDir>/.cache/. Returns an Indexer
// backed by the configured backend. Selection order:
//  1. Read .kg/config.yaml "backend" field (bolt, ladybug, or auto)
//  2. If "auto" (default): prefer ladybug if compiled in, else bolt
//  3. If explicit: use that backend or error if unavailable
func OpenIndex(kgDir string) (Indexer, error) {
	backend := readBackendConfig(kgDir)
	switch backend {
	case "ladybug":
		return openLadybug(kgDir)
	case "bolt":
		return openBolt(kgDir)
	default: // "auto" or empty
		if ladybugAvailable {
			return openLadybug(kgDir)
		}
		return openBolt(kgDir)
	}
}

func openBolt(kgDir string) (Indexer, error) {
	cacheDir := filepath.Join(kgDir, ".cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}

	dbPath := filepath.Join(cacheDir, "cayley.bolt")
	db, err := bolt.Open(dbPath, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open bolt db: %w", err)
	}

	// Ensure buckets exist.
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketSPO, bucketOPS} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}

	return &boltIndex{db: db}, nil
}

// Close closes the underlying BoltDB store.
func (idx *boltIndex) Close() error {
	return idx.db.Close()
}

func spoKey(s, p, o string) []byte { return []byte(s + sep + p + sep + o) }
func opsKey(o, p, s string) []byte { return []byte(o + sep + p + sep + s) }

// AddEntity inserts quads for an entity:
//   - (id, "type", typeName)
//   - (id, "attr:<name>", jsonValue) for each attribute
//   - (id, "rel:<name>", targetID) for each relation target
func (idx *boltIndex) AddEntity(e *Entity) error {
	type triple struct{ s, p, o string }
	var triples []triple

	triples = append(triples, triple{e.ID, "type", e.Type})

	for k, v := range e.Attributes {
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("marshal attr %s: %w", k, err)
		}
		triples = append(triples, triple{e.ID, "attr:" + k, string(encoded)})
	}

	for rel, targets := range e.Relations {
		for _, t := range targets {
			triples = append(triples, triple{e.ID, "rel:" + rel, t})
		}
	}

	return idx.db.Update(func(tx *bolt.Tx) error {
		spo := tx.Bucket(bucketSPO)
		ops := tx.Bucket(bucketOPS)
		val := []byte("1")
		for _, tr := range triples {
			if err := spo.Put(spoKey(tr.s, tr.p, tr.o), val); err != nil {
				return err
			}
			if err := ops.Put(opsKey(tr.o, tr.p, tr.s), val); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveEntity removes all quads with the given subject ID.
func (idx *boltIndex) RemoveEntity(id string) error {
	prefix := []byte(id + sep)
	return idx.db.Update(func(tx *bolt.Tx) error {
		spo := tx.Bucket(bucketSPO)
		ops := tx.Bucket(bucketOPS)

		// Collect keys to delete (can't delete while iterating cursor).
		var toDelete [][]byte
		c := spo.Cursor()
		for k, _ := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, _ = c.Next() {
			toDelete = append(toDelete, append([]byte(nil), k...))
		}

		for _, k := range toDelete {
			parts := splitKey(k)
			if len(parts) == 3 {
				if err := ops.Delete(opsKey(parts[2], parts[1], parts[0])); err != nil {
					return err
				}
			}
			if err := spo.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetEntity reconstructs an entity from its stored quads.
func (idx *boltIndex) GetEntity(id string) (*Entity, error) {
	e := &Entity{
		ID:         id,
		Attributes: make(map[string]any),
		Relations:  make(map[string][]string),
	}

	prefix := []byte(id + sep)
	found := false

	err := idx.db.View(func(tx *bolt.Tx) error {
		spo := tx.Bucket(bucketSPO)
		c := spo.Cursor()
		for k, _ := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, _ = c.Next() {
			found = true
			parts := splitKey(k)
			if len(parts) != 3 {
				continue
			}
			pred, obj := parts[1], parts[2]

			switch {
			case pred == "type":
				e.Type = obj
			case strings.HasPrefix(pred, "attr:"):
				var v any
				if err := json.Unmarshal([]byte(obj), &v); err != nil {
					v = obj
				}
				e.Attributes[strings.TrimPrefix(pred, "attr:")] = v
			case strings.HasPrefix(pred, "rel:"):
				rel := strings.TrimPrefix(pred, "rel:")
				e.Relations[rel] = append(e.Relations[rel], obj)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("entity not found: %s", id)
	}
	return e, nil
}

// FindByType returns IDs of entities with the given type.
func (idx *boltIndex) FindByType(typeName string) ([]string, error) {
	// Look up the reverse index: object=typeName, predicate="type"
	target := []byte(typeName + sep + "type" + sep)

	var ids []string
	err := idx.db.View(func(tx *bolt.Tx) error {
		ops := tx.Bucket(bucketOPS)
		c := ops.Cursor()
		for k, _ := c.Seek(target); k != nil && hasPrefix(k, target); k, _ = c.Next() {
			parts := splitKey(k)
			if len(parts) == 3 {
				ids = append(ids, parts[2])
			}
		}
		return nil
	})
	return ids, err
}

func hasPrefix(b, prefix []byte) bool {
	return len(b) >= len(prefix) && string(b[:len(prefix)]) == string(prefix)
}

func splitKey(k []byte) []string {
	return strings.Split(string(k), sep)
}
