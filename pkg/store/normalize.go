package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Fix describes a single normalization applied to an entity file.
type Fix struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// NormalizeResult describes what was normalized for a single entity file.
type NormalizeResult struct {
	Path     string `json:"path"`
	EntityID string `json:"entity_id"`
	Fixes    []Fix  `json:"fixes,omitempty"`
	Moved    bool   `json:"moved,omitempty"`
	OldPath  string `json:"old_path,omitempty"`
	NewPath  string `json:"new_path,omitempty"`
}

// NormalizeEntityFile reads a YAML entity file, applies normalizations,
// and writes the result back. Returns a result describing changes made.
// If no fixes are needed, result.Fixes will be empty and the file is
// not rewritten.
func NormalizeEntityFile(kgDir, filePath string) (*NormalizeResult, error) {
	result := &NormalizeResult{Path: filePath}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filePath, err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filePath, err)
	}

	// Derive expected type and local ID from directory structure.
	// Path: {kgDir}/entities/{type}/{localid}.yaml
	entitiesDir := filepath.Join(kgDir, "entities")
	rel, err := filepath.Rel(entitiesDir, filePath)
	if err != nil {
		return nil, fmt.Errorf("unexpected path: %w", err)
	}
	parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected entity path structure: %s", rel)
	}
	dirType := parts[0]
	localFromFile := strings.TrimSuffix(filepath.Base(parts[1]), ".yaml")

	normalizeLinksKey(raw, result)
	normalizeID(raw, dirType, localFromFile, result)
	normalizeType(raw, dirType, result)
	normalizeRelationTargets(raw, result)

	result.EntityID, _ = raw["id"].(string)

	// Check if file needs to move (even if no other fixes).
	expectedPath := EntityPath(kgDir, result.EntityID)
	if expectedPath != "" && expectedPath != filePath {
		result.Moved = true
		result.OldPath = filePath
		result.NewPath = expectedPath
		result.Fixes = append(result.Fixes, Fix{
			Field:   "path",
			Message: fmt.Sprintf("moved to correct location"),
		})
	}

	if len(result.Fixes) == 0 {
		return result, nil
	}

	// Reconstruct Entity and write back.
	e := entityFromRawMap(raw)

	if err := WriteEntity(kgDir, e); err != nil {
		return nil, fmt.Errorf("writing normalized entity: %w", err)
	}

	if result.Moved {
		os.Remove(filePath)
		removeEmptyDir(filepath.Dir(filePath))
	}

	return result, nil
}

// normalizeLinksKey renames the "links" YAML key to "relations".
func normalizeLinksKey(raw map[string]interface{}, result *NormalizeResult) {
	links, hasLinks := raw["links"]
	if !hasLinks {
		return
	}

	if rels, hasRels := raw["relations"]; hasRels {
		// Both exist — merge links into relations.
		mergeRelMaps(asStringMapIface(rels), asStringMapIface(links))
	} else {
		raw["relations"] = links
	}
	delete(raw, "links")
	result.Fixes = append(result.Fixes, Fix{
		Field: "links", Message: "renamed 'links' to 'relations'",
	})
}

// normalizeID ensures the id field is present and has snake_case type prefix and local ID.
func normalizeID(raw map[string]interface{}, dirType, localFromFile string, result *NormalizeResult) {
	idVal, _ := raw["id"].(string)

	if idVal == "" {
		idVal = ToSnakeCase(dirType) + ":" + ToSnakeCase(localFromFile)
		raw["id"] = idVal
		result.Fixes = append(result.Fixes, Fix{
			Field: "id", Message: fmt.Sprintf("set missing id to %s", idVal),
		})
		return
	}

	typePart, localPart, err := ParseEntityID(idVal)
	if err != nil {
		// No colon — treat as bare local ID, add type prefix from directory.
		normalized := ToSnakeCase(dirType) + ":" + ToSnakeCase(idVal)
		raw["id"] = normalized
		result.Fixes = append(result.Fixes, Fix{
			Field: "id", Message: fmt.Sprintf("added type prefix: %s → %s", idVal, normalized),
		})
		return
	}

	normalized := ToSnakeCase(typePart) + ":" + ToSnakeCase(localPart)
	if normalized != idVal {
		raw["id"] = normalized
		result.Fixes = append(result.Fixes, Fix{
			Field: "id", Message: fmt.Sprintf("normalized id: %s → %s", idVal, normalized),
		})
	}
}

