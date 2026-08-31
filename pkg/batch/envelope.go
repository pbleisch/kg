package batch

import (
	"fmt"
	"strings"
)

// Version is the only envelope version this build accepts.
const Version = 1

// maxKeyLen bounds the idempotency key so it stays usable as a ledger filename.
const maxKeyLen = 128

// An Envelope carries the two things a batch needs that the compiler cannot
// supply: which version of the record shape the producer wrote, and the key
// that makes a replay a no-op.
//
// Its fields are unexported and it has no literal form outside this package, so
// the only Envelope a caller can hand to [Apply] is one [NewEnvelope] agreed to
// build.  A zero Envelope is refused.
type Envelope struct {
	version int
	key     string
}

// NewEnvelope validates and builds the envelope for one batch.
//
// idempotencyKey identifies the batch's *content*: submitting the same records
// under the same key twice applies them once.  Producers derive it from a hash
// of the records; anything stable and unique will do.
func NewEnvelope(version int, idempotencyKey string) (Envelope, error) {
	e := Envelope{version: version, key: idempotencyKey}
	if err := e.check(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// Version reports the envelope version.
func (e Envelope) Version() int { return e.version }

// IdempotencyKey reports the batch's idempotency key.
func (e Envelope) IdempotencyKey() string { return e.key }

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
