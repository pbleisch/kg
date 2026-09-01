package batch

import (
	"fmt"
	"strings"

	"github.com/pbleisch/kg/pkg/store"
)

// Version is the only envelope version this build accepts.
const Version = 1

// maxKeyLen bounds the idempotency key so it stays usable as a ledger filename.
const maxKeyLen = 128

// An Envelope carries the three things a batch needs that the compiler cannot
// supply: which version of the record shape the producer wrote, the key that
// makes a replay a no-op, and the contributor whose account this batch is.
//
// Its fields are unexported and it has no literal form outside this package, so
// the only Envelope a caller can hand to [Apply] is one [NewEnvelope] agreed to
// build.  A zero Envelope is refused.
type Envelope struct {
	version     int
	key         string
	contributor string
}

// NewEnvelope validates and builds the envelope for one batch, naming no
// contributor.
//
// idempotencyKey identifies the batch's *content*: submitting the same records
// under the same key twice applies them once.  Producers derive it from a hash
// of the records; anything stable and unique will do.
//
// A batch with no contributor still writes.  Its multi-valued items land
// carrying no source, which means no later contributor's write replaces them
// and only `unset` removes them — the right behaviour for a `kg create` or
// `kg set` typed by a person, whose edit then survives every re-ingest.  It is
// reported, not refused (ADR-0030).
func NewEnvelope(version int, idempotencyKey string) (Envelope, error) {
	e := Envelope{version: version, key: idempotencyKey}
	if err := e.check(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// NewEnvelopeFrom builds the envelope for a batch that speaks for one
// contributor: one document, one batch, one source.
//
// The contributor is the unit of reset.  A multi-valued attribute drops the
// items this contributor wrote before and appends the ones this batch carries,
// leaving every other contributor's items standing — which is what makes
// re-ingesting a re-extracted document converge instead of accumulating
// (ADR-0030, P5).
//
// contributor must be a well-formed entity id, `type:local`.  It need not
// already exist: whether it resolves is content, and this is grammar (P3).
func NewEnvelopeFrom(version int, idempotencyKey, contributor string) (Envelope, error) {
	e := Envelope{version: version, key: idempotencyKey}
	if contributor != "" {
		e.contributor = store.NormalizeEntityID(contributor)
		if _, _, err := store.ParseEntityID(e.contributor); err != nil {
			return Envelope{}, fmt.Errorf("batch: contributor %q is not an entity id: %w", contributor, err)
		}
	}
	if err := e.check(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// Version reports the envelope version.
func (e Envelope) Version() int { return e.version }

// IdempotencyKey reports the batch's idempotency key.
func (e Envelope) IdempotencyKey() string { return e.key }

// Contributor reports the source this batch speaks for, or "" for none.
func (e Envelope) Contributor() string { return e.contributor }

func (e Envelope) check() error {
	if e.version == 0 && e.key == "" {
		return fmt.Errorf("batch: envelope is missing; build one with batch.NewEnvelope")
	}
	if e.version != Version {
		return fmt.Errorf("batch: unsupported envelope version %d (this build accepts %d)", e.version, Version)
	}
	if e.key == "" {
		return fmt.Errorf("batch: envelope is missing an idempotency key")
	}
	if len(e.key) > maxKeyLen {
		return fmt.Errorf("batch: idempotency key is %d bytes, limit is %d", len(e.key), maxKeyLen)
	}
	if i := strings.IndexFunc(e.key, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		case r == '-', r == '_', r == '.', r == ':':
			return false
		}
		return true
	}); i >= 0 {
		return fmt.Errorf("batch: idempotency key contains %q at byte %d (allowed: A-Z a-z 0-9 - _ . :)",
			string(e.key[i]), i)
	}
	return nil
}
