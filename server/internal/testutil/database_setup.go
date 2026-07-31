package testutil

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

// GetTestDB creates a test database connection and returns a sql.DB ref for
// testing. The database is dropped and the connection closed when the test
// completes. It delegates to dbtest.GetTestDB, which lives in a leaf package
// so tests in packages that testutil itself imports (e.g. domain/command) can
// use the same restart-tolerant harness without an import cycle.
func GetTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return dbtest.GetTestDB(t)
}
