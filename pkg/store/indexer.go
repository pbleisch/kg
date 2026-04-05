package store

// Indexer defines the interface for a knowledge graph index backend.
// Implementations must support CRUD operations on entities and reverse
// lookups. The index is a disposable cache rebuilt from YAML source files.
type Indexer interface {
	// Close releases resources held by the index.
	Close() error

	// AddEntity indexes all quads (type, attributes, relations) for an entity.
	AddEntity(e *Entity) error

	// RemoveEntity removes all indexed data for the given entity ID.
	RemoveEntity(id string) error

	// UpsertEntity removes old indexed data and adds new data for an entity
	// in a single atomic operation, avoiding two separate transactions.
	UpsertEntity(e *Entity) error

	// GetEntity reconstructs an entity from indexed data.
	GetEntity(id string) (*Entity, error)

	// FindByType returns IDs of all entities with the given type.
	FindByType(typeName string) ([]string, error)

	// FindByRelationTarget returns IDs of entities that point to targetID
	// via the named relation.
	FindByRelationTarget(relation, targetID string) ([]string, error)

	// FindReferrers returns IDs of entities that point to the given entity
	// via any relation. Used by merge to efficiently find incoming links
	// without scanning all entities on disk.
	FindReferrers(id string) ([]string, error)
}

// BatchIndexer is an optional interface for backends that support batched
// writes in a single transaction, reducing per-entity transaction overhead.
type BatchIndexer interface {
	Indexer
	UpsertEntities(entities []*Entity) error
	RemoveEntities(ids []string) error
}
