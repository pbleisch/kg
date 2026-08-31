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
		// A structural refusal is not a miss. Reporting "not found" for a file
		// that is right there but breaks P3 is how 265 shadowed files stayed
		// invisible for three months (ADR-0028).
		if errs, refused := store.LoadRefused(err); refused {
			return nil, &QueryError{
				Code:        "entity_refused",
				Message:     err.Error(),
				Suggestions: suggestRepair(errs),
				Err:         err,
			}
		}
		return nil, &QueryError{
			Code:    "entity_not_found",
			Message: fmt.Sprintf("entity %q not found", id),
		}
	}
	return e, nil
}

// suggestRepair names the command that fixes what the load refused, for the
// classes a command can fix (ADR-0018: an error carries a suggestion where
// there is one).
func suggestRepair(errs []store.Structural) []string {
	for _, e := range errs {
		switch e.Code {
		case "id_disagreement", "type_disagreement", "illegal_attribute":
			return []string{"kg validate --fix rewrites the file to agree with its path"}
		case "identity_collision", "non_canonical_path":
			return []string{"kg validate names both files; removing or renaming one is a decision kg will not make for you"}
		}
	}
	return nil
}
