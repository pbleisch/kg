package store

import (
	"fmt"
	"sort"
	"strings"
)

// attrNameOK reports whether name is a legal attribute name: lowercase ASCII,
// starting with a letter, underscores between words.
//
// The rule is deliberately narrow.  2,118 entities in the GDKB corpus carry an
// attribute literally named "--dir" with the value ".kg", because a producer
// split a command line on '=' and called anything to the left of it a field.
// Nothing between that split and the YAML file disagreed.  This is the check
// that disagrees.
func attrNameOK(name string) bool {
	if name == "" {
		return false
	}
	if name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}
	return true
}

// ValidAttributeName reports whether name may be used as an entity attribute.
func ValidAttributeName(name string) bool { return attrNameOK(name) }

// IllegalAttributeNames returns, sorted, every key of attrs that is not a legal
// attribute name.  Generic over the value type so a caller holding raw
// key=value strings and a caller holding decoded YAML share one rule.
func IllegalAttributeNames[V any](attrs map[string]V) []string {
	var bad []string
	for k := range attrs {
		if !attrNameOK(k) {
			bad = append(bad, k)
		}
	}
	sort.Strings(bad)
	return bad
}

// ValidateAttributeNames returns an error naming every illegal key in attrs.
// The error names the field, because "invalid attribute" tells a producer
// nothing about which one to stop emitting.
func ValidateAttributeNames[V any](attrs map[string]V) error {
	bad := IllegalAttributeNames(attrs)
	if len(bad) == 0 {
		return nil
	}
	quoted := make([]string, len(bad))
	for i, b := range bad {
		quoted[i] = fmt.Sprintf("%q", b)
	}
	return fmt.Errorf("illegal attribute name(s): %s (must match [a-z][a-z0-9_]*)",
		strings.Join(quoted, ", "))
}
