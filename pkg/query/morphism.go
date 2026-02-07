package query

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Morphism is a reusable path pattern.
type Morphism struct {
	Pattern     string `yaml:"pattern"`
	Description string `yaml:"description,omitempty"`
}

// LoadMorphisms reads morphisms from .kg/morphisms.yaml.
func LoadMorphisms(kgDir string) (map[string]Morphism, error) {
	path := filepath.Join(kgDir, "morphisms.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m map[string]Morphism
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse morphisms.yaml: %w", err)
	}
	return m, nil
}

// SaveMorphisms writes morphisms to .kg/morphisms.yaml.
func SaveMorphisms(kgDir string, morphisms map[string]Morphism) error {
	path := filepath.Join(kgDir, "morphisms.yaml")
	data, err := yaml.Marshal(morphisms)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// DefineMorphism adds or updates a named morphism, persisting to disk.
func DefineMorphism(kgDir, name, pattern, description string) error {
	m, err := LoadMorphisms(kgDir)
	if err != nil {
		return err
	}
	if m == nil {
		m = make(map[string]Morphism)
	}
	m[name] = Morphism{Pattern: pattern, Description: description}
	return SaveMorphisms(kgDir, m)
}

// ExpandMorphism looks up a morphism by name and parses its pattern into
// PathSteps. Supported pattern tokens: -> rel, ->* rel, <- rel, <-* rel.
// Back-direction steps (<- and <-*) are not yet supported and return an error.
func ExpandMorphism(kgDir, name string) ([]PathStep, error) {
	m, err := LoadMorphisms(kgDir)
	if err != nil {
		return nil, err
	}
	morph, ok := m[name]
	if !ok {
		return nil, &QueryError{
			Code:    "morphism_not_found",
			Message: fmt.Sprintf("no morphism named %q", name),
		}
	}
	return ParsePattern(morph.Pattern)
}

// ParsePattern converts a pattern string like "-> worksOn <- worksOn"
// into a slice of PathSteps. Only forward steps (-> and ->*) are
// currently supported.
func ParsePattern(pattern string) ([]PathStep, error) {
	tokens := strings.Fields(pattern)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty pattern")
	}

	var steps []PathStep
	i := 0
	for i < len(tokens) {
		arrow := tokens[i]
		switch arrow {
		case "->", "->*", "<-", "<-*":
			// expect a relation name next
		default:
			return nil, fmt.Errorf("expected direction arrow, got %q at position %d", arrow, i)
		}

		i++
		if i >= len(tokens) {
			return nil, fmt.Errorf("expected relation name after %q", arrow)
		}
		rel := tokens[i]
		i++

		switch arrow {
		case "->":
			steps = append(steps, PathStep{Relation: rel})
		case "->*":
			steps = append(steps, PathStep{Relation: rel, Recursive: true})
		case "<-", "<-*":
			return nil, fmt.Errorf("back-direction steps (%s) are not yet supported", arrow)
		}
	}
	return steps, nil
}
