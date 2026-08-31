// Package batch is kg's write boundary.
//
// Every write enters through [Apply]: a versioned envelope, an idempotency key
// and typed records.  The manifest file, the CLI's create/set/link commands and
// any Go caller are producers that come through here; none of them is a
// boundary of its own (P5, P7).
//
// The envelope and the key are the parts that were always doing the work, and a
// Go caller gets neither from the compiler, so they are in the signature rather
// than assumed: [Apply] cannot be called without an [Envelope], and an Envelope
// exists only by way of [NewEnvelope], which refuses a bad one.
//
// Structure is enforced and content is advised (P3).  A record whose attribute
// name is illegal, whose declared type disagrees with its id, whose identity
// collides with another record's, or whose relation points at nothing that
// exists is refused — naming the field and the records — and the batch writes
// nothing.  An unknown attribute on a declared schema is a warning, and a batch
// full of warnings still writes.
//
// A batch is one transaction.  Validation runs over the whole batch before the
// first write, and while records are applied a store journal remembers what
// each file held, so an abort puts the graph back.
package batch
