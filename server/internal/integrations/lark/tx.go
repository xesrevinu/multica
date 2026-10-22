package lark

import (
	"context"
	"database/sql"
)

// TxStarter abstracts transaction creation. Re-declared in this package (rather
// than depending on internal/service) so the integrations layer does not
// back-reference into service — a circular dependency we want to avoid as both
// packages grow. Satisfied by *sql.DB.
type TxStarter interface {
	Begin(ctx context.Context) (*sql.Tx, error)
}
