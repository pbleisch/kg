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
	return json.NewEncoder(w).Encode(entities)
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
