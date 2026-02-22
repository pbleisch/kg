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

	// GetEntity reconstructs an entity from indexed data.
	GetEntity(id string) (*Entity, error)

	// FindByType returns IDs of all entities with the given type.
	FindByType(typeName string) ([]string, error)

	// FindByRelationTarget returns IDs of entities that point to targetID
	// via the named relation.
	FindByRelationTarget(relation, targetID string) ([]string, error)
}
