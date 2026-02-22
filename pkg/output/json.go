package output

import (
	"encoding/json"
	"io"

	"github.com/pbleisch/kg/pkg/store"
)

type jsonFormatter struct{}

func (f *jsonFormatter) FormatEntity(w io.Writer, e *store.Entity) error {
	return json.NewEncoder(w).Encode(e)
}

func (f *jsonFormatter) FormatEntities(w io.Writer, entities []*store.Entity) error {
	// Ensure we never emit `null` for empty result sets, and filter out any
	// nil elements that might have crept into the slice.
	if entities == nil {
		entities = []*store.Entity{}
	}
	clean := make([]*store.Entity, 0, len(entities))
	for _, e := range entities {
		if e != nil {
			clean = append(clean, e)
		}
	}
	return json.NewEncoder(w).Encode(clean)
}

func (f *jsonFormatter) FormatError(w io.Writer, code string, message string, suggestions []string) error {
	obj := map[string]any{
		"error":   code,
		"message": message,
	}
	if len(suggestions) > 0 {
		obj["suggestions"] = suggestions
	}
	return json.NewEncoder(w).Encode(obj)
}
