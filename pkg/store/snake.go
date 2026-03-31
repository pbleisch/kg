package store

import (
	"strings"
	"unicode"
)

// ToSnakeCase converts any identifier string to snake_case.
//
// Handles PascalCase, camelCase, SCREAMING_SNAKE, kebab-case, spaces,
// and mixed separators. Acronyms are split: "someHTTPThing" → "some_http_thing".
func ToSnakeCase(s string) string {
	if s == "" {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + 4) // typical expansion

	runes := []rune(s)
	n := len(runes)

	for i := 0; i < n; i++ {
		r := runes[i]

		// Replace separators (spaces, hyphens, dots) with underscores.
		if r == ' ' || r == '-' || r == '.' {
			if b.Len() > 0 {
				b.WriteByte('_')
			}
			continue
		}

		// Existing underscore — pass through.
		if r == '_' {
			if b.Len() > 0 {
				b.WriteByte('_')
			}
			continue
		}

		if unicode.IsUpper(r) {
			// Insert underscore before uppercase if:
			//   - not at start
			//   - previous char was lowercase or digit, OR
			//   - this is the start of an acronym-to-word transition
			//     (e.g., "HTTP" followed by "Thing" → insert before "T")
			if b.Len() > 0 {
				prev := runes[i-1]
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					// camelCase boundary: "someThing" → "some_thing"
					b.WriteByte('_')
				} else if unicode.IsUpper(prev) && i+1 < n && unicode.IsLower(runes[i+1]) {
					// Acronym→word boundary: "HTTPThing" → "HTTP_Thing"
					b.WriteByte('_')
				}
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(unicode.ToLower(r))
		}
	}

	result := b.String()

	// Collapse multiple consecutive underscores.
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}

	// Strip leading/trailing underscores.
	result = strings.Trim(result, "_")

	return result
}

// NormalizeEntityID normalizes a full entity ID ("type:localID") by
// applying snake_case to both the type prefix and the local ID.
// If the input has no colon, it is treated as a bare local ID and
// returned as snake_case.
func NormalizeEntityID(id string) string {
	if id == "" {
		return id
	}
	typeName, localID, err := ParseEntityID(id)
	if err != nil {
		// No colon — treat as bare local ID.
		return ToSnakeCase(id)
	}
	return ToSnakeCase(typeName) + ":" + ToSnakeCase(localID)
}

// NormalizeRelation normalizes a relation name to snake_case.
func NormalizeRelation(rel string) string {
	return ToSnakeCase(rel)
}
