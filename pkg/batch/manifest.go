package batch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/pbleisch/kg/pkg/store"
	"gopkg.in/yaml.v3"
)

// A Manifest is the file form of a batch.  It is a producer's serialization of
// what [Apply] takes, not a second boundary.
type Manifest struct {
	Version        int    `yaml:"version" json:"version"`
	IdempotencyKey string `yaml:"idempotency_key,omitempty" json:"idempotency_key,omitempty"`

	// Contributor is the entity id of the source this batch speaks for: one
	// document, one batch, one source.  It is the unit of reset — a
	// multi-valued attribute drops the items this contributor wrote before and
	// keeps every other contributor's (ADR-0030).  A manifest that names none
	// still applies; its items land carrying no source and are then permanent.
	Contributor string `yaml:"contributor,omitempty" json:"contributor,omitempty"`

	Actions []Action       `yaml:"actions" json:"actions"`
	Options map[string]any `yaml:"options,omitempty" json:"options,omitempty"`
}

// An Action is a manifest's spelling of a [Record].
type Action struct {
	ID         string         `yaml:"id,omitempty" json:"id,omitempty"`
	Action     string         `yaml:"action" json:"action"`
	Entity     *store.Entity  `yaml:"entity,omitempty" json:"entity,omitempty"`
	Target     string         `yaml:"target,omitempty" json:"target,omitempty"`
	From       string         `yaml:"from,omitempty" json:"from,omitempty"`
	To         string         `yaml:"to,omitempty" json:"to,omitempty"`
	Rel        string         `yaml:"rel,omitempty" json:"rel,omitempty"`
	Prefer     string         `yaml:"prefer,omitempty" json:"prefer,omitempty"`
	Attributes map[string]any `yaml:"attributes,omitempty" json:"attributes,omitempty"`
	Unset      []string       `yaml:"unset,omitempty" json:"unset,omitempty"`
}

// DecodeManifest reads a YAML (or JSON, which is YAML) batch file.
//
// Decoding is strict: a key the shape does not declare is an error rather than
// a field that quietly disappears.  That is this format's half of "a record
// with an unknown field is rejected, not stored" — the other half is the
// attribute-name rule, which covers the one map whose keys are open.
//
// Version defaults to the current one, and a manifest that names no
// idempotency key gets one derived from its own bytes, so re-applying the same
// file is a no-op.  Both defaults live here, in the library, because the CLI
// holds none of its own (P7).
func DecodeManifest(data []byte) (Envelope, []Record, error) {
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return Envelope{}, nil, fmt.Errorf("parsing manifest: %w", err)
	}

	if m.Version == 0 {
		m.Version = Version
	}
	key := m.IdempotencyKey
	if key == "" {
		sum := sha256.Sum256(data)
		key = KeyFor("manifest", hex.EncodeToString(sum[:])[:32])
	}
	env, err := NewEnvelopeFrom(m.Version, key, m.Contributor)
	if err != nil {
		return Envelope{}, nil, err
	}

	records := make([]Record, 0, len(m.Actions))
	for _, a := range m.Actions {
		records = append(records, Record{
			ID:         a.ID,
			Op:         Op(a.Action),
			Entity:     a.Entity,
			Target:     a.Target,
			From:       a.From,
			Rel:        a.Rel,
			To:         a.To,
			Prefer:     a.Prefer,
			Attributes: a.Attributes,
			Unset:      a.Unset,
		})
	}
	return env, records, nil
}
