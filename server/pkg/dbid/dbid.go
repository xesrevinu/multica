// Package dbid mints primary keys for rows the application inserts.
//
// Every id produced here is a UUIDv7 (RFC 9562): a 48-bit big-endian
// millisecond timestamp followed by random bits. Consecutive inserts therefore
// cluster in a narrow contiguous key range instead of scattering across the
// primary-key B-tree the way random v4 keys do. On tables that already contain
// v4 ids, that range is not necessarily the tree's right edge.
//
// Scope and rules of use:
//
//   - Only for identity columns of rows we insert and keep — task queue entries,
//     messages, comments, activity and audit rows. NOT for lease/claim tokens,
//     idempotency keys, or anything used as a secret: those want
//     unpredictability and no embedded timestamp, so they stay on random v4.
//   - Identity columns are TEXT. Callers pass NewV7() straight into a sqlc
//     parameter. An empty return means entropy generation failed; callers that
//     must use the id externally should call uuid.NewV7 directly instead.
//   - A v7 embeds its creation time only to millisecond precision and is only
//     approximately ordered across writers. It is not a substitute for
//     created_at and must not be used to derive ordering guarantees.
//   - On an INSERT ... ON CONFLICT, an id passed in is used only if the row is
//     actually inserted; the conflicting row keeps its own id. Callers must read
//     the id back from RETURNING rather than assume the value they minted won.
package dbid

import (
	"log/slog"

	"github.com/google/uuid"
)

// NewV7 returns a fresh UUIDv7 string, ready to assign to a sqlc-generated
// parameter struct's ID field.
//
// It deliberately has no error return. uuid.NewV7 only fails when the OS
// entropy source does, which does not happen on a healthy host; if it ever
// does, this returns an empty string so the caller can see the failure instead
// of inserting a zero id.
func NewV7() string {
	id, err := uuid.NewV7()
	if err != nil {
		slog.Error("dbid: uuidv7 generation failed", "error", err)
		return ""
	}
	return id.String()
}