// normalizeType ensures the type field is snake_case.
func normalizeType(raw map[string]interface{}, dirType string, result *NormalizeResult) {
	typeVal, _ := raw["type"].(string)

	if typeVal == "" {
		typeVal = ToSnakeCase(dirType)
		raw["type"] = typeVal
		result.Fixes = append(result.Fixes, Fix{
			Field: "type", Message: fmt.Sprintf("set missing type to %s", typeVal),
		})
		return
	}

	normalized := ToSnakeCase(typeVal)
	if normalized != typeVal {
		raw["type"] = normalized
		result.Fixes = append(result.Fixes, Fix{
			Field: "type", Message: fmt.Sprintf("normalized type: %s → %s", typeVal, normalized),
		})
	}
}

// normalizeRelationTargets ensures all relation target IDs are snake_case
// and normalizes relation key names to snake_case.
func normalizeRelationTargets(raw map[string]interface{}, result *NormalizeResult) {
	rels := asStringMapIface(raw["relations"])
	if rels == nil {
		return
	}

	// Collect renames to avoid modifying map during iteration.
	type rename struct{ old, new string }
	var renames []rename

	for relName, targets := range rels {
		// Normalize relation key name.
		normRel := ToSnakeCase(relName)
		if normRel != relName {
			renames = append(renames, rename{relName, normRel})
			result.Fixes = append(result.Fixes, Fix{
				Field:   fmt.Sprintf("relations.%s", relName),
				Message: fmt.Sprintf("normalized relation key: %s → %s", relName, normRel),
			})
		}

		// Normalize target IDs.
		targetList, ok := targets.([]interface{})
		if !ok {
			continue
		}
		for i, t := range targetList {
			s, ok := t.(string)
			if !ok {
				continue
			}
			normalized := NormalizeEntityID(s)
			if normalized != s {
				targetList[i] = normalized
				result.Fixes = append(result.Fixes, Fix{
					Field:   fmt.Sprintf("relations.%s", relName),
					Message: fmt.Sprintf("normalized target: %s → %s", s, normalized),
				})
			}
		}
	}

	// Apply relation key renames.
	for _, r := range renames {
		targets := rels[r.old]
		delete(rels, r.old)
		if existing, ok := rels[r.new]; ok {
			// Merge into existing key.
			mergeRelMaps(
				map[string]interface{}{r.new: existing},
				map[string]interface{}{r.new: targets},
			)
		} else {
			rels[r.new] = targets
		}
	}
}

// entityFromRawMap constructs an Entity from a raw YAML map.
func entityFromRawMap(raw map[string]interface{}) *Entity {
	e := &Entity{}
	if id, ok := raw["id"].(string); ok {
		e.ID = id
	}
	if t, ok := raw["type"].(string); ok {
		e.Type = t
	}

	if attrs, ok := raw["attributes"].(map[string]interface{}); ok {
		e.Attributes = make(map[string]any, len(attrs))
		for k, v := range attrs {
			e.Attributes[k] = v
		}
	}

	rels := asStringMapIface(raw["relations"])
	if rels != nil {
		e.Relations = make(map[string][]string)
		for k, v := range rels {
			if targets, ok := v.([]interface{}); ok {
				for _, t := range targets {
					if s, ok := t.(string); ok {
						e.Relations[k] = append(e.Relations[k], s)
					}
				}
			}
		}
	}

	return e
}

func asStringMapIface(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

// mergeRelMaps merges src relation entries into dst, deduplicating targets.
func mergeRelMaps(dst, src map[string]interface{}) {
	if dst == nil || src == nil {
		return
	}
	for k, v := range src {
		existing, ok := dst[k]
		if !ok {
			dst[k] = v
			continue
		}
		existList, _ := existing.([]interface{})
		srcList, _ := v.([]interface{})
		for _, t := range srcList {
			if !containsIface(existList, t) {
				existList = append(existList, t)
			}
		}
		dst[k] = existList
	}
}

func containsIface(list []interface{}, val interface{}) bool {
	for _, v := range list {
		if v == val {
			return true
		}
	}
	return false
}

func removeEmptyDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	os.Remove(dir)
}
