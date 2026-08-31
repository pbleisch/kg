package query

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pbleisch/kg/pkg/store"
)

// Similar finds entities of the given type ranked by vector similarity,
// reading the vector sidecar.
func Similar(kgDir string, params store.SimilarityParams) ([]store.SimilarityResult, error) {
	return store.Similar(kgDir, params)
}

// VectorForEntity returns an entity's stored vector, which is how
// `kg similar --like` turns an entity into a query vector.
func VectorForEntity(kgDir, entityID string, fields []string) ([]float64, error) {
	vec, _, err := store.VectorFor(kgDir, entityID, fields)
	return vec, err
}

// ParseVector parses a comma-separated string of floats into a []float64.
func ParseVector(s string) ([]float64, error) {
	parts := strings.Split(s, ",")
	vec := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		f, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float %q: %w", p, err)
		}
		vec = append(vec, f)
	}
	if len(vec) == 0 {
		return nil, fmt.Errorf("empty vector")
	}
	return vec, nil
}

// LoadVectorFile reads a query vector from a file. Two formats are accepted:
//   - Single line of comma-separated floats: "0.1,0.2,0.3"
//   - One float per line
func LoadVectorFile(path string) ([]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading vector file: %w", err)
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil, fmt.Errorf("vector file %s is empty", path)
	}

	// Single-line comma-separated.
	if !strings.Contains(content, "\n") {
		return ParseVector(content)
	}

	// One float per line.
	scanner := bufio.NewScanner(strings.NewReader(content))
	var vec []float64
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		f, err := strconv.ParseFloat(line, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float %q in %s: %w", line, path, err)
		}
		vec = append(vec, f)
	}
	if len(vec) == 0 {
		return nil, fmt.Errorf("no floats found in %s", path)
	}
	return vec, nil
}
