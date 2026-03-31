package store

import "testing"

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// PascalCase
		{"Person", "person"},
		{"SomeConcept", "some_concept"},
		{"GameMechanic", "game_mechanic"},

		// camelCase
		{"someConcept", "some_concept"},
		{"gameID", "game_id"},

		// Acronyms
		{"someHTTPThing", "some_http_thing"},
		{"HTTPServer", "http_server"},
		{"getAPIKey", "get_api_key"},
		{"XMLParser", "xml_parser"},
		{"parseJSON", "parse_json"},

		// SCREAMING_SNAKE
		{"SOME_CONCEPT", "some_concept"},
		{"HTTP_SERVER", "http_server"},
		{"ALL_CAPS", "all_caps"},

		// Spaces
		{"some concept", "some_concept"},
		{"Some Concept", "some_concept"},
		{"SOME CONCEPT", "some_concept"},

		// Hyphens (kebab-case)
		{"some-concept", "some_concept"},
		{"Some-Concept", "some_concept"},

		// Already snake_case
		{"already_snake", "already_snake"},
		{"person", "person"},
		{"game_mechanic", "game_mechanic"},

		// Single word
		{"Person", "person"},
		{"PERSON", "person"},
		{"person", "person"},

		// Numeric suffixes
		{"item2", "item2"},
		{"Item2Name", "item2_name"},
		{"v2API", "v2_api"},

		// Mixed separators
		{"some-mixed_Case thing", "some_mixed_case_thing"},

		// Edge cases
		{"", ""},
		{"a", "a"},
		{"A", "a"},
		{"ID", "id"},
		{"_leading", "leading"},
		{"trailing_", "trailing"},
		{"__double__", "double"},

		// Dots
		{"some.thing", "some_thing"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ToSnakeCase(tt.input)
			if got != tt.want {
				t.Errorf("ToSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeEntityID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"person:alice", "person:alice"},
		{"Person:alice", "person:alice"},
		{"Person:SomeConcept", "person:some_concept"},
		{"GameMechanic:ComboSystem", "game_mechanic:combo_system"},
		{"GAME_MECHANIC:COMBO_SYSTEM", "game_mechanic:combo_system"},
		{"game_mechanic:combo_system", "game_mechanic:combo_system"},
		{"person:Alice Smith", "person:alice_smith"},

		// Bare local ID (no colon)
		{"SomeConcept", "some_concept"},

		// Empty
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeEntityID(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeEntityID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeRelation(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"worksOn", "works_on"},
		{"RelatesTo", "relates_to"},
		{"authored_by", "authored_by"},
		{"knows", "knows"},
		{"WORKS_ON", "works_on"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeRelation(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeRelation(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
