package sqliteutil

import (
	"database/sql/driver"
	"time"

	"github.com/google/uuid"
	"modernc.org/sqlite"
)

// init registers SQLite stand-ins for the Postgres functions the schema and
// queries call. modernc.org/sqlite applies driver-level scalar functions to
// every connection opened afterwards (sqlite.MustRegisterScalarFunction).
func init() {
	sqlite.MustRegisterScalarFunction(
		"gen_random_uuid",
		0,
		func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
			return uuid.NewString(), nil
		},
	)
	sqlite.MustRegisterScalarFunction(
		"now",
		0,
		func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
			return time.Now().UTC().Format(time.RFC3339Nano), nil
		},
	)
}
