package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"lum-harbor-notifier/internal/appdir"

	_ "modernc.org/sqlite"
)

// Database owns the process-wide SQL handle and its on-disk path. The handle
// is migrated before Open returns and is closed by the application composition
// root during shutdown.
type Database struct {
	DB   *sql.DB
	path string
}

// Open creates Lum Harbor's application-scoped SQLite database and returns it
// only after the connection has been established and all embedded migrations
// have completed. The DSN enables foreign-key enforcement, WAL journaling, a
// five-second busy timeout, and NORMAL synchronous durability.
func Open(ctx context.Context) (*Database, error) {
	dbDir, err := appdir.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve database directory: %w", err)
	}

	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	dbPath := filepath.Join(dbDir, "lum_harbor.db")

	dsn := buildDSN(dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// V1 intentionally serializes database work through one connection even
	// though WAL is enabled. Re-evaluate read concurrency and pool sizing only
	// after the Background Scheduler or source ingestion introduces a measured
	// concurrent workload; changing both journaling and pooling together would
	// obscure their independent runtime effects.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	// Open() guarantees that callers receive a schema-ready database.
	if err := Migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite database: %w", err)
	}

	return &Database{
		DB:   db,
		path: dbPath,
	}, nil
}

// buildDSN configures the modernc SQLite driver pragmas used by every opened
// connection. WAL is persistent for the database file, while foreign_keys,
// busy_timeout, and synchronous are connection settings applied by the driver.
//
// The path must be converted to forward slashes and given a leading slash
// before being placed on a url.URL, regardless of host OS. A bare
// url.URL{Scheme:"file", Path: dbPath} is not enough: for a Windows path such
// as `C:\Users\...`, url.URL.String() treats the non-empty, non-"/"-prefixed
// Path as requiring an authority component, producing the malformed
// "file://C:%5CUsers%5C..." — which net/url itself cannot even parse back
// (the "C:" reads as a host:port pair) and which does not match any of
// SQLite's documented URI filename forms. Forcing forward slashes and a
// leading slash instead produces "file:///C:/Users/..." on Windows and
// "file:///Users/..." on Unix, both valid per SQLite's URI filename syntax
// (https://www.sqlite.org/uri.html) and both round-trip cleanly through
// url.Parse.
func buildDSN(dbPath string) string {
	urlPath := strings.ReplaceAll(dbPath, `\`, "/")
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	u := &url.URL{
		Scheme: "file",
		Path:   urlPath,
	}

	query := u.Query()
	query.Set("_foreign_keys", "on")
	query.Set("_journal_mode", "WAL")
	query.Set("_busy_timeout", "5000")
	query.Set("_synchronous", "NORMAL")

	u.RawQuery = query.Encode()
	return u.String()
}

// Close releases the application database handle. It is safe to call on a nil
// or partially initialized Database during startup-error cleanup.
func (d *Database) Close() error {
	if d == nil || d.DB == nil {
		return nil
	}
	return d.DB.Close()
}
