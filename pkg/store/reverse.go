package store

import (
	"strings"

	bolt "go.etcd.io/bbolt"
)

// FindByRelationTarget returns entity IDs that point to targetID via the given
// relation. It scans the OPS bucket with prefix "targetID\x00rel:<relation>\x00".
func (idx *boltIndex) FindByRelationTarget(relation, targetID string) ([]string, error) {
	prefix := []byte(targetID + sep + "rel:" + relation + sep)

	var ids []string
	err := idx.db.View(func(tx *bolt.Tx) error {
		ops := tx.Bucket(bucketOPS)
		c := ops.Cursor()
		for k, _ := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, _ = c.Next() {
			parts := strings.Split(string(k), sep)
			if len(parts) == 3 {
				ids = append(ids, parts[2])
			}
		}
		return nil
	})
	return ids, err
}

// FindReferrers returns unique entity IDs that point to the given entity via
// any relation. It seeks the OPS bucket with prefix "id\x00rel:" to find all
// incoming relation references regardless of relation name.
func (idx *boltIndex) FindReferrers(id string) ([]string, error) {
	prefix := []byte(id + sep + "rel:")

	seen := make(map[string]struct{})
	var ids []string
	err := idx.db.View(func(tx *bolt.Tx) error {
		ops := tx.Bucket(bucketOPS)
		c := ops.Cursor()
		for k, _ := c.Seek(prefix); k != nil && hasPrefix(k, prefix); k, _ = c.Next() {
			parts := strings.Split(string(k), sep)
			if len(parts) == 3 {
				subj := parts[2]
				if _, ok := seen[subj]; !ok {
					seen[subj] = struct{}{}
					ids = append(ids, subj)
				}
			}
		}
		return nil
	})
	return ids, err
}
