package database

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestOpenCreatesSQLiteDatabase verifies the most basic database
// startup contract:
//
//  1. Lum Harbor can create its application database directory.
//  2. SQLite can be opened successfully.
//  3. The database file is actually created on disk.
//
// The test replaces HOME with a temporary directory so it never
// touches the user's real ~/Library/Application Support directory.
func TestOpenCreatesSQLiteDatabase(t *testing.T) {
	tempHome := t.TempDir()

	// os.UserConfigDir() uses HOME on macOS.
	// t.Setenv automatically restores the original value after this
	// test finishes.
	t.Setenv("HOME", tempHome)

	ctx := context.Background()

	db, err := Open(ctx)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Always close database resources created by a test.
	// This is especially important for SQLite because WAL/SHM files
	// may remain open while the connection is alive.
	defer db.DB.Close()

	if db.path == "" {
		t.Fatal("expected database path to be populated")
	}

	// The database created during this test should live somewhere under
	// our temporary HOME instead of the developer's real application data.
	if !strings.HasPrefix(db.path, tempHome) {
		t.Fatalf(
			"expected database path to be under temporary home %q, got %q",
			tempHome,
			db.path,
		)
	}

	info, err := os.Stat(db.path)
	if err != nil {
		t.Fatalf("expected database file to exist: %v", err)
	}

	if info.IsDir() {
		t.Fatalf(
			"expected database path %q to be a file, but it is a directory",
			db.path,
		)
	}
}

// TestBuildDSNProducesValidSQLiteURIForWindowsAndUnixPaths proves buildDSN's
// forward-slash/leading-slash normalization without requiring a real Windows
// host: it exercises the exact string transformation buildDSN performs on
// any OS, using a literal Windows-style absolute path as input. Before this
// fix, url.URL{Scheme:"file", Path: `C:\Users\...`}.String() produced
// "file://C:%5CUsers%5C..." — a string net/url itself cannot parse back (the
// "C:" reads as an invalid host:port) and that does not match any of
// SQLite's documented URI filename forms. This test also locks in that the
// existing macOS/Unix DSN shape is unchanged by the fix.
func TestBuildDSNProducesValidSQLiteURIForWindowsAndUnixPaths(t *testing.T) {
	windowsDSN := buildDSN(`C:\Users\Test\AppData\Roaming\lum_harbor\lum_harbor.db`)
	parsedWindows, err := url.Parse(windowsDSN)
	if err != nil {
		t.Fatalf("Windows-style DSN must be a valid, parseable URI: %v (dsn=%q)", err, windowsDSN)
	}
	if parsedWindows.Host != "" {
		t.Fatalf("Windows drive letter must not be read as a URL host, got host=%q dsn=%q", parsedWindows.Host, windowsDSN)
	}
	if parsedWindows.Path != "/C:/Users/Test/AppData/Roaming/lum_harbor/lum_harbor.db" {
		t.Fatalf("unexpected decoded Windows path: %q (dsn=%q)", parsedWindows.Path, windowsDSN)
	}
	if !strings.HasPrefix(windowsDSN, "file:///C:/") {
		t.Fatalf("expected SQLite's documented file:///C:/... Windows URI form, got %q", windowsDSN)
	}

	unixDSN := buildDSN("/Users/test/Library/Application Support/lum_harbor/lum_harbor.db")
	parsedUnix, err := url.Parse(unixDSN)
	if err != nil {
		t.Fatalf("Unix-style DSN must be a valid, parseable URI: %v (dsn=%q)", err, unixDSN)
	}
	if parsedUnix.Host != "" {
		t.Fatalf("expected no host component for a Unix path, got host=%q dsn=%q", parsedUnix.Host, unixDSN)
	}
	if parsedUnix.Path != "/Users/test/Library/Application Support/lum_harbor/lum_harbor.db" {
		t.Fatalf("unexpected decoded Unix path: %q (dsn=%q)", parsedUnix.Path, unixDSN)
	}
	if !strings.HasPrefix(unixDSN, "file:///Users/") {
		t.Fatalf("expected the existing file:///Users/... Unix URI form to remain unchanged, got %q", unixDSN)
	}
}

// TestOpenEnablesWAL verifies the modernc DSN against SQLite runtime state.
// Checking PRAGMA journal_mode on the opened file catches misspelled driver
// parameters that modernc may otherwise ignore without returning an error.
func TestOpenEnablesWAL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	db, err := Open(context.Background())
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close database: %v", err)
		}
	})

	var journalMode string
	if err := db.DB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("failed to read SQLite journal mode: %v", err)
	}
	if normalized := strings.ToLower(journalMode); normalized != "wal" {
		t.Fatalf("expected SQLite journal mode wal, got %q", journalMode)
	}
}
