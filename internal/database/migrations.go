package database

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies all pending database migrations to db using goose.
// It configures goose to read migration files from the embedded migrationFiles
// filesystem, sets the SQL dialect to sqlite3, and runs any migrations that
// have not yet been applied.
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationFiles)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("configure migration dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}
	return nil
}
