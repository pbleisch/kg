package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pbleisch/kg/pkg/store"
	"gopkg.in/yaml.v3"
)

// AttrDef defines an attribute's type and whether it is required.
type AttrDef struct {
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
}

// RelDef defines a relation's target type and cardinality.
type RelDef struct {
	Target      string `yaml:"target"`
	Cardinality string `yaml:"cardinality"`
}

// Schema describes the expected shape of an entity type.
type Schema struct {
	Type        string             `yaml:"type"`
	Description string             `yaml:"description"`
	Attributes  map[string]AttrDef `yaml:"attributes"`
	Relations   map[string]RelDef  `yaml:"relations"`
}

// ValidationError reports a single validation issue on a field.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// LoadSchema loads a single schema file from <kgDir>/schema/<typeName>.yaml.
// The typeName is normalized to snake_case before looking up the file.
func LoadSchema(kgDir, typeName string) (*Schema, error) {
	norm := store.ToSnakeCase(typeName)
	path := filepath.Join(kgDir, "schema", norm+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading schema %s: %w", typeName, err)
	}
	var s Schema
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing schema %s: %w", typeName, err)
	}
	return &s, nil
}

// LoadAllSchemas loads every .yaml file in <kgDir>/schema/ and returns
// a map keyed by the schema's Type field.
func LoadAllSchemas(kgDir string) (map[string]*Schema, error) {
	dir := filepath.Join(kgDir, "schema")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading schema directory: %w", err)
	}
	schemas := make(map[string]*Schema)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		typeName := strings.TrimSuffix(e.Name(), ".yaml")
		s, err := LoadSchema(kgDir, typeName)
		if err != nil {
			return nil, err
		}
		schemas[s.Type] = s
	}
	return schemas, nil
}

// ValidateEntity checks an entity against a schema and returns all issues found.
func ValidateEntity(schema *Schema, entity *store.Entity) []ValidationError {
	var errs []ValidationError

	// Check required attributes are present.
	for name, def := range schema.Attributes {
		if def.Required {
			val, ok := entity.Attributes[name]
			if !ok || val == nil {
				errs = append(errs, ValidationError{
					Field:   "attributes." + name,
					Message: "required attribute is missing",
				})
				continue
			}
		}
	}

	// Check attribute types.
	for name, val := range entity.Attributes {
		def, ok := schema.Attributes[name]
		if !ok || val == nil {
			continue
		}
		if !checkAttrType(def.Type, val) {
			errs = append(errs, ValidationError{
				Field:   "attributes." + name,
				Message: fmt.Sprintf("expected type %s", def.Type),
			})
		}
	}

	// Check cardinality constraints on relations.
	for name, def := range schema.Relations {
		targets, ok := entity.Relations[name]
		if !ok {
			continue
		}
		if def.Cardinality == "one" && len(targets) > 1 {
			errs = append(errs, ValidationError{
				Field:   "relations." + name,
				Message: fmt.Sprintf("cardinality is one but found %d targets", len(targets)),
			})
		}
	}

	return errs
}

// checkAttrType returns true if val is compatible with the given schema type.
func checkAttrType(typeName string, val any) bool {
	switch typeName {
	case "string":
		_, ok := val.(string)
		return ok
	case "number":
		switch val.(type) {
		case int, int64, float64:
			return true
		}
		return false
	case "boolean":
		_, ok := val.(bool)
		return ok
	case "date", "datetime":
		// Dates are stored as strings; accept string values.
		_, ok := val.(string)
		return ok
	default:
		return true
	}
}
