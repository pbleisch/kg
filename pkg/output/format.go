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

// SchemaInfo holds schema data for formatting.
type SchemaInfo struct {
	Type        string
	Description string
	Attributes  []SchemaAttr
	Relations   []SchemaRel
}

// SchemaAttr describes a single schema attribute.
type SchemaAttr struct {
	Name     string
	Type     string
	Required bool
}

// SchemaRel describes a single schema relation.
type SchemaRel struct {
	Name        string
	Target      string
	Cardinality string
}

// PathResult holds a single verbose path traversal result.
type PathResult struct {
	ID    string
	Depth int
	Via   string
}

// Formatter writes entities and errors in a specific format.
type Formatter interface {
	FormatEntity(w io.Writer, e *store.Entity) error
	FormatEntities(w io.Writer, entities []*store.Entity) error
	FormatError(w io.Writer, code string, message string, suggestions []string) error
	FormatSchema(w io.Writer, s SchemaInfo) error
	FormatTypeList(w io.Writer, names []string) error
	FormatPathIDs(w io.Writer, ids []string) error
	FormatPathVerbose(w io.Writer, results []PathResult) error
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
