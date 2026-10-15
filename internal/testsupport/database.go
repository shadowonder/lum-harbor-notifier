package testsupport

import (
	"context"
	"testing"

	"lum-harbor-notifier/internal/database"
)

// OpenTestDatabase creates an isolated migrated database for integration tests.
//
// The database is automatically closed when the test finishes.
func OpenTestDatabase(t *testing.T) *database.Database {
	t.Helper()

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	db, err := database.Open(context.Background())
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	t.Cleanup(func() {
		if err := db.DB.Close(); err != nil {
			t.Errorf("failed to close databse: %v", err)
		}
	})

	if err := database.Migrate(db.DB); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return db
}
