package query

import (
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
)

// Get retrieves a single entity by ID.
//
// One file, one read. There is no index to open first and nothing to sync, so
// the cost of this call does not depend on the size of the graph — which is
// the single-entity half of P4's budget (ADR-0026).
func Get(kgDir, id string) (*store.Entity, error) {
	e, err := store.ReadEntity(kgDir, id)
	if err != nil {
		return nil, &QueryError{
			Code:    "entity_not_found",
			Message: fmt.Sprintf("entity %q not found", id),
		}
	}
	return e, nil
}
