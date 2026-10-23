package telegram

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

var testPool *sql.DB

func TestMain(m *testing.M) {
	pool, err := testutil.OpenTestPool()
	if err != nil {
		fmt.Printf("Skipping telegram delivery tests: could not open test db: %v\n", err)
		os.Exit(m.Run())
	}
	testPool = pool
	code := m.Run()
	_ = pool.Close()
	os.Exit(code)
}
