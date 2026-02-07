package output

import (
	"io"

	"github.com/pbleisch/kg/pkg/store"
)

// Format specifies the output format.
type Format string

const (
	FormatTOON  Format = "toon"
	FormatJSON  Format = "json"
	FormatHuman Format = "human"
)

// Formatter writes entities and errors in a specific format.
type Formatter interface {
	FormatEntity(w io.Writer, e *store.Entity) error
	FormatEntities(w io.Writer, entities []*store.Entity) error
	FormatError(w io.Writer, code string, message string, suggestions []string) error
}

// NewFormatter returns a Formatter for the given format.
func NewFormatter(format Format) Formatter {
	switch format {
	case FormatJSON:
		return &jsonFormatter{}
	case FormatTOON:
		return &toonFormatter{}
	case FormatHuman:
		return &humanFormatter{}
	default:
		return &toonFormatter{}
	}
}
