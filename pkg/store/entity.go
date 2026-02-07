package store

// Entity represents a node in the knowledge graph.
type Entity struct {
	ID         string              `yaml:"id"    json:"id"`
	Type       string              `yaml:"type"  json:"type"`
	Attributes map[string]any      `yaml:"attributes" json:"attributes,omitempty"`
	Relations  map[string][]string `yaml:"relations"  json:"relations,omitempty"`
}
